import type { BrowserName } from "./browser";
import type { ConnectionEvents } from "./connection-events";
import { inboundInterval, type ConnectivitySummary, type InboundVideoStat } from "./stats";

export interface ConnectivityAggregate {
  samples: number;
  /** Last seen values; the *Seen lists keep every value met during the phase. */
  protocol: ConnectivitySummary["protocol"];
  candidateType: ConnectivitySummary["candidateType"];
  protocolsSeen: string[];
  candidateTypesSeen: string[];
  avgRttMs: number | null;
  minInboundFps: number | null;
  avgInboundFps: number | null;
  inboundStreamSamples: number;
  inboundStallSamples: number;
  /** Over stream samples that had frames (fps > 0). */
  minInboundHeight: number | null;
  maxInboundHeight: number | null;
  /** Per stream, per interval between two samples of that stream. */
  avgInboundKbps: number | null;
  minInboundKbps: number | null;
  /** Worst single interval. */
  maxLossPercent: number | null;
  /** Packet-weighted over all intervals: lost / (lost + received). */
  avgLossPercent: number | null;
  maxOutboundFps: number | null;
  outboundLimitations: string[];
}

// Loops, not Math.min(...xs): a long session has more values than the call stack allows arguments.
function minOf(xs: number[]): number | null {
  let m: number | null = null;
  for (const x of xs) if (m === null || x < m) m = x;
  return m;
}
function maxOf(xs: number[]): number | null {
  let m: number | null = null;
  for (const x of xs) if (m === null || x > m) m = x;
  return m;
}
function avg(xs: number[]): number | null {
  let sum = 0;
  for (const x of xs) sum += x;
  return xs.length ? sum / xs.length : null;
}
const rounded = (n: number | null): number | null => (n === null ? null : Math.round(n));
const rounded1 = (n: number | null): number | null => (n === null ? null : Math.round(n * 10) / 10);
const seen = <T extends string>(xs: T[]): T[] => [...new Set(xs.filter((x) => x !== "unknown"))];

export function aggregateSamples(samples: ConnectivitySummary[]): ConnectivityAggregate {
  const last = samples[samples.length - 1];
  const fps: number[] = [];
  const heights: number[] = [];
  const kbps: number[] = [];
  const losses: number[] = [];
  let lostTotal = 0;
  let packetsTotal = 0;
  let streamSamples = 0;
  let stallSamples = 0;
  const prev = new Map<string, InboundVideoStat>();

  for (const sample of samples) {
    for (const i of sample.inbound) {
      streamSamples++;
      if (i.fps > 0) {
        fps.push(i.fps);
        heights.push(i.height);
      } else {
        stallSamples++;
      }
      const before = prev.get(i.trackId);
      if (before) {
        const interval = inboundInterval(before, i);
        if (interval.kbps !== null) kbps.push(interval.kbps);
        if (interval.lossPercent !== null) losses.push(interval.lossPercent);
        lostTotal += interval.lost;
        packetsTotal += interval.lost + interval.received;
      }
      prev.set(i.trackId, i);
    }
  }

  const rtts = samples.flatMap((s) => (s.rttMs === null ? [] : [s.rttMs]));
  const outFps = samples.flatMap((s) => (s.outbound ? [s.outbound.fps] : []));
  const limits = samples.flatMap((s) => (s.outbound ? [s.outbound.qualityLimitation] : []));

  return {
    samples: samples.length,
    protocol: last?.protocol ?? "unknown",
    candidateType: last?.candidateType ?? "unknown",
    protocolsSeen: seen(samples.map((s) => s.protocol)),
    candidateTypesSeen: seen(samples.map((s) => s.candidateType)),
    avgRttMs: rounded(avg(rtts)),
    minInboundFps: minOf(fps),
    avgInboundFps: rounded(avg(fps)),
    inboundStreamSamples: streamSamples,
    inboundStallSamples: stallSamples,
    minInboundHeight: minOf(heights),
    maxInboundHeight: maxOf(heights),
    avgInboundKbps: rounded(avg(kbps)),
    minInboundKbps: rounded(minOf(kbps)),
    maxLossPercent: rounded1(maxOf(losses)),
    avgLossPercent: packetsTotal > 0 ? rounded1((lostTotal / packetsTotal) * 100) : null,
    maxOutboundFps: maxOf(outFps),
    outboundLimitations: [...new Set(limits.filter((l) => l !== "none"))],
  };
}

export interface ReportMeta {
  name: string;
  browser: BrowserName;
  userAgent: string;
  startedAt: Date;
  /** End of the measured phase (when the report is copied). */
  now: Date;
}

export function formatReport(meta: ReportMeta, agg: ConnectivityAggregate, events: ConnectionEvents): string {
  return JSON.stringify(
    {
      name: meta.name,
      browser: meta.browser,
      userAgent: meta.userAgent,
      startedAt: meta.startedAt.toISOString(),
      durationSec: Math.max(0, Math.round((meta.now.getTime() - meta.startedAt.getTime()) / 1000)),
      ...agg,
      ...events,
    },
    null,
    2,
  );
}
