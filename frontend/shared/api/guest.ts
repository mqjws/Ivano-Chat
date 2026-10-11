import { resolveApiBaseURL } from "@/shared/api/http-client";


export type GuestModelDTO = {
  platformModelName: string;
  vendor: string;
  vendorName: string;
  vendorIcon: string;
  displayGroupID?: number;
  displayGroupName: string;
  displayGroupIcon: string;
  kindsJSON: string;
  icon: string;
  capabilitiesJSON: string;
  description: string;
  sortOrder: number;
};

export type GuestLimitsDTO = {
  perVisitorDailyBudgetNanousd: number;
  perVisitorDailyBudgetUSD: string;
  globalDailyBudgetNanousd: number;
  globalDailyBudgetUSD: string;
  dailyGenerationLimit: number;
  rateLimitPerMinute: number;
  perVisitorConcurrency: number;
  globalConcurrency: number;
  inputTokenLimit: number;
  outputTokenLimit: number;
  billingRetentionDays: number;
};

export type GuestConfigDTO = {
  enabled: boolean;
  sessionTTLHours: number;
  budgetTimezone: string;
  budgetDate: string;
  resetAt: string;
  limits: GuestLimitsDTO;
  models: GuestModelDTO[];
};

export type GuestSessionDTO = {
  guestId: string;
  createdAt: string;
  expiresAt: string;
  restored: boolean;
};

export type GuestUsageDTO = {
  budgetNanousd: number;
  budgetUSD: string;
  settledNanousd: number;
  settledUSD: string;
  reservedNanousd: number;
  reservedUSD: string;
  consumedNanousd: number;
  availableNanousd: number;
  availableUSD: string;
  requestCount: number;
};

export type GuestMeDTO = {
  guestId: string;
  createdAt: string;
  expiresAt: string;
  budgetTimezone: string;
  budgetDate: string;
  resetAt: string;
  perVisitor: GuestUsageDTO;
  global: GuestUsageDTO;
  limits: GuestLimitsDTO;
};

export type GuestChatMessageDTO = {
  role: "user" | "assistant";
  content: string;
};

export type GuestStreamDeltaEvent = { type: "delta"; delta: string };
export type GuestStreamUsageEvent = {
  type: "usage";
  input_tokens?: number;
  output_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  reasoning_tokens?: number;
};
export type GuestStreamStatusEvent = { type: "process_update"; status?: string };
export type GuestStreamErrorEvent = {
  type: "error";
  status?: number;
  message?: string;
  errorCode?: string;
};
export type GuestStreamCompletedEvent = { type: "completed"; data?: unknown };

export type GuestStreamHandlers = {
  onDelta?: (delta: string) => void;
  onUsage?: (event: GuestStreamUsageEvent) => void;
  onStatus?: (status: string) => void;
  onError?: (event: GuestStreamErrorEvent) => void;
  onCompleted?: (event: GuestStreamCompletedEvent) => void;
};

export type GuestStreamOptions = {
  signal?: AbortSignal;
};

export class GuestApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(message: string, status: number, code = "") {
    super(message);
    this.name = "GuestApiError";
    this.status = status;
    this.code = code;
  }
}

async function readEnvelope<T>(response: Response): Promise<T> {
  let payload: { errorMsg?: string; errorCode?: string; data?: T } | null = null;
  try {
    payload = (await response.json()) as { errorMsg?: string; errorCode?: string; data?: T };
  } catch {
    payload = null;
  }
  if (!response.ok) {
    throw new GuestApiError(
      payload?.errorMsg || `request failed with status ${response.status}`,
      response.status,
      payload?.errorCode ?? "",
    );
  }
  return (payload?.data ?? ({} as T)) as T;
}

export async function getGuestConfig(): Promise<GuestConfigDTO> {
  const response = await fetch(`${resolveApiBaseURL()}/api/v1/guest/config`, {
    method: "GET",
    cache: "no-store",
    credentials: "include",
  });
  return readEnvelope<GuestConfigDTO>(response);
}

export async function createGuestSession(): Promise<GuestSessionDTO> {
  const response = await fetch(`${resolveApiBaseURL()}/api/v1/guest/session`, {
    method: "POST",
    cache: "no-store",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: "{}",
  });
  return readEnvelope<GuestSessionDTO>(response);
}

export async function getGuestMe(): Promise<GuestMeDTO> {
  const response = await fetch(`${resolveApiBaseURL()}/api/v1/guest/me`, {
    method: "GET",
    cache: "no-store",
    credentials: "include",
  });
  return readEnvelope<GuestMeDTO>(response);
}

export type GuestStreamPayload = {
  sessionID: string;
  clientRunID: string;
  model: string;
  messages: GuestChatMessageDTO[];
  options?: Record<string, unknown>;
};

export async function streamGuestChatMessage(
  payload: GuestStreamPayload,
  handlers: GuestStreamHandlers = {},
  options: GuestStreamOptions = {},
): Promise<void> {
  const response = await fetch(`${resolveApiBaseURL()}/api/v1/guest/messages/stream`, {
    method: "POST",
    cache: "no-store",
    credentials: "include",
    headers: { "Content-Type": "application/json", Accept: "application/x-ndjson" },
    body: JSON.stringify(payload),
    signal: options.signal,
  });
  if (!response.ok) {
    await readEnvelope(response);
    throw new GuestApiError("guest stream failed", response.status);
  }
  if (!response.body) {
    throw new GuestApiError("guest stream has no body", response.status);
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });
    let newlineIndex = buffer.indexOf("\n");
    while (newlineIndex >= 0) {
      const line = buffer.slice(0, newlineIndex).trim();
      buffer = buffer.slice(newlineIndex + 1);
      newlineIndex = buffer.indexOf("\n");
      if (line) {
        dispatchGuestStreamEvent(line, handlers);
      }
    }
  }
  const tail = buffer.trim();
  if (tail) {
    dispatchGuestStreamEvent(tail, handlers);
  }
}

function dispatchGuestStreamEvent(line: string, handlers: GuestStreamHandlers): void {
  let event: Record<string, unknown>;
  try {
    event = JSON.parse(line) as Record<string, unknown>;
  } catch {
    return;
  }
  switch (event.type) {
    case "delta":
      if (typeof event.delta === "string") {
        handlers.onDelta?.(event.delta);
      }
      return;
    case "usage":
      handlers.onUsage?.(event as GuestStreamUsageEvent);
      return;
    case "process_update":
      handlers.onStatus?.(typeof event.status === "string" ? event.status : "processing");
      return;
    case "error":
      handlers.onError?.(event as GuestStreamErrorEvent);
      return;
    case "completed":
      handlers.onCompleted?.(event as GuestStreamCompletedEvent);
      return;
    default:
      return;
  }
}

