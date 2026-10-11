#!/usr/bin/env bash
# Ivano Chat 部署后自检脚本
#
# 用法：
#   bash scripts/ivano/post-deploy-check.sh [BASE_URL]
#
# 示例：
#   bash scripts/ivano/post-deploy-check.sh http://127.0.0.1:8080
#   bash scripts/ivano/post-deploy-check.sh https://chat.ivano.cc.cd
#
# 可选环境变量：
#   EXPECT_BRAND=Ivano          期望的品牌标题（默认 Ivano）
#   ADMIN_USERNAME / ADMIN_PASSWORD   提供后额外检查后台游客接口
#   RUN_GUEST_STREAM=1          额外真实调用一次游客生成（会产生一次上游费用，默认关闭）
#   GUEST_MODEL=xxx             指定游客生成用的平台模型（默认取白名单第一个）
#
# 退出码：0 = 全部通过；1 = 有失败项；2 = 缺少依赖。

set -uo pipefail

BASE_URL="${1:-http://127.0.0.1:8080}"
BASE_URL="${BASE_URL%/}"
EXPECT_BRAND="${EXPECT_BRAND:-Ivano}"
ADMIN_USERNAME="${ADMIN_USERNAME:-}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-}"
RUN_GUEST_STREAM="${RUN_GUEST_STREAM:-0}"
GUEST_MODEL="${GUEST_MODEL:-}"

PASS=0
FAIL=0
SKIP=0
COOKIE_JAR="$(mktemp)"
BODY_FILE="$(mktemp)"
JSON_GETTER="$(mktemp)"
cat > "$JSON_GETTER" <<'PY'
import json, sys
path = sys.argv[1]
fallback = sys.argv[2] if len(sys.argv) > 2 else ""
raw = sys.stdin.read()
try:
    data = json.loads(raw)
except Exception:
    print(fallback)
    sys.exit(0)
current = data
for part in path.split("."):
    if isinstance(current, dict) and part in current:
        current = current[part]
    else:
        print(fallback)
        sys.exit(0)
if isinstance(current, bool):
    print("true" if current else "false")
elif current is None:
    print(fallback)
elif isinstance(current, (list, dict)):
    print(json.dumps(current, ensure_ascii=False))
else:
    print(current)
PY

cleanup() {
  rm -f "$COOKIE_JAR" "$BODY_FILE" "$JSON_GETTER"
}
trap cleanup EXIT

ok()   { printf '[PASS] %s\n' "$1"; PASS=$((PASS + 1)); }
bad()  { printf '[FAIL] %s\n' "$1"; FAIL=$((FAIL + 1)); }
skip() { printf '[SKIP] %s\n' "$1"; SKIP=$((SKIP + 1)); }
info() { printf '       %s\n' "$1"; }
head2() { printf '\n== %s ==\n' "$1"; }

if ! command -v curl >/dev/null 2>&1; then
  echo "缺少依赖：curl，请先安装后再运行"
  exit 2
fi

PY=""
for candidate in "$PY" python; do
  if command -v "$candidate" >/dev/null 2>&1; then
    PY="$candidate"
    break
  fi
done
if [ -z "$PY" ]; then
  echo "缺少依赖：python3（或 python），请先安装后再运行"
  exit 2
fi

CODE=""
BODY=""

request() {
  local method="$1" path="$2" data="${3:-}"
  local args=(-sS -o "$BODY_FILE" -w '%{http_code}' -X "$method" -b "$COOKIE_JAR" -c "$COOKIE_JAR" -H 'Accept: application/json')
  if [ -n "$data" ]; then
    args+=(-H 'Content-Type: application/json' --data "$data")
  fi
  CODE="$(curl "${args[@]}" "$BASE_URL$path" 2>/dev/null || echo 000)"
  BODY="$(cat "$BODY_FILE" 2>/dev/null || true)"
}

jget() {
  # jget <点分路径> [默认值]：从 $BODY 里取 JSON 字段
  printf '%s' "$BODY" | "$PY" "$JSON_GETTER" "$1" "${2-}"
}

printf 'Ivano Chat 部署后自检\n目标：%s\n' "$BASE_URL"

head2 "1. 健康检查"
request GET /healthz
if [ "$CODE" = "200" ]; then
  ok "/healthz 返回 200"
else
  bad "/healthz 返回 $CODE（服务未启动或反代未生效）"
  printf '\n结果：无法继续，请先确认容器与反向代理。\n'
  exit 1
fi

head2 "2. 品牌与版本"
request GET /api/v1/branding
title="$(jget data.title)"
if [ "$CODE" = "200" ] && printf '%s' "$title" | grep -q "$EXPECT_BRAND"; then
  ok "品牌标题为 $title"
else
  bad "品牌接口异常（HTTP $CODE，title=$title，期望包含 $EXPECT_BRAND）"
fi

request GET /api/v1/version
if [ "$CODE" = "200" ]; then
  ok "版本接口正常（product=$(jget product) version=$(jget version)）"
else
  bad "版本接口返回 $CODE"
fi

head2 "3. 游客公开配置"
request GET /api/v1/guest/config
BODY_CFG="$BODY"
guest_enabled="$(jget data.enabled false)"
model_count="$("$PY" -c 'import json,sys;d=json.load(sys.stdin);print(len(d.get("data",{}).get("models") or []))' <<<"$BODY" 2>/dev/null || echo 0)"
if [ "$CODE" = "200" ]; then
  ok "游客配置接口正常（enabled=$guest_enabled，白名单模型数=$model_count）"
  info "预算时区=$(jget data.budgetTimezone) 重置时间=$(jget data.resetAt)"
  info "频率限制=$(jget data.limits.rateLimitPerMinute)/分钟 每日次数=$(jget data.limits.dailyGenerationLimit) 输入上限=$(jget data.limits.inputTokenLimit)"
