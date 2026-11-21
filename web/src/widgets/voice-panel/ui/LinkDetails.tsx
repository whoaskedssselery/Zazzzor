import { useEffect, useState } from "react";
import { readLinkStats } from "@/features/voice-call";
import { t } from "@/shared/i18n";
import type { LinkStats } from "@/shared/lib";
import { cn } from "@/shared/ui";

/** How often the figures refresh while they are shown. */
export const LINK_SAMPLE_MS = 2000;

/** Above these voices start to break up: the figure turns amber. */
const PING_BAD = 150;
const LOSS_BAD = 2;

const ms = (v: number | null) => (v === null ? t("shell:link.unknown") : t("shell:link.ms", { value: v }));
const kbps = (v: number | null) => (v === null ? t("shell:link.unknown") : t("shell:link.kbps", { value: v }));
const percent = (v: number | null) =>
  v === null ? t("shell:link.unknown") : t("shell:link.percent", { value: v < 10 ? v.toFixed(1) : Math.round(v) });

/**
 * The call's link in figures, read every two seconds while shown: ping,
 * jitter, loss each way, the bitrates and the route to the voice server.
 */
export const LinkDetails = () => {
  const [stats, setStats] = useState<LinkStats | null>(null);
  useEffect(() => {
    let live = true;
    const read = () => {
      void readLinkStats().then((s) => {
        if (live) setStats(s);
      });
    };
    read();
    const timer = setInterval(read, LINK_SAMPLE_MS);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, []);

  const rows: { label: string; value: string; bad?: boolean }[] = [
    { label: t("shell:link.ping"), value: ms(stats?.ping ?? null), bad: (stats?.ping ?? 0) > PING_BAD },
    { label: t("shell:link.jitter"), value: ms(stats?.jitter ?? null) },
    { label: t("shell:link.lossIn"), value: percent(stats?.lossIn ?? null), bad: (stats?.lossIn ?? 0) > LOSS_BAD },
    { label: t("shell:link.lossOut"), value: percent(stats?.lossOut ?? null), bad: (stats?.lossOut ?? 0) > LOSS_BAD },
    { label: t("shell:link.upload"), value: kbps(stats?.upKbps ?? null) },
    { label: t("shell:link.download"), value: kbps(stats?.downKbps ?? null) },
    {
      label: t("shell:link.route"),
      value: stats?.route ? t(`shell:link.${stats.route}`) : t("shell:link.unknown"),
      bad: stats?.route === "relay",
    },
  ];
  return (
    <div className="w-64 p-3">
      <h3 className="text-sm font-semibold">{t("shell:link.title")}</h3>
      <dl className="mt-2 grid grid-cols-[1fr_auto] gap-x-4 gap-y-1 text-sm">
        {rows.map((r) => (
          <div key={r.label} className="contents">
            <dt className="text-fg-muted">{r.label}</dt>
            <dd className={cn("text-right tabular-nums", r.bad && "text-fg-warning")}>{r.value}</dd>
          </div>
        ))}
      </dl>
      <p className="mt-3 text-xs text-fg-muted">{t("shell:link.hint")}</p>
    </div>
  );
};
