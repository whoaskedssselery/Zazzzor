import type { LinkStats } from "@/shared/lib";

type Stat = Record<string, unknown>;

interface Counters {
  at: number;
  bytesSent: number;
  bytesReceived: number;
  packetsReceived: number;
  packetsLost: number;
}

const num = (v: unknown): number | null => (typeof v === "number" && Number.isFinite(v) ? v : null);

const average = (values: number[]): number | null =>
  values.length === 0 ? null : values.reduce((a, b) => a + b, 0) / values.length;

/** The pair a peer connection sends over now, and the local end of it. */
const activePair = (report: RTCStatsReport): { pair: Stat; local: Stat | undefined } | null => {
  for (const s of report.values() as IterableIterator<Stat>) {
    if (s.type === "candidate-pair" && s.nominated === true && s.state === "succeeded") {
      return { pair: s, local: report.get(String(s.localCandidateId)) as Stat | undefined };
    }
  }
  return null;
};

/**
 * Reads the link's figures from the stats reports of a call's tracks (each
 * report carries its peer connection's candidate pair). Counters become
 * rates between one call and the next, so it is called at a steady pace.
 */
export const createLinkSampler = (): ((reports: RTCStatsReport[]) => LinkStats) => {
  let last: Counters | null = null;
  return (reports) => {
    const pairs = new Map<string, { pair: Stat; local: Stat | undefined }>();
    const rtts: number[] = [];
    const jitters: number[] = [];
    const lossOut: number[] = [];
    let packetsReceived = 0;
    let packetsLost = 0;
    let at = 0;
    for (const report of reports) {
      const active = activePair(report);
      // Tracks on one peer connection share its pair: count its bytes once.
      if (active) pairs.set(String(active.pair.id), active);
      for (const s of report.values() as IterableIterator<Stat>) {
        if (s.type === "inbound-rtp" && s.kind === "audio") {
          packetsReceived += num(s.packetsReceived) ?? 0;
          packetsLost += num(s.packetsLost) ?? 0;
          const jitter = num(s.jitter);
          if (jitter !== null) jitters.push(jitter * 1000);
        }
        if (s.type === "remote-inbound-rtp" && s.kind === "audio") {
          const lost = num(s.fractionLost);
          if (lost !== null) lossOut.push(lost * 100);
        }
      }
    }
    let bytesSent = 0;
    let bytesReceived = 0;
    let route: LinkStats["route"] = null;
    for (const { pair, local } of pairs.values()) {
      const rtt = num(pair.currentRoundTripTime);
      if (rtt !== null) rtts.push(rtt * 1000);
      bytesSent += num(pair.bytesSent) ?? 0;
      bytesReceived += num(pair.bytesReceived) ?? 0;
      at = Math.max(at, num(pair.timestamp) ?? 0);
      // The worst route wins: one relayed connection is what to know about.
      const here =
        local?.candidateType === "relay" ? "relay" : local?.protocol === "tcp" ? "tcp" : local ? "udp" : null;
      if (here === "relay" || (here === "tcp" && route !== "relay") || (here === "udp" && route === null)) route = here;
    }

    const now: Counters = { at, bytesSent, bytesReceived, packetsReceived, packetsLost };
    const prev = last;
    last = pairs.size > 0 ? now : null;
    const seconds = prev && now.at > prev.at ? (now.at - prev.at) / 1000 : null;
    const rate = (bytes: number) => (seconds === null ? null : Math.round((bytes * 8) / 1000 / seconds));
    const received = prev ? now.packetsReceived - prev.packetsReceived : 0;
    const lost = prev ? now.packetsLost - prev.packetsLost : 0;
    const ping = average(rtts);
    const jitter = average(jitters);
    const out = average(lossOut);
    return {
      ping: ping === null ? null : Math.round(ping),
      jitter: jitter === null ? null : Math.round(jitter),
      lossIn: seconds === null || received + lost <= 0 ? null : (Math.max(0, lost) / (received + lost)) * 100,
      lossOut: out,
      upKbps: prev ? rate(now.bytesSent - prev.bytesSent) : null,
      downKbps: prev ? rate(now.bytesReceived - prev.bytesReceived) : null,
      route,
    };
  };
};
