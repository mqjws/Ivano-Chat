"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import { RefreshCw, Save } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { SpinnerLabel } from "@/components/ui/spinner";
import {
  getAdminGuestOverview,
  listAdminGuestRequests,
  type AdminGuestOverview,
  type AdminGuestRequestPage,
} from "@/features/admin/api/guest";
import { listAdminLLMModels } from "@/features/admin/api/llm";
import { patchAdminSettings } from "@/features/admin/api/settings";
import { resolveAdminErrorMessage } from "@/features/admin/utils/admin-error";
import { resolveAccessToken } from "@/shared/auth/resolve-access-token";
import {
  SettingsFieldInset,
  SettingsFieldItem,
  SettingsFieldList,
  SettingsFieldRow,
  SettingsPage,
  SettingsSection,
} from "@/shared/components/settings-layout";

type FormState = {
  enabled: boolean;
  modelWhitelist: string;
  perVisitorDailyBudgetUSD: string;
  globalDailyBudgetUSD: string;
  dailyGenerationLimit: string;
  rateLimitPerMinute: string;
  perVisitorConcurrency: string;
  globalConcurrency: string;
  inputTokenLimit: string;
  outputTokenLimit: string;
  budgetTimezone: string;
  billingRetentionDays: string;
};

const EMPTY_FORM: FormState = {
  enabled: false,
  modelWhitelist: "",
  perVisitorDailyBudgetUSD: "0.10",
  globalDailyBudgetUSD: "1.00",
  dailyGenerationLimit: "20",
  rateLimitPerMinute: "20",
  perVisitorConcurrency: "1",
  globalConcurrency: "2",
  inputTokenLimit: "8000",
  outputTokenLimit: "2048",
  budgetTimezone: "Europe/Berlin",
  billingRetentionDays: "30",
};

function formFromOverview(overview: AdminGuestOverview): FormState {
  return {
    enabled: overview.enabled,
    modelWhitelist: (overview.modelWhitelist ?? []).join(","),
    perVisitorDailyBudgetUSD: overview.limits.perVisitorDailyBudgetUSD,
    globalDailyBudgetUSD: overview.limits.globalDailyBudgetUSD,
    dailyGenerationLimit: String(overview.limits.dailyGenerationLimit),
    rateLimitPerMinute: String(overview.limits.rateLimitPerMinute),
    perVisitorConcurrency: String(overview.limits.perVisitorConcurrency),
    globalConcurrency: String(overview.limits.globalConcurrency),
    inputTokenLimit: String(overview.limits.inputTokenLimit),
    outputTokenLimit: String(overview.limits.outputTokenLimit),
    budgetTimezone: overview.budgetTimezone,
    billingRetentionDays: String(overview.limits.billingRetentionDays),
  };
}

