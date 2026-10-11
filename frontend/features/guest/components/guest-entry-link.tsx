"use client";

import * as React from "react";
import { useTranslations } from "next-intl";
import Link from "next/link";

import { getGuestConfig } from "@/shared/api/guest";

// GuestEntryLink 只在后台打开游客开关时显示登录页入口。
export function GuestEntryLink() {
  const t = useTranslations("guest");
  const [enabled, setEnabled] = React.useState(false);

  React.useEffect(() => {
    let active = true;
    void getGuestConfig()
      .then((config) => {
        if (active) {
          setEnabled(Boolean(config.enabled));
        }
      })
      .catch(() => undefined);
    return () => {
      active = false;
    };
  }, []);

  if (!enabled) {
    return null;
  }

  return (
    <div className="mt-6 text-center">
      <Link
        href="/guest"
        className="inline-flex w-full items-center justify-center rounded-md border border-border/70 px-4 py-2 text-sm font-semibold text-foreground transition-colors hover:bg-muted/60"
      >
        {t("entry")}
      </Link>
      <p className="mt-2 text-xs leading-5 text-muted-foreground">{t("entryHint")}</p>
      <p className="mt-1 text-xs leading-5 text-muted-foreground/80">{t("entryLimitation")}</p>
    </div>
  );
}
