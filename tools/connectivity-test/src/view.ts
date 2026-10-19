import { bitrateKbps, type ConnectivitySummary, type InboundVideoStat } from "./stats";
import type { ConnectivityAggregate } from "./report";

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

const fmt = (n: number | null, digits = 0, unit = ""): string => (n === null ? "нет данных" : `${n.toFixed(digits)}${unit}`);
const resolution = (w: number, hgt: number): string => (w > 0 ? `${w}×${hgt}` : "—");

function row(label: string, value: string, warn = false): HTMLElement {
  return h("tr", {}, h("th", { textContent: label }), h("td", { textContent: value, className: warn ? "warn" : "" }));
}

/** Строит таблицу живых показателей. Только текст, без innerHTML. */
export function renderLiveStats(
  summary: ConnectivitySummary | null,
  prevInbound: Map<string, InboundVideoStat>,
  agg: ConnectivityAggregate | null,
): HTMLElement {
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
    const prev = prevInbound.get(i.trackId);
    const stalled = i.fps === 0;
    const kbps = prev ? bitrateKbps(prev, i) : null;
    table.append(
      row(
        `Приём ${i.trackId.slice(0, 8)}`,
        `${resolution(i.width, i.height)} @ ${stalled ? "0 fps — замер" : `${i.fps.toFixed(0)} fps`}, ` +
          `потери ${i.lossPercent.toFixed(1)}%, ${fmt(kbps, 0, " кбит/с")}`,
        stalled,
      ),
    );
  }
  if (agg) {
    table.append(
      row("Макс. потери", fmt(agg.maxLossPercent, 1, "%")),
      row("Замеров с остановкой кадров", `${agg.inboundStallSamples} из ${agg.inboundStreamSamples}`, agg.inboundStallSamples > 0),
    );
  }
  return table;
}
