import { createLinkSampler } from "./link-stats";

type Stat = Record<string, unknown> & { id: string; type: string };
const report = (...stats: Stat[]): RTCStatsReport => new Map(stats.map((s) => [s.id, s]));

/** The publisher's side: our microphone and what the server says about receiving it. */
const sent = (at: number, bytesSent: number, fractionLost: number) =>
  report(
    {
      id: "pair-pub",
      type: "candidate-pair",
      nominated: true,
      state: "succeeded",
      localCandidateId: "local-pub",
      currentRoundTripTime: 0.042,
      bytesSent,
      bytesReceived: 0,
      timestamp: at,
    },
    { id: "local-pub", type: "local-candidate", candidateType: "host", protocol: "udp" },
    { id: "out", type: "outbound-rtp", kind: "audio", timestamp: at },
    { id: "rin", type: "remote-inbound-rtp", kind: "audio", fractionLost },
  );

/** The subscriber's side: a voice we receive. */
const received = (at: number, bytesReceived: number, packetsReceived: number, packetsLost: number) =>
  report(
    {
      id: "pair-sub",
      type: "candidate-pair",
      nominated: true,
      state: "succeeded",
      localCandidateId: "local-sub",
      currentRoundTripTime: 0.038,
      bytesSent: 0,
      bytesReceived,
      timestamp: at,
    },
    { id: "local-sub", type: "local-candidate", candidateType: "host", protocol: "udp" },
    { id: "in", type: "inbound-rtp", kind: "audio", packetsReceived, packetsLost, jitter: 0.004, timestamp: at },
  );

describe("the link sampler", () => {
  it("reads the ping, the route and the loss the server reports at once", () => {
    const sample = createLinkSampler();
    const first = sample([sent(1000, 10_000, 0.02), received(1000, 20_000, 500, 0)]);
    expect(first.ping).toBe(40);
    expect(first.route).toBe("udp");
    expect(first.lossOut).toBeCloseTo(2);
    expect(first.jitter).toBe(4);
    // Rates need two samples.
    expect(first.upKbps).toBeNull();
    expect(first.downKbps).toBeNull();
    expect(first.lossIn).toBeNull();
  });

  it("turns counters into rates between two samples", () => {
    const sample = createLinkSampler();
    sample([sent(1000, 10_000, 0), received(1000, 20_000, 500, 0)]);
    const next = sample([sent(3000, 22_000, 0), received(3000, 50_000, 590, 10)]);
    // 12 000 bytes in 2 s = 48 kbit/s up; 30 000 bytes = 120 kbit/s down.
    expect(next.upKbps).toBe(48);
    expect(next.downKbps).toBe(120);
    // 10 of the last 100 packets were lost.
    expect(next.lossIn).toBeCloseTo(10);
  });

  it("names a relayed link, and one over TCP", () => {
    const relay = report(
      {
        id: "p",
        type: "candidate-pair",
        nominated: true,
        state: "succeeded",
        localCandidateId: "l",
        currentRoundTripTime: 0.1,
        timestamp: 1,
      },
      { id: "l", type: "local-candidate", candidateType: "relay", protocol: "udp" },
    );
    expect(createLinkSampler()([relay]).route).toBe("relay");
    const tcp = report(
      { id: "p", type: "candidate-pair", nominated: true, state: "succeeded", localCandidateId: "l", timestamp: 1 },
      { id: "l", type: "local-candidate", candidateType: "host", protocol: "tcp" },
    );
    expect(createLinkSampler()([tcp]).route).toBe("tcp");
  });

  it("knows nothing without reports", () => {
    expect(createLinkSampler()([])).toEqual({
      ping: null,
      jitter: null,
      lossIn: null,
      lossOut: null,
      upKbps: null,
      downKbps: null,
      route: null,
    });
  });
});