else
  bad "游客配置接口返回 $CODE"
fi

if [ "$guest_enabled" != "true" ]; then
  skip "游客功能当前关闭：跳过会话与额度检查（可在后台「游客设置」打开后重跑）"
else
  head2 "4. 游客会话与额度"
  request POST /api/v1/guest/session '{}'
  if [ "$CODE" = "200" ]; then
    ok "创建/恢复游客会话成功（guestId=$(jget data.guestId) restored=$(jget data.restored)）"
  else
    bad "创建游客会话返回 $CODE（$BODY）"
  fi

  request GET /api/v1/guest/me
  if [ "$CODE" = "200" ]; then
    ok "额度接口正常（可用 \$$(jget data.perVisitor.availableUSD)，已用 \$$(jget data.perVisitor.settledUSD)，次数 $(jget data.perVisitor.requestCount)/$(jget data.limits.dailyGenerationLimit)）"
    before_count="$(jget data.perVisitor.requestCount 0)"
  else
    bad "额度接口返回 $CODE（游客 Cookie 未被接受？检查反代是否同源、trusted_proxies 是否包含代理地址）"
    before_count=""
  fi

  if [ "$RUN_GUEST_STREAM" = "1" ]; then
    model="$GUEST_MODEL"
    if [ -z "$model" ]; then
      model="$("$PY" -c 'import json,sys;d=json.load(sys.stdin);m=d.get("data",{}).get("models") or [];print(m[0]["platformModelName"] if m else "")' <<<"$BODY_CFG" 2>/dev/null || true)"
    fi
    if [ -z "$model" ]; then
      skip "白名单没有可用模型，跳过游客生成检查"
    else
      head2 "5. 游客生成（真实调用一次，会产生上游费用）"
      run_id="$("$PY" -c 'import uuid;print(uuid.uuid4())')"
      payload="$("$PY" -c 'import json,sys;print(json.dumps({"sessionID":"post-deploy-check","clientRunID":sys.argv[1],"model":sys.argv[2],"messages":[{"role":"user","content":"部署自检：请回复一句话。"}]}, ensure_ascii=False))' "$run_id" "$model")"
      request POST /api/v1/guest/messages/stream "$payload"
      if printf '%s' "$BODY" | grep -q '"type":"completed"'; then
        ok "流式生成成功（model=$model）"
      elif printf '%s' "$BODY" | grep -q '"type":"delta"'; then
        ok "收到流式分片（model=$model，未等到 completed 事件）"
      elif printf '%s' "$BODY" | grep -q '"type":"error"'; then
        bad "生成返回错误：$(printf '%s' "$BODY" | tr -d '\n' | tail -c 300)"
      else
        bad "生成响应异常（HTTP $CODE）：$(printf '%s' "$BODY" | head -c 200)"
      fi

      request GET /api/v1/guest/me
      after_count="$(jget data.perVisitor.requestCount 0)"
      if [ -n "$before_count" ] && [ -n "$after_count" ] && [ "$after_count" != "$before_count" ]; then
        ok "额度已更新（次数 $before_count → $after_count，已用 \$$(jget data.perVisitor.settledUSD)）"
      else
        bad "生成后额度未变化（before=$before_count after=$after_count）"
      fi
    fi
  else
    skip "未开启真实生成检查（如需：RUN_GUEST_STREAM=1 bash $0 $BASE_URL）"
  fi
fi

head2 "6. 后台游客接口"
if [ -z "$ADMIN_USERNAME" ] || [ -z "$ADMIN_PASSWORD" ]; then
  skip "未提供 ADMIN_USERNAME/ADMIN_PASSWORD，跳过后台检查"
else
  login_payload="$("$PY" -c 'import json,sys;print(json.dumps({"username":sys.argv[1],"password":sys.argv[2],"clientID":"deeix-web","clientType":"web"}))' "$ADMIN_USERNAME" "$ADMIN_PASSWORD")"
  request POST /api/v1/auth/login "$login_payload"
  token="$(jget data.accessToken)"
  if [ -z "$token" ]; then
    bad "管理员登录失败：$BODY"
  else
    ok "管理员登录成功"
    admin_get() {
      curl -sS -o "$BODY_FILE" -w '%{http_code}' -H "Authorization: Bearer $token" -H 'Accept: application/json' "$BASE_URL$1" 2>/dev/null || echo 000
    }
    CODE="$(admin_get /api/v1/admin/guest/overview)"
    BODY="$(cat "$BODY_FILE")"
    if [ "$CODE" = "200" ]; then
      ok "后台概览正常（全站已用 \$$(jget data.global.settledUSD)，今日请求 $(jget data.global.requestCount)，白名单 $(jget data.modelWhitelist)）"
    else
      bad "后台概览返回 $CODE"
    fi
    CODE="$(admin_get '/api/v1/admin/guest/requests?page=1&page_size=5')"
    BODY="$(cat "$BODY_FILE")"
    if [ "$CODE" = "200" ]; then
      if printf '%s' "$BODY" | grep -q '"content"'; then
        bad "匿名账单响应中出现 content 字段（不应包含任何正文）"
      else
        ok "匿名账单可查询且不含正文字段（total=$(jget data.total)）"
      fi
    else
      bad "匿名账单返回 $CODE"
    fi
  fi
fi

printf '\n结果：通过 %d，失败 %d，跳过 %d\n' "$PASS" "$FAIL" "$SKIP"
printf '提醒：重启容器后请再跑一次，确认当日额度不恢复、账单不重复（这一步无法全自动）。\n'
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
exit 0
