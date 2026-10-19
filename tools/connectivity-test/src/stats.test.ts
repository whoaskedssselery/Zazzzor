import { summarizeStats, bitrateKbps } from "./stats";

const base = (o: object) => ({ timestamp: 1000, ...o }) as any;
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
test("inbound loss percent", () => {
  const s = summarizeStats([
    base({ id: "i1", type: "inbound-rtp", kind: "video", trackIdentifier: "t1", frameWidth: 1920, frameHeight: 1080, framesPerSecond: 60, packetsLost: 2, packetsReceived: 98, bytesReceived: 5000 }),
    base({ id: "i2", type: "inbound-rtp", kind: "audio", packetsLost: 50, packetsReceived: 50 }),
  ]);
  expect(s.inbound).toEqual([{ trackId: "t1", width: 1920, height: 1080, fps: 60, lossPercent: 2, bytesReceived: 5000, timestamp: 1000 }]);
});
test("bitrate in kbps", () => {
  const a = { trackId: "t", width: 0, height: 0, fps: 0, lossPercent: 0, bytesReceived: 0, timestamp: 0 };
  expect(bitrateKbps(a, { ...a, bytesReceived: 1_000_000, timestamp: 1000 })).toBe(8000);
  expect(bitrateKbps(a, { ...a, bytesReceived: 10 })).toBe(0);
});
