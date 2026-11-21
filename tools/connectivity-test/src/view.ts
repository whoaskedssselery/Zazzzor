import type { ConnectionEvents } from "./connection-events";
import { type Language, translations } from "./locale";
import type { ConnectivityAggregate } from "./report";
import { type ConnectivitySummary, type InboundVideoStat, inboundInterval } from "./stats";

type Child = Node | string;

export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  props: Partial<Pick<HTMLElementTagNameMap[K], "className" | "textContent" | "id">> = {},
  ...children: Child[]
): HTMLElementTagNameMap[K] {
  const node = document.createElement(tag);
  Object.assign(node, props);
  node.append(...children);
  return node;
}


const resolution = (w: number, hgt: number): string => (w > 0 ? `${w}×${hgt}` : "—");

function row(label: string, value: string, warn = false): HTMLElement {
  return h("tr", {}, h("th", { textContent: label }), h("td", { textContent: value, className: warn ? "warn" : "" }));
}

export interface LiveContext {
  prevInbound: Map<string, InboundVideoStat>;
  agg: ConnectivityAggregate | null;
  events: ConnectionEvents;
  /** MediaStreamTrack id → identity of the participant who sends it. */
  labels: Map<string, string>;
}

/** Builds the live measurements table. */
export function renderLiveStats(summary: ConnectivitySummary | null, language: Language, ctx: LiveContext): HTMLElement {
  const text = translations[language];
  const fmt = (n: number | null, digits: number, unit: string): string =>
    n === null ? text.noData : `${n.toFixed(digits)}${unit}`;
  if (!summary) return h("p", { textContent: text.waiting });
  const table = h("table");
  table.append(
    row("Протокол", summary.protocol),
    row(text.candidateType, summary.candidateType),
    row("RTT", fmt(summary.rttMs, 0, text.ms)),
  );
  if (summary.outbound) {
    const o = summary.outbound;
    table.append(
      row(text.outbound, `${resolution(o.width, o.height)} @ ${o.fps.toFixed(0)} fps`),
      row(text.qualityLimit, o.qualityLimitation, o.qualityLimitation !== "none"),
    );
  }
  for (const i of summary.inbound) {
    const prev = ctx.prevInbound.get(i.trackId);
    const interval = prev ? inboundInterval(prev, i) : null;
    const stalled = i.fps === 0;
    table.append(
      row(
        text.receiveFrom + (ctx.labels.get(i.trackId) ?? text.participant),
        `${resolution(i.width, i.height)} @ ${stalled ? text.stalled : `${i.fps.toFixed(0)} fps`}, ` +
          `${fmt(interval?.kbps ?? null, 0, text.kbps)}, ${text.loss}${fmt(interval?.lossPercent ?? null, 1, "%")}`,
        stalled,
      ),
    );
  }
  if (ctx.agg) {
    table.append(
      row(text.maxLoss, fmt(ctx.agg.maxLossPercent, 1, "%")),
      row(
        text.stalledSamples,
        `${ctx.agg.inboundStallSamples}${text.of}${ctx.agg.inboundStreamSamples}`,
        ctx.agg.inboundStallSamples > 0,
      ),
    );
  }
  const e = ctx.events;
  table.append(
    row(
      text.connectionDrops,
      `${text.disconnects}${e.disconnects}, ${text.reconnectAttempts}${e.reconnectAttempts}, ${text.reconnects}${e.reconnects}`,
      e.disconnects + e.reconnectAttempts > 0,
    ),
  );
  return table;
}
