import { aggregateSamples, formatReport } from "./report";
import type { ConnectivitySummary } from "./stats";

const s = (fps: number[], loss: number, out: number | null = null, lim = "none"): ConnectivitySummary => ({
  protocol: "udp", candidateType: "host", rttMs: 40,
  outbound: out === null ? null : { width: 1920, height: 1080, fps: out, qualityLimitation: lim },
  inbound: fps.map((f, i) => ({ trackId: `t${i}`, width: 1920, height: 1080, fps: f, lossPercent: loss, bytesReceived: 0, timestamp: 0 })),
});

test("aggregates fps and loss, skipping zero fps", () => {
  const agg = aggregateSamples([s([60, 0], 1), s([56], 3, 60, "bandwidth"), s([], 0, 59, "cpu")]);
  expect(agg).toMatchObject({ samples: 3, minInboundFps: 56, avgInboundFps: 58, maxLossPercent: 3, maxOutboundFps: 60, avgRttMs: 40 });
  expect(agg.outboundLimitations.sort()).toEqual(["bandwidth", "cpu"]);
});
test("empty samples give nulls", () => {
  expect(aggregateSamples([])).toMatchObject({ samples: 0, protocol: "unknown", minInboundFps: null, avgRttMs: null });
});
test("report is JSON with meta first", () => {
  const text = formatReport({ name: "Вася", browser: "chrome", startedAt: new Date("2026-10-20T18:00:00Z") }, aggregateSamples([s([60], 0)]));
  const obj = JSON.parse(text);
  expect(Object.keys(obj).slice(0, 3)).toEqual(["name", "browser", "startedAt"]);
  expect(obj.startedAt).toBe("2026-10-20T18:00:00.000Z");
  expect(obj.minInboundFps).toBe(60);
});
