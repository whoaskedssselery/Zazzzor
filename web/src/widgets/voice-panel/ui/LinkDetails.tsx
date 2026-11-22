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

/** How many readings the chart keeps: two minutes at one every two seconds. */
const HISTORY = 60;

interface Point {
  ping: number | null;
  /** The worse of the two directions' loss, %. */
  loss: number | null;
}

const W = 240;
const H = 56;

/**
 * The last two minutes of the link: ping as a line (its scale grows with
 * it, from 100 ms), loss as bars from the bottom (full height at 10%).
 * Readings over the thresholds are amber, as the figures are.
 */
const LinkChart = ({ points }: { points: Point[] }) => {
  const pings = points.map((p) => p.ping).filter((v): v is number => v !== null);
  const top = Math.max(100, ...pings.map((v) => v * 1.2));
  const step = W / (HISTORY - 1);
  // The newest reading sits at the right edge.
  const x = (i: number) => W - (points.length - 1 - i) * step;
  const y = (v: number) => H - (v / top) * H;
  const line = points
    .map((p, i) => (p.ping === null ? null : `${x(i).toFixed(1)},${y(p.ping).toFixed(1)}`))
    .filter((v) => v !== null)
    .join(" ");
  const label =
    pings.length === 0
      ? t("shell:link.chartEmpty")
      : t("shell:link.chart", { min: Math.min(...pings), max: Math.max(...pings) });
  return (
    <figure className="mt-3">
      <svg
        role="img"
        aria-label={label}
        viewBox={`0 0 ${String(W)} ${String(H)}`}
        className="h-14 w-full overflow-visible"
      >
        <line x1={0} x2={W} y1={y(PING_BAD)} y2={y(PING_BAD)} className="stroke-border" strokeDasharray="3 3" />
        {points.map((p, i) =>
          p.loss !== null && p.loss > 0 ? (
            <rect
              // biome-ignore lint/suspicious/noArrayIndexKey: a reading's place in the window is its identity.
              key={i}
              x={x(i) - step / 2}
              width={Math.max(1, step - 1)}
              y={H - Math.min(1, p.loss / 10) * H}
              height={Math.min(1, p.loss / 10) * H}
              className={p.loss > LOSS_BAD ? "fill-(--text-warning)" : "fill-(--text-muted) opacity-50"}
            />
          ) : null,
        )}
        {line !== "" && (
          <polyline points={line} fill="none" strokeWidth={1.5} strokeLinejoin="round" className="stroke-(--accent)" />
        )}
      </svg>
      <figcaption className="mt-1 flex gap-3 text-xs text-fg-muted">
        <span className="flex items-center gap-1">
          <span aria-hidden className="inline-block h-0.5 w-3 bg-(--accent)" />
          {t("shell:link.ping")}
        </span>
        <span className="flex items-center gap-1">
          <span aria-hidden className="inline-block size-2 bg-(--text-warning)" />
          {t("shell:link.loss")}
        </span>
      </figcaption>
    </figure>
  );
};

/**
 * The call's link in figures, read every two seconds while shown: ping,
 * jitter, loss each way, the bitrates and the route to the voice server.
 */
export const LinkDetails = () => {
  const [stats, setStats] = useState<LinkStats | null>(null);
  const [points, setPoints] = useState<Point[]>([]);
  useEffect(() => {
    let live = true;
    const read = () => {
      void readLinkStats().then((s) => {
        if (!live) return;
        setStats(s);
        if (s === null) return;
        const losses = [s.lossIn, s.lossOut].filter((v): v is number => v !== null);
        const point = { ping: s.ping, loss: losses.length === 0 ? null : Math.max(...losses) };
        setPoints((old) => [...old, point].slice(-HISTORY));
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
      <LinkChart points={points} />
      <p className="mt-3 text-xs text-fg-muted">{t("shell:link.hint")}</p>
    </div>
  );
};
