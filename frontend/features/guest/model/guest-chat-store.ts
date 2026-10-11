"use client";

// 游客对话的浏览器端存储：带 schemaVersion 的 IndexedDB，24 小时过期，失败降级为内存。
// 只保存游客自己的对话正文；不保存任何凭证、API Key 或可用于修改服务器额度的数据。

export const GUEST_CHAT_SCHEMA_VERSION = 1;
export const GUEST_CHAT_TTL_MS = 24 * 60 * 60 * 1000;
export const GUEST_CHAT_MAX_MESSAGES = 200;
export const GUEST_CHAT_MAX_BYTES = 5 * 1024 * 1024;

const DB_NAME = "ivano_guest_chat";
const STORE_NAME = "sessions";
const RECORD_ID = "current";
const CHANNEL_NAME = "ivano_guest_chat";

export type GuestStoredMessage = {
  id: string;
  role: "user" | "assistant";
  content: string;
  at: number;
  costUSD?: string;
};

export type GuestChatSnapshot = {
  schemaVersion: number;
  guestId: string;
  sessionID: string;
  model: string;
  messages: GuestStoredMessage[];
  updatedAt: number;
  expiresAt: number;
  // interrupted 为真表示保存时仍在生成：恢复后只提示，不自动重发。
  interrupted: boolean;
};

type GuestChatRecord = GuestChatSnapshot & { id: string };

export type GuestChatChangeEvent = { type: "update" | "clear"; at: number };

let channel: BroadcastChannel | null = null;

function broadcastChannel(): BroadcastChannel | null {
  if (typeof window === "undefined" || typeof BroadcastChannel === "undefined") {
    return null;
  }
  channel ??= new BroadcastChannel(CHANNEL_NAME);
  return channel;
}

// notifyGuestChatChanged 通知其他标签页缓存已变化（清空会同步清空其他标签页）。
export function notifyGuestChatChanged(type: GuestChatChangeEvent["type"]): void {
  broadcastChannel()?.postMessage({ type, at: Date.now() } satisfies GuestChatChangeEvent);
}

// subscribeGuestChatChanges 订阅其他标签页的缓存变化。
export function subscribeGuestChatChanges(
  listener: (event: GuestChatChangeEvent) => void,
): () => void {
  const bus = broadcastChannel();
  if (!bus) {
    return () => undefined;
  }
  const handler = (event: MessageEvent<GuestChatChangeEvent>) => {
    if (event.data && (event.data.type === "update" || event.data.type === "clear")) {
      listener(event.data);
    }
  };
  bus.addEventListener("message", handler);
  return () => bus.removeEventListener("message", handler);
}

function openDatabase(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    if (typeof indexedDB === "undefined") {
      reject(new Error("indexeddb unavailable"));
      return;
    }
    const request = indexedDB.open(DB_NAME, GUEST_CHAT_SCHEMA_VERSION);
    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        db.createObjectStore(STORE_NAME, { keyPath: "id" });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("indexeddb open failed"));
    request.onblocked = () => reject(new Error("indexeddb blocked"));
  });
}

function runTransaction<T>(
  mode: IDBTransactionMode,
  work: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  return openDatabase().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const transaction = db.transaction(STORE_NAME, mode);
        const request = work(transaction.objectStore(STORE_NAME));
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error ?? new Error("indexeddb request failed"));
        transaction.oncomplete = () => db.close();
        transaction.onabort = () => {
          db.close();
          reject(transaction.error ?? new Error("indexeddb transaction aborted"));
        };
      }),
  );
}

// isGuestChatStoreAvailable 判断当前浏览器能否保存临时对话（用于降级提示）。
export async function isGuestChatStoreAvailable(): Promise<boolean> {
  try {
    const db = await openDatabase();
    db.close();
    return true;
  } catch {
    return false;
  }
}

// trimGuestMessages 先按条数、再按总字节裁剪，保证只保留最近内容。
export function trimGuestMessages(messages: GuestStoredMessage[]): GuestStoredMessage[] {
  let results = messages.slice(-GUEST_CHAT_MAX_MESSAGES);
  let total = results.reduce((sum, item) => sum + item.content.length + (item.id?.length ?? 0) + 32, 0);
  while (results.length > 1 && total > GUEST_CHAT_MAX_BYTES) {
    const removed = results.shift();
    total -= (removed?.content.length ?? 0) + (removed?.id?.length ?? 0) + 32;
  }
  if (results.length === 1 && total > GUEST_CHAT_MAX_BYTES) {
    const only = results[0];
    const keep = Math.max(0, GUEST_CHAT_MAX_BYTES - 64);
    results = [{ ...only, content: only.content.slice(-keep) }];
  }
  return results;
}

// loadGuestChat 读取缓存；过期、版本不符或已损坏时返回 null（并清理）。
export async function loadGuestChat(now: number = Date.now()): Promise<GuestChatSnapshot | null> {
  let record: GuestChatRecord | undefined;
  try {
    record = await runTransaction<GuestChatRecord | undefined>("readonly", (store) => store.get(RECORD_ID));
  } catch {
    return null;
  }
  if (!record || typeof record !== "object") {
    return null;
  }
  if (record.schemaVersion !== GUEST_CHAT_SCHEMA_VERSION || !record.expiresAt || record.expiresAt <= now) {
    await clearGuestChat();
    return null;
  }
  return {
    schemaVersion: record.schemaVersion,
    guestId: record.guestId ?? "",
    sessionID: record.sessionID ?? "",
    model: record.model ?? "",
    messages: Array.isArray(record.messages) ? record.messages : [],
    updatedAt: record.updatedAt ?? now,
    expiresAt: record.expiresAt,
    interrupted: Boolean(record.interrupted),
  };
}

export type SaveGuestChatInput = {
  guestId: string;
  sessionID: string;
  model: string;
  messages: GuestStoredMessage[];
  interrupted: boolean;
  now?: number;
};

// saveGuestChat 写入缓存并把过期时间顺延为“最后更新 + 24 小时”。
export async function saveGuestChat(input: SaveGuestChatInput): Promise<boolean> {
  const now = input.now ?? Date.now();
  const record: GuestChatRecord = {
    id: RECORD_ID,
    schemaVersion: GUEST_CHAT_SCHEMA_VERSION,
    guestId: input.guestId,
    sessionID: input.sessionID,
    model: input.model,
    messages: trimGuestMessages(input.messages),
    updatedAt: now,
    expiresAt: now + GUEST_CHAT_TTL_MS,
    interrupted: input.interrupted,
  };
  try {
    await runTransaction("readwrite", (store) => store.put(record));
    return true;
  } catch {
    return false;
  }
}

// clearGuestChat 删除缓存（新建对话、清空、过期时调用）。
export async function clearGuestChat(): Promise<void> {
  try {
    await runTransaction("readwrite", (store) => store.delete(RECORD_ID));
  } catch {
    // 缓存不可用时无需处理：调用方会提示降级。
  }
}
