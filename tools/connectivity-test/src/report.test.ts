import { emptyConnectionEvents } from "./connection-events";
import { aggregateSamples, formatReport } from "./report";
import type { ConnectivitySummary, InboundVideoStat } from "./stats";

type In = Partial<InboundVideoStat>;
const inb = (o: In = {}): InboundVideoStat => ({
  trackId: "t0",
  width: 1920,
  height: 1080,
  fps: 60,
  packetsLost: 0,
  packetsReceived: 0,
  bytesReceived: 0,
  timestamp: 0,
  ...o,
});
const s = (inbound: In[], o: Partial<ConnectivitySummary> = {}): ConnectivitySummary => ({
  protocol: "udp",
  candidateType: "host",
  rttMs: 40,
  outbound: null,
  inbound: inbound.map(inb),
  ...o,
});
const outb = (fps: number, qualityLimitation = "none") => ({ width: 1920, height: 1080, fps, qualityLimitation });

test("aggregates fps, skipping zero fps", () => {
  const agg = aggregateSamples([
    s([{ fps: 60 }, { trackId: "t1", fps: 0 }]),
    s([{ fps: 56 }], { outbound: outb(60, "bandwidth") }),
    s([], { outbound: outb(59, "cpu") }),
  ]);
  expect(agg).toMatchObject({
    samples: 3,
    minInboundFps: 56,
    avgInboundFps: 58,
    inboundStreamSamples: 3,
    inboundStallSamples: 1,
    maxOutboundFps: 60,
    avgRttMs: 40,
  });
  expect(agg.outboundLimitations.sort()).toEqual(["bandwidth", "cpu"]);
});

test("empty samples give nulls", () => {
  expect(aggregateSamples([])).toMatchObject({
    samples: 0,
    protocol: "unknown",
    protocolsSeen: [],
    candidateTypesSeen: [],
    minInboundFps: null,
    avgRttMs: null,
    maxLossPercent: null,
    avgLossPercent: null,
    avgInboundKbps: null,
    minInboundKbps: null,
    minInboundHeight: null,
    maxInboundHeight: null,
    inboundStreamSamples: 0,
    inboundStallSamples: 0,
  });
});

test("bitrate comes from deltas per stream and skips each stream's first sample", () => {
  const agg = aggregateSamples([
    s([{ trackId: "a", bytesReceived: 0, timestamp: 0 }]),
    s([
      { trackId: "a", bytesReceived: 1_000_000, timestamp: 1000 },
      { trackId: "b", bytesReceived: 999_999_999, timestamp: 1000 },
    ]),
    s([
      { trackId: "a", bytesReceived: 1_500_000, timestamp: 2000 },
      { trackId: "b", bytesReceived: 1_000_249_999, timestamp: 2000 },
    ]),
  ]);
  // a: 8000 then 4000 kbps; b: 2000 kbps. b's first sample (huge byte count) is skipped.
  expect(agg.minInboundKbps).toBe(2000);
  expect(agg.avgInboundKbps).toBe(4667);
});

test("a stream that skipped a sample is compared with its own last sample", () => {
  const agg = aggregateSamples([
    s([{ trackId: "a", bytesReceived: 0, timestamp: 0 }]),
    s([]),
    s([{ trackId: "a", bytesReceived: 500_000, timestamp: 2000 }]),
  ]);
  expect(agg.minInboundKbps).toBe(2000);
});

test("resolution range over samples that had frames", () => {
  const agg = aggregateSamples([
    s([{ height: 1080 }, { trackId: "b", height: 720 }, { trackId: "c", height: 360, fps: 0 }]),
  ]);
  expect(agg).toMatchObject({ minInboundHeight: 720, maxInboundHeight: 1080 });
});

test("loss is per interval: an old burst does not dominate later intervals", () => {
  const agg = aggregateSamples([
    s([{ packetsLost: 50, packetsReceived: 50, timestamp: 0 }]),
    s([{ packetsLost: 51, packetsReceived: 149, timestamp: 2000 }]),
    s([{ packetsLost: 53, packetsReceived: 247, timestamp: 4000 }]),
  ]);
  expect(agg.maxLossPercent).toBe(2);
  // Packet-weighted over both intervals: 3 lost of 200.
  expect(agg.avgLossPercent).toBe(1.5);
});

test("loss is null when no packets were measured", () => {
  const agg = aggregateSamples([s([{ packetsReceived: 10 }]), s([{ packetsReceived: 10, timestamp: 2000 }])]);
  expect(agg.maxLossPercent).toBeNull();
  expect(agg.avgLossPercent).toBeNull();
});

test("transport history keeps every protocol and candidate type seen", () => {
  const agg = aggregateSamples([
    s([], { protocol: "udp", candidateType: "srflx" }),
    s([], { protocol: "unknown", candidateType: "unknown" }),
    s([], { protocol: "tcp", candidateType: "host" }),
  ]);
  expect(agg).toMatchObject({
    protocol: "tcp",
    candidateType: "host",
    protocolsSeen: ["udp", "tcp"],
    candidateTypesSeen: ["srflx", "host"],
  });
});

test("long sessions do not overflow the call stack", () => {
  const many = Array.from({ length: 200_000 }, (_, i) =>
    s([{ fps: 60 - (i % 2), packetsReceived: i * 100, bytesReceived: i * 1000, timestamp: i * 2000 }], {
      outbound: outb(60),
    }),
  );
  const agg = aggregateSamples(many);
  expect(agg.minInboundFps).toBe(59);
  expect(agg.maxOutboundFps).toBe(60);
  expect(agg.maxInboundHeight).toBe(1080);
});

test("report is JSON with meta first, then aggregate and connection events", () => {
  const events = { ...emptyConnectionEvents(), disconnects: 1, disconnectReasons: ["SIGNAL_CLOSE"] };
  const text = formatReport(
    {
      name: "Вася",
      browser: "chrome",
      userAgent: "UA/1",
      startedAt: new Date("2026-10-20T18:00:00Z"),
      now: new Date("2026-10-20T18:05:00.400Z"),
    },
    aggregateSamples([s([{ fps: 60 }])]),
    events,
  );
  const obj = JSON.parse(text);
  expect(Object.keys(obj).slice(0, 6)).toEqual(["name", "browser", "userAgent", "startedAt", "durationSec", "samples"]);
  expect(obj).toMatchObject({
    userAgent: "UA/1",
    startedAt: "2026-10-20T18:00:00.000Z",
    durationSec: 300,
    minInboundFps: 60,
    disconnects: 1,
    disconnectReasons: ["SIGNAL_CLOSE"],
  });
});
