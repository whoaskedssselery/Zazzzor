import type { BrowserName } from "./browser";
import type { ConnectivitySummary } from "./stats";

export interface ConnectivityAggregate {
  samples: number;
  protocol: ConnectivitySummary["protocol"];
  candidateType: ConnectivitySummary["candidateType"];
  avgRttMs: number | null;
  minInboundFps: number | null;
  avgInboundFps: number | null;
  maxLossPercent: number;
  maxOutboundFps: number | null;
  outboundLimitations: string[];
}

const avg = (xs: number[]): number | null => (xs.length ? xs.reduce((a, b) => a + b, 0) / xs.length : null);
const rounded = (n: number | null): number | null => (n === null ? null : Math.round(n));

export function aggregateSamples(samples: ConnectivitySummary[]): ConnectivityAggregate {
  const last = samples[samples.length - 1];
  const inbound = samples.flatMap((s) => s.inbound);
  const fps = inbound.map((i) => i.fps).filter((f) => f > 0);
  const rtts = samples.flatMap((s) => (s.rttMs === null ? [] : [s.rttMs]));
  const outFps = samples.flatMap((s) => (s.outbound ? [s.outbound.fps] : []));
  const limits = samples.flatMap((s) => (s.outbound ? [s.outbound.qualityLimitation] : []));

  return {
    samples: samples.length,
    protocol: last?.protocol ?? "unknown",
    candidateType: last?.candidateType ?? "unknown",
    avgRttMs: rounded(avg(rtts)),
    minInboundFps: fps.length ? Math.min(...fps) : null,
    avgInboundFps: rounded(avg(fps)),
    maxLossPercent: inbound.reduce((m, i) => Math.max(m, i.lossPercent), 0),
    maxOutboundFps: outFps.length ? Math.max(...outFps) : null,
    outboundLimitations: [...new Set(limits.filter((l) => l !== "none"))],
  };
}

export function formatReport(
  meta: { name: string; browser: BrowserName; startedAt: Date },
  agg: ConnectivityAggregate,
): string {
  return JSON.stringify({ name: meta.name, browser: meta.browser, startedAt: meta.startedAt.toISOString(), ...agg }, null, 2);
}
