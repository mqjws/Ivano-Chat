"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { Loader2, RefreshCw, Send, Square, Trash2 } from "lucide-react";
import Link from "next/link";

import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import {
  createGuestSession,
  getGuestConfig,
  getGuestMe,
  streamGuestChatMessage,
  GuestApiError,
  type GuestConfigDTO,
  type GuestMeDTO,
  type GuestModelDTO,
} from "@/shared/api/guest";
import {
  clearGuestChat,
  isGuestChatStoreAvailable,
  loadGuestChat,
  notifyGuestChatChanged,
  saveGuestChat,
  subscribeGuestChatChanges,
  type GuestStoredMessage,
} from "@/features/guest/model/guest-chat-store";
import { StreamdownRender } from "@/shared/components/markdown/streamdown-render";
import { createSecureUUID } from "@/shared/lib/secure-id";
import { cn } from "@/lib/utils";

type GuestMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  streaming?: boolean;
  failed?: boolean;
  costNanousd?: number;
  costUSD?: string;
  errorText?: string;
};

function resolveErrorMessage(error: unknown, t: (key: string) => string): string {
  if (error instanceof GuestApiError) {
    switch (error.code) {
      case "guest.disabled":
        return t("disabled");
      case "guest.daily_budget_exceeded":
        return t("budgetExhausted");
      case "guest.global_budget_exceeded":
        return t("globalBudgetExhausted");
      case "guest.quota_exceeded":
        return t("quotaExhausted");
      case "guest.rate_limited":
        return t("rateLimited");
      case "guest.concurrency_limited":
        return t("concurrencyLimited");
      case "guest.model_not_allowed":
        return t("modelNotAllowed");
      case "guest.input_too_large":
        return t("inputTooLarge");
      case "guest.session_invalid":
      case "guest.session_required":
        return t("sessionExpired");
      default:
        return error.message || t("requestFailed");
    }
  }
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return t("requestFailed");
}

