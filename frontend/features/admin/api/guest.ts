import { authedRequest } from "@/shared/api/authed-client";

export type AdminGuestLimits = {
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

export type AdminGuestUsage = {
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

export type AdminGuestUsageRow = {
  guestId: string;
  sessionId: number;
  settledNanousd: number;
  settledUSD: string;
  reservedNanousd: number;
  reservedUSD: string;
  requestCount: number;
};

export type AdminGuestOverview = {
  enabled: boolean;
  budgetTimezone: string;
  budgetDate: string;
  resetAt: string;
  modelWhitelist: string[];
  limits: AdminGuestLimits;
  global: AdminGuestUsage;
  guests: AdminGuestUsageRow[];
};

export type AdminGuestRequest = {
  requestId: string;
  guestId: string;
  model: string;
  status: string;
  budgetDate: string;
  reservedNanousd: number;
  settledNanousd: number;
  settledUSD: string;
  billingRefNo: string;
  createdAt: string;
  settledAt?: string;
};

export type AdminGuestRequestPage = {
  total: number;
  results: AdminGuestRequest[];
};

export async function getAdminGuestOverview(accessToken: string, signal?: AbortSignal): Promise<AdminGuestOverview> {
  return authedRequest<AdminGuestOverview>("/api/v1/admin/guest/overview", { accessToken, signal }, true);
}

export async function listAdminGuestRequests(
  accessToken: string,
  params: { page: number; pageSize: number; status?: string } = { page: 1, pageSize: 20 },
): Promise<AdminGuestRequestPage> {
  const query = new URLSearchParams({ page: String(params.page), page_size: String(params.pageSize) });
  if (params.status) {
    query.set("status", params.status);
  }
  return authedRequest<AdminGuestRequestPage>(`/api/v1/admin/guest/requests?${query.toString()}`, { accessToken }, true);
}
