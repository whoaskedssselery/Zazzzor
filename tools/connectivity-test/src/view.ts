import type { ConnectionEvents } from "./connection-events";
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

const fmt = (n: number | null, digits = 0, unit = ""): string =>
  n === null ? "нет данных" : `${n.toFixed(digits)}${unit}`;
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

/** Строит таблицу живых показателей. Только текст, без innerHTML. */
export function renderLiveStats(summary: ConnectivitySummary | null, ctx: LiveContext): HTMLElement {
  if (!summary) return h("p", { textContent: "Ждём первых данных…" });
  const table = h("table");
  table.append(
    row("Протокол", summary.protocol),
    row("Тип кандидата", summary.candidateType),
    row("RTT", fmt(summary.rttMs, 0, " мс")),
  );
  if (summary.outbound) {
    const o = summary.outbound;
    table.append(
      row("Отправка", `${resolution(o.width, o.height)} @ ${o.fps.toFixed(0)} fps`),
      row("Ограничение", o.qualityLimitation, o.qualityLimitation !== "none"),
    );
  }
  for (const i of summary.inbound) {
    const prev = ctx.prevInbound.get(i.trackId);
    const interval = prev ? inboundInterval(prev, i) : null;
    const stalled = i.fps === 0;
    table.append(
      row(
        `Приём от ${ctx.labels.get(i.trackId) ?? "участника"}`,
        `${resolution(i.width, i.height)} @ ${stalled ? "0 fps — замер" : `${i.fps.toFixed(0)} fps`}, ` +
          `${fmt(interval?.kbps ?? null, 0, " кбит/с")}, потери ${fmt(interval?.lossPercent ?? null, 1, "%")}`,
        stalled,
      ),
    );
  }
  if (ctx.agg) {
    table.append(
      row("Макс. потери за 2 с", fmt(ctx.agg.maxLossPercent, 1, "%")),
      row(
        "Замеров с остановкой кадров",
        `${ctx.agg.inboundStallSamples} из ${ctx.agg.inboundStreamSamples}`,
        ctx.agg.inboundStallSamples > 0,
      ),
    );
  }
  const e = ctx.events;
  table.append(
    row(
      "Обрывы связи",
      `отключений ${e.disconnects}, попыток переподключения ${e.reconnectAttempts}, переподключений ${e.reconnects}`,
      e.disconnects + e.reconnectAttempts > 0,
    ),
  );
  return table;
}
