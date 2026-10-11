#!/usr/bin/env python3
"""本地假上游：OpenAI 兼容的 /v1/chat/completions，用于游客链路端到端验证。

用法：python scripts/ivano/fake-upstream.py [port]（默认 8099）

行为：
  - 模型名包含 "fail"  -> 返回 500，用于验证失败路径；
  - 模型名包含 "slow"  -> 每个分片间隔 1 秒，用于验证中断 / 断线路径；
  - 其他模型名          -> 正常流式返回若干分片并带 usage。
不访问任何真实模型服务，也不产生任何费用。
"""
import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *args):  # 保持输出干净
        return

    def do_GET(self):
        if self.path.startswith("/v1/models"):
            payload = json.dumps({"object": "list", "data": [{"id": "fake-chat", "object": "model"}]}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        self.send_response(404)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b"{}"
        try:
            body = json.loads(raw or b"{}")
        except json.JSONDecodeError:
            body = {}
        model = str(body.get("model") or "fake-chat")

        if "fail" in model:
            payload = json.dumps({"error": {"message": "fake upstream failure", "type": "server_error"}}).encode()
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return

        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream; charset=utf-8")
        self.send_header("Cache-Control", "no-store")
        self.send_header("Connection", "close")
        self.end_headers()

        delay = 1.0 if "slow" in model else 0.05
        for piece in ["你好", "，这是", "本地", "假上游", "的回复。"]:
            payload = {
                "id": "chatcmpl-fake",
                "object": "chat.completion.chunk",
                "model": model,
                "choices": [{"index": 0, "delta": {"content": piece}, "finish_reason": None}],
            }
            self.wfile.write(("data: " + json.dumps(payload, ensure_ascii=False) + "\n\n").encode("utf-8"))
            self.wfile.flush()
            time.sleep(delay)
        final = {
            "id": "chatcmpl-fake",
            "object": "chat.completion.chunk",
            "model": model,
            "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
            "usage": {"prompt_tokens": 12, "completion_tokens": 9, "total_tokens": 21},
        }
        self.wfile.write(("data: " + json.dumps(final, ensure_ascii=False) + "\n\n").encode("utf-8"))
        self.wfile.write(b"data: [DONE]\n\n")
        self.wfile.flush()


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8099
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"fake upstream listening on http://127.0.0.1:{port}/v1", flush=True)
    server.serve_forever()
