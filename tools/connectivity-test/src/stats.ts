export type RTCStatsLike = { id: string; type: string; timestamp: number; [k: string]: unknown };

export interface VideoStat {
  width: number;
  height: number;
  fps: number;
}

/** Cumulative counters as getStats() reports them; per-interval values come from inboundInterval(). */
export interface InboundVideoStat extends VideoStat {
  trackId: string;
  packetsLost: number;
  packetsReceived: number;
  bytesReceived: number;
  timestamp: number;
}

export interface InboundInterval {
  /** null when no time passed or the byte counter went backwards (stream restarted). */
  kbps: number | null;
  lost: number;
  received: number;
  /** null when no packets arrived or were lost in the interval. */
  lossPercent: number | null;
}

export interface ConnectivitySummary {
  protocol: "udp" | "tcp" | "unknown";
  candidateType: "host" | "srflx" | "prflx" | "relay" | "unknown";
  rttMs: number | null;
  outbound: (VideoStat & { qualityLimitation: string }) | null;
  inbound: InboundVideoStat[];
}

const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);
const str = (v: unknown, fallback: string): string => (typeof v === "string" ? v : fallback);

const PROTOCOLS = ["udp", "tcp"] as const;
const CANDIDATE_TYPES = ["host", "srflx", "prflx", "relay"] as const;

export function summarizeStats(stats: RTCStatsLike[]): ConnectivitySummary {
  const byId = new Map(stats.map((s) => [s.id, s]));
  const selectedId = stats.find(
    (s) => s.type === "transport" && typeof s.selectedCandidatePairId === "string",
  )?.selectedCandidatePairId;
  const pair =
    (typeof selectedId === "string" ? byId.get(selectedId) : undefined) ??
    stats.find((s) => s.type === "candidate-pair" && s.state === "succeeded" && s.nominated === true);
  const local = pair ? byId.get(str(pair.localCandidateId, "")) : undefined;

  const protocol = PROTOCOLS.find((p) => p === local?.protocol) ?? "unknown";
  const candidateType = CANDIDATE_TYPES.find((t) => t === local?.candidateType) ?? "unknown";
  const rttMs =
    pair && typeof pair.currentRoundTripTime === "number" && Number.isFinite(pair.currentRoundTripTime)
      ? pair.currentRoundTripTime * 1000
      : null;

  let outbound: ConnectivitySummary["outbound"] = null;
  for (const s of stats) {
    if (s.type !== "outbound-rtp" || s.kind !== "video") continue;
    if (outbound && num(s.frameWidth) <= outbound.width) continue;
    outbound = {
      width: num(s.frameWidth),
      height: num(s.frameHeight),
      fps: num(s.framesPerSecond),
      qualityLimitation: str(s.qualityLimitationReason, "none"),
    };
  }

  const inbound: InboundVideoStat[] = stats
    .filter((s) => s.type === "inbound-rtp" && s.kind === "video")
    .map((s) => ({
      trackId: str(s.trackIdentifier, s.id),
      width: num(s.frameWidth),
      height: num(s.frameHeight),
      fps: num(s.framesPerSecond),
      // packetsLost may go negative (duplicated packets); that is not loss.
      packetsLost: Math.max(0, num(s.packetsLost)),
      packetsReceived: Math.max(0, num(s.packetsReceived)),
      bytesReceived: num(s.bytesReceived),
      timestamp: s.timestamp,
    }));

  return { protocol, candidateType, rttMs, outbound, inbound };
}

/** Bitrate and loss between two samples of the same stream. */
export function inboundInterval(prev: InboundVideoStat, next: InboundVideoStat): InboundInterval {
  const dtMs = next.timestamp - prev.timestamp;
  const bytes = next.bytesReceived - prev.bytesReceived;
  const lost = Math.max(0, next.packetsLost - prev.packetsLost);
  const received = Math.max(0, next.packetsReceived - prev.packetsReceived);
  const total = lost + received;
  return {
    kbps: dtMs > 0 && bytes >= 0 ? (bytes * 8) / dtMs : null,
    lost,
    received,
    lossPercent: total > 0 ? (lost / total) * 100 : null,
  };
}

/** Merges per-peer-connection summaries. With singlePeerConnection (livekit-client default) everything is on `pub`. */
export function mergeSummaries(pub: ConnectivitySummary | null, sub: ConnectivitySummary | null): ConnectivitySummary {
  const transport = [pub, sub].find((s) => s && s.protocol !== "unknown") ?? pub ?? sub;
  return {
    protocol: transport?.protocol ?? "unknown",
    candidateType: transport?.candidateType ?? "unknown",
    rttMs: transport?.rttMs ?? null,
    outbound: pub?.outbound ?? sub?.outbound ?? null,
    inbound: [...(pub?.inbound ?? []), ...(sub?.inbound ?? [])],
  };
}