export function AdminGuestPanel() {
  const t = useTranslations("adminGuest");
  const [form, setForm] = React.useState<FormState>(EMPTY_FORM);
  const [overview, setOverview] = React.useState<AdminGuestOverview | null>(null);
  const [requests, setRequests] = React.useState<AdminGuestRequestPage>({ total: 0, results: [] });
  const [platformModels, setPlatformModels] = React.useState<string[]>([]);
  const [page, setPage] = React.useState(1);
  const [loading, setLoading] = React.useState(true);
  const [saving, setSaving] = React.useState(false);

  const load = React.useCallback(async () => {
    const token = await resolveAccessToken();
    if (!token) {
      return;
    }
    setLoading(true);
    try {
      const [nextOverview, nextRequests, modelPage] = await Promise.all([
        getAdminGuestOverview(token),
        listAdminGuestRequests(token, { page, pageSize: 20 }),
        listAdminLLMModels(token, { page: 1, pageSize: 200, onlyActive: true }).catch(() => ({ total: 0, results: [] })),
      ]);
      setOverview(nextOverview);
      setForm(formFromOverview(nextOverview));
      setRequests(nextRequests);
      const names = (modelPage.results ?? [])
        .map((item) => String((item as { platformModelName?: string }).platformModelName ?? "").trim())
        .filter((name) => name !== "");
      setPlatformModels(Array.from(new Set(names)).sort((left, right) => left.localeCompare(right)));
    } catch (error) {
      toast.error(resolveAdminErrorMessage(error, t("loadFailed")));
    } finally {
      setLoading(false);
    }
  }, [page, t]);

  React.useEffect(() => {
    void load();
  }, [load]);

  const update = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => ({ ...current, [key]: value }));
  };

  const handleSave = async () => {
    const token = await resolveAccessToken();
    if (!token) {
      return;
    }
    setSaving(true);
    try {
      await patchAdminSettings(token, {
        items: [
          { namespace: "guest", key: "enabled", value: form.enabled ? "true" : "false" },
          { namespace: "guest", key: "model_whitelist", value: form.modelWhitelist.trim() },
          { namespace: "guest", key: "per_visitor_daily_budget_usd", value: form.perVisitorDailyBudgetUSD.trim() },
          { namespace: "guest", key: "global_daily_budget_usd", value: form.globalDailyBudgetUSD.trim() },
          { namespace: "guest", key: "daily_generation_limit", value: form.dailyGenerationLimit.trim() },
          { namespace: "guest", key: "rate_limit_per_minute", value: form.rateLimitPerMinute.trim() },
          { namespace: "guest", key: "per_visitor_concurrency", value: form.perVisitorConcurrency.trim() },
          { namespace: "guest", key: "global_concurrency", value: form.globalConcurrency.trim() },
          { namespace: "guest", key: "input_token_limit", value: form.inputTokenLimit.trim() },
          { namespace: "guest", key: "output_token_limit", value: form.outputTokenLimit.trim() },
          { namespace: "guest", key: "budget_timezone", value: form.budgetTimezone.trim() },
          { namespace: "guest", key: "billing_retention_days", value: form.billingRetentionDays.trim() },
        ],
      });
      toast.success(t("saved"));
      await load();
    } catch (error) {
      toast.error(resolveAdminErrorMessage(error, t("saveFailed")));
    } finally {
      setSaving(false);
    }
  };

  const totalPages = Math.max(1, Math.ceil(requests.total / 20));

  return (
    <SettingsPage>
      <SettingsSection title={t("title")}>
        <p className="px-0.5 text-sm text-muted-foreground">{t("description")}</p>
        <div className="space-y-4 px-0.5">
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <MetricCard label={t("enabledLabel")} value={overview?.enabled ? t("enabledOn") : t("enabledOff")} />
            <MetricCard label={t("globalUsed")} value={`$${overview?.global.settledUSD ?? "0"}`} />
            <MetricCard label={t("globalAvailable")} value={`$${overview?.global.availableUSD ?? "0"}`} />
            <MetricCard label={t("todayRequests")} value={String(overview?.global.requestCount ?? 0)} />
          </div>
          <p className="text-xs text-muted-foreground">
            {t("resetHint", {
              time: overview ? new Date(overview.resetAt).toLocaleString() : "-",
              timezone: overview?.budgetTimezone ?? "-",
            })}
          </p>

          <SettingsFieldList>
            <SettingsFieldItem index={0}><SettingsFieldRow title={t("fields.enabled")} description={t("fields.enabledHint")}>
              <SettingsFieldInset>
                <input
                  type="checkbox"
                  checked={form.enabled}
                  onChange={(event) => update("enabled", event.target.checked)}
                  className="size-4"
                />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={1}><SettingsFieldRow title={t("fields.modelWhitelist")} description={t("fields.modelWhitelistHint")}>
              <SettingsFieldInset>
                <WhitelistPicker
                  models={platformModels}
                  selected={selectedWhitelist(form.modelWhitelist)}
                  emptyLabel={t("fields.modelWhitelistEmpty")}
                  selectedLabel={(count) => t("fields.modelWhitelistSelected", { count })}
                  unknownLabel={(names) => t("fields.modelWhitelistUnknown", { names })}
                  onToggle={(name, checked) => update("modelWhitelist", toggleWhitelist(form.modelWhitelist, name, checked, platformModels))}
                />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={2}><SettingsFieldRow title={t("fields.perVisitorDailyBudget")}>
              <SettingsFieldInset>
                <Input value={form.perVisitorDailyBudgetUSD} onChange={(event) => update("perVisitorDailyBudgetUSD", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={3}><SettingsFieldRow title={t("fields.globalDailyBudget")}>
              <SettingsFieldInset>
                <Input value={form.globalDailyBudgetUSD} onChange={(event) => update("globalDailyBudgetUSD", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={4}><SettingsFieldRow title={t("fields.dailyGenerationLimit")}>
              <SettingsFieldInset>
                <Input value={form.dailyGenerationLimit} onChange={(event) => update("dailyGenerationLimit", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={5}><SettingsFieldRow title={t("fields.rateLimitPerMinute")} description={t("fields.rateLimitHint")}>
              <SettingsFieldInset>
                <Input value={form.rateLimitPerMinute} onChange={(event) => update("rateLimitPerMinute", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={6}><SettingsFieldRow title={t("fields.perVisitorConcurrency")}>
              <SettingsFieldInset>
                <Input value={form.perVisitorConcurrency} onChange={(event) => update("perVisitorConcurrency", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={7}><SettingsFieldRow title={t("fields.globalConcurrency")}>
              <SettingsFieldInset>
                <Input value={form.globalConcurrency} onChange={(event) => update("globalConcurrency", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={8}><SettingsFieldRow title={t("fields.inputTokenLimit")}>
              <SettingsFieldInset>
                <Input value={form.inputTokenLimit} onChange={(event) => update("inputTokenLimit", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={9}><SettingsFieldRow title={t("fields.outputTokenLimit")}>
              <SettingsFieldInset>
                <Input value={form.outputTokenLimit} onChange={(event) => update("outputTokenLimit", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={10}><SettingsFieldRow title={t("fields.budgetTimezone")} description={t("fields.budgetTimezoneHint")}>
              <SettingsFieldInset>
                <Input value={form.budgetTimezone} onChange={(event) => update("budgetTimezone", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
            <SettingsFieldItem index={11}><SettingsFieldRow title={t("fields.billingRetentionDays")}>
              <SettingsFieldInset>
                <Input value={form.billingRetentionDays} onChange={(event) => update("billingRetentionDays", event.target.value)} />
              </SettingsFieldInset>
            </SettingsFieldRow></SettingsFieldItem>
          </SettingsFieldList>

          <div className="flex items-center gap-2">
            <Button type="button" onClick={() => void handleSave()} disabled={saving}>
              {saving ? <SpinnerLabel>{t("saving")}</SpinnerLabel> : (<><Save className="mr-1 size-4" />{t("save")}</>)}
            </Button>
            <Button type="button" variant="secondary" onClick={() => void load()} disabled={loading}>
              <RefreshCw className="mr-1 size-4" />
              {t("refresh")}
            </Button>
          </div>
        </div>
      </SettingsSection>

      <SettingsSection title={t("guestUsageTitle")}>
        <p className="px-0.5 text-sm text-muted-foreground">{t("guestUsageHint")}</p>
        <SettingsFieldList>
          {(overview?.guests ?? []).map((item) => (
            <SettingsFieldItem key={item.sessionId}>
              <div className="flex items-center justify-between gap-3 text-sm">
                <span className="truncate font-mono text-xs text-muted-foreground">{item.guestId || `#${item.sessionId}`}</span>
                <span className="shrink-0">
                  {t("usageRow", { settled: item.settledUSD, reserved: item.reservedUSD, count: item.requestCount })}
                </span>
              </div>
            </SettingsFieldItem>
          ))}
          {(overview?.guests ?? []).length === 0 ? (
            <p className="px-2.5 py-3 text-sm text-muted-foreground">{t("noUsage")}</p>
          ) : null}
        </SettingsFieldList>
      </SettingsSection>

      <SettingsSection title={t("billsTitle")}>
        <p className="px-0.5 text-sm text-muted-foreground">{t("billsHint")}</p>
        <div className="space-y-2 px-0.5">
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-left text-sm">
              <thead className="text-xs text-muted-foreground">
                <tr>
                  <th className="py-2 pr-3">{t("columns.time")}</th>
                  <th className="py-2 pr-3">{t("columns.guest")}</th>
                  <th className="py-2 pr-3">{t("columns.model")}</th>
                  <th className="py-2 pr-3">{t("columns.status")}</th>
                  <th className="py-2 pr-3">{t("columns.settled")}</th>
                  <th className="py-2">{t("columns.billingRef")}</th>
                </tr>
              </thead>
              <tbody>
                {requests.results.map((item) => (
                  <tr key={item.requestId} className="border-t border-border/60">
                    <td className="py-2 pr-3 text-xs text-muted-foreground">{new Date(item.createdAt).toLocaleString()}</td>
                    <td className="py-2 pr-3 font-mono text-xs">{item.guestId ? item.guestId.slice(0, 8) : "-"}</td>
                    <td className="py-2 pr-3">{item.model}</td>
                    <td className="py-2 pr-3">{item.status}</td>
                    <td className="py-2 pr-3">${item.settledUSD}</td>
                    <td className="py-2 font-mono text-xs text-muted-foreground">{item.billingRefNo}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="flex items-center justify-between text-xs text-muted-foreground">
            <span>{t("totalRows", { total: requests.total })}</span>
            <span className="flex items-center gap-2">
              <Button type="button" size="sm" variant="secondary" disabled={page <= 1} onClick={() => setPage((current) => Math.max(1, current - 1))}>
                {t("prev")}
              </Button>
              <span>{page} / {totalPages}</span>
              <Button type="button" size="sm" variant="secondary" disabled={page >= totalPages} onClick={() => setPage((current) => current + 1)}>
                {t("next")}
              </Button>
            </span>
          </div>
          <p className="text-xs text-muted-foreground">{t("noContentNote")}</p>
        </div>
      </SettingsSection>
    </SettingsPage>
  );
}

function selectedWhitelist(raw: string): string[] {
  return raw
    .split(",")
    .map((item) => item.trim())
    .filter((item) => item !== "");
}

function toggleWhitelist(raw: string, name: string, checked: boolean, order: string[]): string {
  const current = new Set(selectedWhitelist(raw));
  if (checked) {
    current.add(name);
  } else {
    current.delete(name);
  }
  const ordered = [...order.filter((item) => current.has(item)), ...[...current].filter((item) => !order.includes(item))];
  return ordered.join(",");
}

function WhitelistPicker({
  models,
  selected,
  emptyLabel,
  selectedLabel,
  unknownLabel,
  onToggle,
}: {
  models: string[];
  selected: string[];
  emptyLabel: string;
  selectedLabel: (count: number) => string;
  unknownLabel: (names: string) => string;
  onToggle: (name: string, checked: boolean) => void;
}) {
  const selectedSet = new Set(selected);
  const unknown = selected.filter((name) => !models.includes(name));
  return (
    <div className="space-y-2">
      <div className="text-xs text-muted-foreground">
        {models.length === 0 ? emptyLabel : selectedLabel(selected.length)}
      </div>
      <div className="max-h-56 overflow-y-auto rounded-md border border-border/60">
        {models.map((name) => (
          <label key={name} className="flex cursor-pointer items-center gap-2 border-b border-border/40 px-2.5 py-1.5 text-sm last:border-b-0 hover:bg-muted/50">
            <input
              type="checkbox"
              className="size-4"
              checked={selectedSet.has(name)}
              onChange={(event) => onToggle(name, event.target.checked)}
            />
            <span className="truncate">{name}</span>
          </label>
        ))}
        {models.length === 0 ? (
          <p className="px-2.5 py-2 text-xs text-muted-foreground">{emptyLabel}</p>
        ) : null}
      </div>
      {unknown.length > 0 ? (
        <p className="text-xs text-amber-600 dark:text-amber-400">{unknownLabel(unknown.join(", "))}</p>
      ) : null}
    </div>
  );
}

function MetricCard({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-border/60 bg-muted/30 px-3 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="text-sm font-semibold text-foreground">{value}</div>
    </div>
  );
}