export function GuestChatPage() {
  const t = useTranslations("guest");
  const [config, setConfig] = React.useState<GuestConfigDTO | null>(null);
  const [me, setMe] = React.useState<GuestMeDTO | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [fatalError, setFatalError] = React.useState<string | null>(null);
  const [notice, setNotice] = React.useState<string | null>(null);
  const [model, setModel] = React.useState("");
  const [draft, setDraft] = React.useState("");
  const [messages, setMessages] = React.useState<GuestMessage[]>([]);
  const [sending, setSending] = React.useState(false);
  const [lastCost, setLastCost] = React.useState<{ nanousd: number; usd: string } | null>(null);
  const [guestId, setGuestId] = React.useState("");
  const [storeAvailable, setStoreAvailable] = React.useState(true);
  const abortRef = React.useRef<AbortController | null>(null);
  const sessionIDRef = React.useRef<string>(createSecureUUID());
  const generationRef = React.useRef(0);
  const sendingRef = React.useRef(false);

  const refreshMe = React.useCallback(async () => {
    try {
      const snapshot = await getGuestMe();
      setMe(snapshot);
    } catch (error) {
      if (error instanceof GuestApiError && error.status === 401) {
        setFatalError(t("sessionExpired"));
      }
    }
  }, [t]);

  React.useEffect(() => {
    let active = true;
    void (async () => {
      try {
        const nextConfig = await getGuestConfig();
        if (!active) {
          return;
        }
        setConfig(nextConfig);
        if (!nextConfig.enabled) {
          setFatalError(t("disabled"));
          return;
        }
        const session = await createGuestSession();
        const snapshot = await getGuestMe();
        if (!active) {
          return;
        }
        setGuestId(session.guestId);
        setMe(snapshot);
        const firstModel = nextConfig.models[0]?.platformModelName ?? "";
        setModel(firstModel);
        if (nextConfig.models.length === 0) {
          setNotice(t("noModels"));
        }
        const available = await isGuestChatStoreAvailable();
        if (!active) {
          return;
        }
        setStoreAvailable(available);
        if (!available) {
          return;
        }
        const cached = await loadGuestChat();
        if (!active || !cached || cached.messages.length === 0) {
          return;
        }
        const lastIndex = cached.messages.length - 1;
        setMessages(
          cached.messages.map((item, index) => {
            const interrupted = cached.interrupted && index === lastIndex && item.role === "assistant";
            return {
              id: item.id,
              role: item.role,
              content: item.content,
              failed: interrupted,
              errorText: interrupted ? t("interrupted") : undefined,
            } satisfies GuestMessage;
          }),
        );
        if (cached.model) {
          setModel(cached.model);
        }
        if (cached.sessionID) {
          sessionIDRef.current = cached.sessionID;
        }
        if (cached.interrupted) {
          setNotice(t("interrupted"));
        }
      } catch (error) {
        if (active) {
          setFatalError(resolveErrorMessage(error, t));
        }
      } finally {
        if (active) {
          setLoading(false);
        }
      }
    })();
    return () => {
      active = false;
      abortRef.current?.abort();
    };
  }, [t]);

  // 节流保存：消息 / 模型变化后 500ms 落库一次；流式分片不会逐片写库。
  React.useEffect(() => {
    if (!guestId || !storeAvailable) {
      return;
    }
    const timer = window.setTimeout(() => {
      const at = Date.now();
      const stored: GuestStoredMessage[] = messages.map((item) => ({
        id: item.id,
        role: item.role,
        content: item.content,
        at,
        costUSD: item.costUSD,
      }));
      void saveGuestChat({
        guestId,
        sessionID: sessionIDRef.current,
        model,
        messages: stored,
        interrupted: sendingRef.current,
      }).then((saved) => {
        if (saved) {
          notifyGuestChatChanged("update");
        }
      });
    }, 500);
    return () => window.clearTimeout(timer);
  }, [guestId, messages, model, storeAvailable]);

  // 多标签页：清空同步清空；更新在空闲时重新加载，生成中不覆盖本地内容。
  React.useEffect(() => {
    if (!storeAvailable) {
      return;
    }
    return subscribeGuestChatChanges((event) => {
      if (event.type === "clear") {
        generationRef.current += 1;
        setMessages([]);
        setLastCost(null);
        setNotice(null);
        return;
      }
      if (sendingRef.current) {
        return;
      }
      void loadGuestChat().then((snapshot) => {
        if (!snapshot) {
          return;
        }
        setMessages(
          snapshot.messages.map((item) => ({
            id: item.id,
            role: item.role,
            content: item.content,
            costUSD: item.costUSD,
          } satisfies GuestMessage)),
        );
      });
    });
  }, [storeAvailable]);

  const pushAssistantDelta = React.useCallback((messageID: string, delta: string) => {
    setMessages((current) =>
      current.map((item) =>
        item.id === messageID ? { ...item, content: item.content + delta, streaming: true } : item,
      ),
    );
  }, []);

  const runTurn = React.useCallback(
    async (history: GuestMessage[], assistantID: string, userContent: string) => {
      if (!model) {
        setNotice(t("selectModel"));
        return;
      }
      setSending(true);
      sendingRef.current = true;
      setNotice(null);
      const controller = new AbortController();
      abortRef.current = controller;
      const generation = generationRef.current;
      let usageCost: number | undefined;
      try {
        const payloadMessages = [
          ...history.map((item) => ({ role: item.role, content: item.content })),
          { role: "user" as const, content: userContent },
        ];
        await streamGuestChatMessage(
          {
            sessionID: sessionIDRef.current,
            clientRunID: createSecureUUID(),
            model,
            messages: payloadMessages,
          },
          {
            onDelta: (delta) => {
              if (generationRef.current !== generation) {
                return;
              }
              pushAssistantDelta(assistantID, delta);
            },
            onUsage: (event) => {
              const input = Number(event.input_tokens ?? 0);
              const output = Number(event.output_tokens ?? 0);
              usageCost = input + output;
            },
            onError: (event) => {
              const reason = event.message || t("requestFailed");
              setNotice(reason);
              setMessages((current) =>
                current.map((item) =>
                  item.id === assistantID
                    ? { ...item, streaming: false, failed: true, errorText: reason }
                    : item,
                ),
              );
            },
            onCompleted: () => {
              setMessages((current) =>
                current.map((item) =>
                  item.id === assistantID ? { ...item, streaming: false } : item,
                ),
              );
            },
          },
          { signal: controller.signal },
        );
        if (usageCost !== undefined) {
          setLastCost({ nanousd: usageCost, usd: "" });
        }
        await refreshMe();
      } catch (error) {
        if (generationRef.current !== generation) {
          return;
        }
        if (controller.signal.aborted) {
          setMessages((current) =>
            current.map((item) =>
              item.id === assistantID ? { ...item, streaming: false } : item,
            ),
          );
        } else {
          const reason = resolveErrorMessage(error, t);
          setNotice(reason);
          setMessages((current) =>
            current.map((item) =>
              item.id === assistantID
                ? { ...item, streaming: false, failed: true, errorText: reason }
                : item,
            ),
          );
        }
      } finally {
        abortRef.current = null;
        sendingRef.current = false;
        setSending(false);
      }
    },
    [model, pushAssistantDelta, refreshMe, t],
  );

  const handleSend = React.useCallback(async () => {
    const content = draft.trim();
    if (!content || sending) {
      return;
    }
    // 与后端一致的粗略口径：中日韩约 0.7 token/字。后端仍是权威校验，
    // 这里提前拦截是为了避免"请求被拒绝但用户只看到一个空气泡"。
    const tokenLimit = config?.limits.inputTokenLimit ?? 0;
    if (tokenLimit > 0 && Math.ceil(content.length * 0.7) > tokenLimit) {
      setNotice(t("inputTooLarge", { limit: tokenLimit }));
      return;
    }
    const userMessage: GuestMessage = { id: createSecureUUID(), role: "user", content };
    const assistantMessage: GuestMessage = {
      id: createSecureUUID(),
      role: "assistant",
      content: "",
      streaming: true,
    };
    const history = messages.filter((item) => !item.failed);
    setMessages([...messages, userMessage, assistantMessage]);
    setDraft("");
    await runTurn(history, assistantMessage.id, content);
  }, [config?.limits.inputTokenLimit, draft, messages, runTurn, sending, t]);

  const handleStop = React.useCallback(() => {
    abortRef.current?.abort();
  }, []);

  const handleRegenerate = React.useCallback(async () => {
    if (sending) {
      return;
    }
    const lastUserIndex = [...messages].reverse().findIndex((item) => item.role === "user");
    if (lastUserIndex < 0) {
      return;
    }
    const index = messages.length - 1 - lastUserIndex;
    const userMessage = messages[index];
    const history = messages.slice(0, index);
    const assistantMessage: GuestMessage = {
      id: createSecureUUID(),
      role: "assistant",
      content: "",
      streaming: true,
    };
    setMessages([...history, userMessage, assistantMessage]);
    await runTurn(history, assistantMessage.id, userMessage.content);
  }, [messages, runTurn, sending]);

  const handleNewChat = React.useCallback(() => {
    generationRef.current += 1;
    abortRef.current?.abort();
    sessionIDRef.current = createSecureUUID();
    setMessages([]);
    setNotice(null);
    setLastCost(null);
    void clearGuestChat().then(() => notifyGuestChatChanged("clear"));
  }, []);

  if (loading) {
    return (
      <main className="flex min-h-screen items-center justify-center gap-2 text-sm text-muted-foreground">
        <Loader2 className="size-4 animate-spin" />
        {t("loading")}
      </main>
    );
  }

  if (fatalError) {
    return (
      <main className="flex min-h-screen flex-col items-center justify-center gap-4 px-6 text-center">
        <p className="max-w-md text-sm text-muted-foreground">{fatalError}</p>
        <div className="flex gap-3">
          <Button asChild variant="secondary">
            <Link href="/login">{t("backToLogin")}</Link>
          </Button>
          <Button asChild>
            <Link href="/register">{t("register")}</Link>
          </Button>
        </div>
      </main>
    );
  }

  const models: GuestModelDTO[] = config?.models ?? [];
  const perVisitor = me?.perVisitor;

  return (
    <main className="mx-auto flex h-full min-h-screen w-full max-w-3xl flex-col gap-3 px-4 py-4">
      <header className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-base font-semibold text-foreground">{t("title")}</h1>
          <p className="text-xs text-muted-foreground">
            {storeAvailable ? t("storageHint") : t("cacheUnavailable")}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Link href="/login" className="text-xs font-medium text-muted-foreground hover:text-foreground">
            {t("backToLogin")}
          </Link>
          <Button type="button" size="sm" variant="secondary" onClick={handleNewChat}>
            <Trash2 className="mr-1 size-3.5" />
            {t("newChat")}
          </Button>
        </div>
      </header>

      {perVisitor ? (
        <section className="grid grid-cols-2 gap-2 rounded-md border border-border/60 bg-muted/30 px-3 py-2 text-xs text-muted-foreground sm:grid-cols-4">
          <span>{t("remainingBudget", { amount: `$${perVisitor.availableUSD}` })}</span>
          <span>{t("usedBudget", { amount: `$${perVisitor.settledUSD}` })}</span>
          <span>
            {t("usedRequests", {
              used: perVisitor.requestCount,
              total: me?.limits.dailyGenerationLimit ?? 0,
            })}
          </span>
          <span>{t("resetAt", { time: new Date(me?.resetAt ?? "").toLocaleString() })}</span>
        </section>
      ) : null}

      {notice ? (
        <p className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs text-destructive">
          {notice}
        </p>
      ) : null}

      <section className="min-h-0 flex-1 space-y-3 overflow-y-auto rounded-md border border-border/60 p-3">
        {messages.length === 0 ? (
          <p className="py-10 text-center text-sm text-muted-foreground">{t("empty")}</p>
        ) : null}
        {messages.map((message) => (
          <div
            key={message.id}
            className={cn(
              "max-w-[85%] break-words rounded-md px-3 py-2 text-sm leading-6",
              message.role === "user"
                ? "ml-auto whitespace-pre-wrap bg-foreground text-background"
                : "mr-auto bg-muted text-foreground",
            )}
          >
            {message.content ? (
              message.role === "assistant" ? (
                <StreamdownRender content={message.content} streaming={Boolean(message.streaming)} />
              ) : (
                message.content
              )
            ) : message.streaming ? (
              t("thinking")
            ) : message.errorText ? (
              <span className="text-destructive">{message.errorText}</span>
            ) : (
              ""
            )}
          </div>
        ))}
      </section>

      {lastCost ? <p className="text-xs text-muted-foreground">{t("lastCost")}</p> : null}

      <footer className="space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <select
            className="h-9 rounded-md border border-input bg-background px-2 text-sm"
            value={model}
            onChange={(event) => setModel(event.target.value)}
            disabled={sending}
          >
            {models.length === 0 ? <option value="">{t("noModels")}</option> : null}
            {models.map((item) => (
              <option key={item.platformModelName} value={item.platformModelName}>
                {item.platformModelName}
              </option>
            ))}
          </select>
          <div className="ml-auto flex gap-2">
            <Button type="button" size="sm" variant="secondary" onClick={handleRegenerate} disabled={sending || messages.length === 0}>
              <RefreshCw className="mr-1 size-3.5" />
              {t("regenerate")}
            </Button>
            {sending ? (
              <Button type="button" size="sm" variant="secondary" onClick={handleStop}>
                <Square className="mr-1 size-3.5" />
                {t("stop")}
              </Button>
            ) : null}
          </div>
        </div>
        <div className="flex items-end gap-2">
          <Textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder={t("placeholder")}
            className="min-h-[76px] flex-1 resize-none"
            disabled={sending}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey) {
                event.preventDefault();
                void handleSend();
              }
            }}
          />
          <Button type="button" onClick={() => void handleSend()} disabled={sending || draft.trim() === ""}>
            <Send className="mr-1 size-4" />
            {t("send")}
          </Button>
        </div>
      </footer>
    </main>
  );
}
