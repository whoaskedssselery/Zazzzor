import { summarizeStats, inboundInterval, mergeSummaries, type ConnectivitySummary, type InboundVideoStat, type RTCStatsLike } from "./stats";

const base = (o: object): RTCStatsLike => ({ id: "x", type: "x", timestamp: 1000, ...o });
const pair = (localId: string, extra: object = {}) =>
  base({ id: "cp", type: "candidate-pair", state: "succeeded", nominated: true, localCandidateId: localId, currentRoundTripTime: 0.042, ...extra });

test("udp host transport and rtt", () => {
  const s = summarizeStats([pair("lc"), base({ id: "lc", type: "local-candidate", protocol: "udp", candidateType: "srflx" })]);
  expect(s.protocol).toBe("udp");
  expect(s.candidateType).toBe("srflx");
  expect(s.rttMs).toBe(42);
});
test("reports tcp when UDP is blocked", () => {
  const s = summarizeStats([pair("lc"), base({ id: "lc", type: "local-candidate", protocol: "tcp", candidateType: "host" })]);
  expect(s.protocol).toBe("tcp");
});
test("reports relay candidate", () => {
  const s = summarizeStats([pair("lc"), base({ id: "lc", type: "local-candidate", protocol: "udp", candidateType: "relay" })]);
  expect(s.candidateType).toBe("relay");
});
test("ignores non-nominated pairs; unknown when none", () => {
  const s = summarizeStats([pair("lc", { nominated: false })]);
  expect(s).toMatchObject({ protocol: "unknown", candidateType: "unknown", rttMs: null });
});
test("outbound picks the largest simulcast layer", () => {
  const s = summarizeStats([
    base({ id: "o1", type: "outbound-rtp", kind: "video", frameWidth: 640, frameHeight: 360, framesPerSecond: 60, qualityLimitationReason: "none" }),
    base({ id: "o2", type: "outbound-rtp", kind: "video", frameWidth: 1920, frameHeight: 1080, framesPerSecond: 58, qualityLimitationReason: "bandwidth" }),
  ]);
  expect(s.outbound).toEqual({ width: 1920, height: 1080, fps: 58, qualityLimitation: "bandwidth" });
});
test("inbound keeps raw packet counters for per-interval loss", () => {
  const s = summarizeStats([
    base({ id: "i1", type: "inbound-rtp", kind: "video", trackIdentifier: "t1", frameWidth: 1920, frameHeight: 1080, framesPerSecond: 60, packetsLost: 2, packetsReceived: 98, bytesReceived: 5000 }),
    base({ id: "i2", type: "inbound-rtp", kind: "audio", packetsLost: 50, packetsReceived: 50 }),
  ]);
  expect(s.inbound).toEqual([{ trackId: "t1", width: 1920, height: 1080, fps: 60, packetsLost: 2, packetsReceived: 98, bytesReceived: 5000, timestamp: 1000 }]);
});
test("negative packetsLost clamps to 0", () => {
  const s = summarizeStats([base({ id: "i1", type: "inbound-rtp", kind: "video", trackIdentifier: "t1", packetsLost: -5, packetsReceived: 100 })]);
  expect(s.inbound[0].packetsLost).toBe(0);
});
test("NaN rtt gives null", () => {
  const s = summarizeStats([pair("lc", { currentRoundTripTime: NaN }), base({ id: "lc", type: "local-candidate", protocol: "udp", candidateType: "host" })]);
  expect(s.rttMs).toBeNull();
});
test("transport selectedCandidatePairId picks that pair", () => {
  const s = summarizeStats([
    pair("lc1", { id: "cp1" }),
    pair("lc2", { id: "cp2", currentRoundTripTime: 0.1 }),
    base({ id: "t", type: "transport", selectedCandidatePairId: "cp2" }),
    base({ id: "lc1", type: "local-candidate", protocol: "udp", candidateType: "host" }),
    base({ id: "lc2", type: "local-candidate", protocol: "tcp", candidateType: "relay" }),
  ]);
  expect(s).toMatchObject({ protocol: "tcp", candidateType: "relay", rttMs: 100 });
});

const stat = (o: Partial<InboundVideoStat> = {}): InboundVideoStat => ({
  trackId: "t", width: 1920, height: 1080, fps: 60, packetsLost: 0, packetsReceived: 0, bytesReceived: 0, timestamp: 0, ...o,
});

describe("inboundInterval", () => {
  test("bitrate in kbps from the byte delta", () => {
    expect(inboundInterval(stat(), stat({ bytesReceived: 1_000_000, timestamp: 1000 })).kbps).toBe(8000);
  });
  test("no time passed gives no bitrate", () => {
    expect(inboundInterval(stat(), stat({ bytesReceived: 10 })).kbps).toBeNull();
  });
  test("counter reset gives no bitrate instead of a fake zero", () => {
    expect(inboundInterval(stat({ bytesReceived: 5000 }), stat({ bytesReceived: 100, timestamp: 1000 })).kbps).toBeNull();
  });
  test("loss is computed from this interval only", () => {
    const prev = stat({ packetsLost: 10, packetsReceived: 90 });
    const next = stat({ packetsLost: 11, packetsReceived: 189, timestamp: 2000 });
    expect(inboundInterval(prev, next).lossPercent).toBe(1);
  });
  test("no packets in the interval gives null loss", () => {
    const prev = stat({ packetsLost: 1, packetsReceived: 9 });
    expect(inboundInterval(prev, { ...prev, timestamp: 2000 }).lossPercent).toBeNull();
  });
  test("interval exposes packet deltas", () => {
    const i = inboundInterval(stat({ packetsLost: 1, packetsReceived: 9 }), stat({ packetsLost: 3, packetsReceived: 107, timestamp: 2000 }));
    expect(i).toMatchObject({ lost: 2, received: 98, lossPercent: 2 });
  });
});

const inb = (trackId: string): InboundVideoStat => stat({ trackId, packetsReceived: 10, bytesReceived: 1, timestamp: 1 });
const out = { width: 1920, height: 1080, fps: 60, qualityLimitation: "none" };
const summ = (o: Partial<ConnectivitySummary>): ConnectivitySummary => ({ protocol: "udp", candidateType: "host", rttMs: 10, outbound: null, inbound: [], ...o });

test("mergeSummaries keeps inbound when only publisher exists (single peer connection)", () => {
  const m = mergeSummaries(summ({ outbound: out, inbound: [inb("a")] }), null);
  expect(m.outbound).toEqual(out);
  expect(m.inbound).toEqual([inb("a")]);
});
test("mergeSummaries combines separate publisher and subscriber", () => {
  const m = mergeSummaries(summ({ outbound: out }), summ({ protocol: "tcp", inbound: [inb("b")] }));
  expect(m.outbound).toEqual(out);
  expect(m.inbound).toEqual([inb("b")]);
  expect(m.protocol).toBe("udp");
});
test("mergeSummaries falls back to subscriber when publisher transport is unknown", () => {
  const m = mergeSummaries(summ({ protocol: "unknown", candidateType: "unknown", rttMs: null }), summ({ protocol: "tcp", candidateType: "relay", rttMs: 5 }));
  expect(m).toMatchObject({ protocol: "tcp", candidateType: "relay", rttMs: 5 });
});
test("mergeSummaries of nothing is unknown and empty", () => {
  expect(mergeSummaries(null, null)).toEqual({ protocol: "unknown", candidateType: "unknown", rttMs: null, outbound: null, inbound: [] });
});
