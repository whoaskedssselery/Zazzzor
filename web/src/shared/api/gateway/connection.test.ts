import { type ConnectionStatus, GatewayConnection, type GatewaySocket, gatewayUrl, nextDelay } from "./connection";

/**
 * A WebSocket the test plays the server of: `receive` delivers a frame from
 * the server, `drop` closes the connection from the server side with a code.
 */
class FakeSocket implements GatewaySocket {
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onclose: ((ev: CloseEvent) => void) | null = null;
  onerror: ((ev: Event) => void) | null = null;
  readonly sent: unknown[] = [];
  closedWith: number | null = null;

  constructor(readonly url: string) {}

  send(data: string): void {
    this.sent.push(JSON.parse(data));
  }

  close(code = 1000): void {
    this.closedWith = code;
  }

  receive(frame: unknown): void {
    this.onmessage?.(new MessageEvent("message", { data: typeof frame === "string" ? frame : JSON.stringify(frame) }));
  }

  receiveRaw(data: unknown): void {
    this.onmessage?.(new MessageEvent("message", { data }));
  }

  drop(code: number): void {
    this.onclose?.(new CloseEvent("close", { code, wasClean: code !== 1006 }));
  }
}

const GW_URL = "ws://zazzzor.test/api/v1/gateway";
const HEARTBEAT = { op: 1, d: null };
const ACK = { op: 11, d: null };
const hello = (ms: number) => ({ op: 10, d: { heartbeat_interval_ms: ms } });
const readyFrame = (s = 1) => ({
  op: 0,
  t: "READY",
  s,
  d: {
    user: {
      id: "1",
      username: "vasya",
      display_name: "Вася",
      avatar_url: null,
      created_at: "2026-10-21T12:00:00.000Z",
    },
    guilds: [],
    presences: [],
    read_states: [],
    relationships: [],
    private_channels: [],
    user_guild_settings: [],
  },
});

function setup() {
  const sockets: FakeSocket[] = [];
  const statuses: ConnectionStatus[] = [];
  const dispatched: [string, unknown][] = [];
  const onUnauthorized = vi.fn();
  const gw = new GatewayConnection({
    url: () => GW_URL,
    createSocket: (url) => {
      const s = new FakeSocket(url);
      sockets.push(s);
      return s;
    },
    random: () => 0.5,
  });
  gw.listen({
    onUnauthorized,
    onDispatch: (t, d) => dispatched.push([t, d]),
    onStatus: (s) => statuses.push(s),
  });
  const current = () => {
    const s = sockets.at(-1);
    if (!s) throw new Error("no socket opened");
    return s;
  };
  return { gw, sockets, statuses, dispatched, onUnauthorized, current };
}

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe("gatewayUrl", () => {
  it("is wss on an https page and ws on an http page, on the page's host", () => {
    expect(gatewayUrl({ protocol: "https:", host: "zazzzor.duckdns.org" })).toBe(
      "wss://zazzzor.duckdns.org/api/v1/gateway",
    );
    expect(gatewayUrl({ protocol: "http:", host: "localhost:5173" })).toBe("ws://localhost:5173/api/v1/gateway");
  });
});

/** The first reconnect delay with the jitter of the tests (0.5). */
const FIRST = nextDelay(0, () => 0.5);

describe("nextDelay", () => {
  it("doubles from about 1 s and levels off below 30 s", () => {
    const mid = () => 0.5;
    // The base is kept within [1 s / 0.7, 30 s / 1.3], so ±30 % of it stays in 1–30 s.
    expect([0, 1, 2, 3, 4, 5, 6, 20].map((a) => nextDelay(a, mid))).toEqual([
      1429, 2000, 4000, 8000, 16000, 23077, 23077, 23077,
    ]);
  });

  it("adds up to ±30 % of jitter", () => {
    expect(nextDelay(1, () => 0)).toBe(1400);
    expect(nextDelay(1, () => 1)).toBe(2600);
    expect(nextDelay(3, () => 0.25)).toBe(6800);
    expect(nextDelay(10, () => 0)).toBe(16154);
  });

  it("spreads the delays over the whole range at both ends instead of piling them on a bound", () => {
    for (const attempt of [0, 20]) {
      const draws = [0, 0.25, 0.5, 0.75, 1].map((r) => nextDelay(attempt, () => r));
      expect(new Set(draws).size).toBe(draws.length);
      expect([...draws].sort((a, b) => a - b)).toEqual(draws);
    }
    expect(nextDelay(0, () => 0)).toBe(1000);
    expect(nextDelay(20, () => 1)).toBe(30000);
  });

  it("stays within 1–30 s for any attempt and any random value", () => {
    for (let attempt = 0; attempt < 40; attempt++) {
      for (const r of [0, 0.1, 0.25, 0.5, 0.75, 0.9, 0.999999, 1]) {
        const d = nextDelay(attempt, () => r);
        expect(d).toBeGreaterThanOrEqual(1000);
        expect(d).toBeLessThanOrEqual(30000);
      }
    }
  });
});

describe("GatewayConnection", () => {
  it("opens one socket to the gateway URL, also when started twice", () => {
    const { gw, sockets } = setup();
    gw.start();
    gw.start();
    expect(sockets.map((s) => s.url)).toEqual([GW_URL]);
    gw.stop();
  });

  it("after HELLO sends a heartbeat every interval while ACKs come back", () => {
    const { gw, sockets, current } = setup();
    gw.start();
    current().receive(hello(1000));
    expect(current().sent).toEqual([]);
    for (let i = 1; i <= 5; i++) {
      vi.advanceTimersByTime(1000);
      expect(current().sent).toHaveLength(i);
      current().receive(ACK);
    }
    expect(current().sent.every((f) => JSON.stringify(f) === JSON.stringify(HEARTBEAT))).toBe(true);
    expect(sockets).toHaveLength(1);
    gw.stop();
  });

  it("reconnects when a heartbeat is still unanswered when the next one is due", () => {
    const { gw, sockets, statuses, current } = setup();
    gw.start();
    current().receive(hello(1000));
    current().receive(readyFrame());
    vi.advanceTimersByTime(1000); // heartbeat 1, never acknowledged
    expect(sockets[0]?.closedWith).toBeNull();
    vi.advanceTimersByTime(999);
    expect(sockets[0]?.closedWith).toBeNull();
    vi.advanceTimersByTime(1); // heartbeat 2 is due: two intervals since HELLO without an ACK
    expect(sockets[0]?.closedWith).not.toBeNull();
    expect(sockets[0]?.sent).toHaveLength(1);
    expect(statuses.at(-1)).toBe("reconnecting");
    vi.advanceTimersByTime(nextDelay(0, () => 0.5));
    expect(sockets).toHaveLength(2);
    gw.stop();
  });

  it("stops heartbeats of a closed socket", () => {
    const { gw, sockets, current } = setup();
    gw.start();
    current().receive(hello(1000));
    current().drop(1006);
    vi.advanceTimersByTime(1000);
    expect(sockets[0]?.sent).toEqual([]);
    gw.stop();
  });

  it("on 4004 signs out and does not reconnect", () => {
    const { gw, sockets, onUnauthorized, current } = setup();
    gw.start();
    current().receive(hello(1000));
    current().receive(readyFrame());
    current().drop(4004);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(120_000);
    expect(sockets).toHaveLength(1);
    expect(sockets[0]?.sent).toEqual([]);
  });

  it.each([1012, 1006, 4008, 4009, 1011, 1000])("reconnects after close %i with the delays of nextDelay", (code) => {
    const { gw, sockets, current } = setup();
    gw.start();
    current().drop(code);
    vi.advanceTimersByTime(nextDelay(0, () => 0.5) - 1);
    expect(sockets).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(sockets).toHaveLength(2);
    // Without a READY in between the next delay doubles.
    current().drop(code);
    vi.advanceTimersByTime(nextDelay(1, () => 0.5) - 1);
    expect(sockets).toHaveLength(2);
    vi.advanceTimersByTime(1);
    expect(sockets).toHaveLength(3);
    gw.stop();
  });

  it("starts over at the shortest delay after a READY", () => {
    const { gw, sockets, current } = setup();
    gw.start();
    current().drop(1006);
    vi.advanceTimersByTime(FIRST);
    current().drop(1006);
    vi.advanceTimersByTime(nextDelay(1, () => 0.5));
    current().receive(hello(30000));
    current().receive(readyFrame());
    current().drop(1012);
    vi.advanceTimersByTime(FIRST);
    expect(sockets).toHaveLength(4);
    gw.stop();
  });

  it("on 4002 logs the decode error and reconnects", () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const { gw, sockets, current } = setup();
    gw.start();
    current().drop(4002);
    expect(error).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(FIRST);
    expect(sockets).toHaveLength(2);
    gw.stop();
    error.mockRestore();
  });

  it("does not reconnect after stop()", () => {
    const { gw, sockets, current } = setup();
    gw.start();
    current().receive(hello(1000));
    gw.stop();
    expect(sockets[0]?.closedWith).toBe(1000);
    vi.advanceTimersByTime(120_000);
    expect(sockets).toHaveLength(1);
    expect(sockets[0]?.sent).toEqual([]);
  });

  it("cancels a pending reconnect on stop()", () => {
    const { gw, sockets, current } = setup();
    gw.start();
    current().drop(1006);
    gw.stop();
    vi.advanceTimersByTime(120_000);
    expect(sockets).toHaveLength(1);
  });

  it("can start again after stop()", () => {
    const { gw, sockets } = setup();
    gw.start();
    gw.stop();
    gw.start();
    expect(sockets).toHaveLength(2);
    gw.stop();
  });

  it("reports connecting, ready, reconnecting and ready again", () => {
    const { gw, statuses, current } = setup();
    expect(gw.status).toBe("connecting");
    gw.start();
    current().receive(hello(1000));
    current().receive(readyFrame());
    current().drop(1012);
    vi.advanceTimersByTime(FIRST);
    current().receive(hello(1000));
    current().receive(readyFrame());
    expect(statuses).toEqual(["ready", "reconnecting", "ready"]);
    gw.stop();
    expect(gw.status).toBe("connecting");
  });

  it("hands every DISPATCH to onDispatch and to the subscribers of its type", () => {
    const { gw, dispatched, current } = setup();
    const onReady = vi.fn();
    const onMessage = vi.fn();
    const offReady = gw.on("READY", onReady);
    const offMessage = gw.on("MESSAGE_CREATE", onMessage);
    gw.start();
    current().receive(hello(1000));
    current().receive(readyFrame());
    const message = { id: "5", content: "привет" };
    current().receive({ op: 0, t: "MESSAGE_CREATE", s: 2, d: message });
    expect(dispatched.map(([t]) => t)).toEqual(["READY", "MESSAGE_CREATE"]);
    expect(onReady).toHaveBeenCalledTimes(1);
    expect(onMessage).toHaveBeenCalledWith(message);
    offMessage();
    current().receive({ op: 0, t: "MESSAGE_CREATE", s: 3, d: message });
    expect(onMessage).toHaveBeenCalledTimes(1);
    offReady();
    gw.stop();
  });

  it("keeps calling the other subscribers when one throws", () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const { gw, current } = setup();
    const second = vi.fn();
    gw.on("TYPING_START", () => {
      throw new Error("boom");
    });
    gw.on("TYPING_START", second);
    gw.start();
    current().receive({ op: 0, t: "TYPING_START", s: 1, d: {} });
    expect(second).toHaveBeenCalledTimes(1);
    expect(error).toHaveBeenCalledTimes(1);
    gw.stop();
    error.mockRestore();
  });

  it.each<[string, unknown]>([
    ["invalid JSON", "{nope"],
    ["a frame without op", { d: null }],
    ["an unknown op", { op: 7, d: null }],
    ["a DISPATCH without t", { op: 0, s: 1, d: {} }],
    ["a DISPATCH without s", { op: 0, t: "READY", d: {} }],
    ["a HELLO without an interval", { op: 10, d: {} }],
    ["a HELLO with a zero interval", { op: 10, d: { heartbeat_interval_ms: 0 } }],
    ["a JSON array", [1, 2]],
    ["JSON null", "null"],
  ])("logs and drops %s without closing the connection", (_name, frame) => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { gw, sockets, dispatched, current } = setup();
    gw.start();
    current().receive(frame);
    expect(warn).toHaveBeenCalledTimes(1);
    expect(dispatched).toEqual([]);
    expect(sockets[0]?.closedWith).toBeNull();
    gw.stop();
    warn.mockRestore();
  });

  it("stays connecting while the first connection has not reached READY", () => {
    const { gw, statuses, current } = setup();
    gw.start();
    current().drop(1006);
    vi.advanceTimersByTime(FIRST);
    expect(statuses).toEqual([]);
    current().receive(hello(1000));
    current().receive(readyFrame());
    expect(statuses).toEqual(["ready"]);
    gw.stop();
  });

  it("stops telling a listener once its unsubscribe ran", () => {
    const { gw, dispatched, current } = setup();
    const onDispatch = vi.fn();
    const unlisten = gw.listen({ onDispatch });
    gw.start();
    current().receive(readyFrame());
    unlisten();
    current().receive({ op: 0, t: "PRESENCE_UPDATE", s: 2, d: { user_id: "1", status: "online" } });
    expect(onDispatch).toHaveBeenCalledTimes(1);
    expect(dispatched).toHaveLength(2);
    gw.stop();
  });

  it("closes a socket whose heartbeats go unanswered with a code of its own", () => {
    const { gw, sockets, current } = setup();
    gw.start();
    current().receive(hello(1000));
    vi.advanceTimersByTime(2000);
    expect(sockets[0]?.closedWith).toBe(4000);
    gw.stop();
  });

  it("gives up on a socket that brings no HELLO within 10 s and reconnects", () => {
    const { gw, sockets } = setup();
    gw.start();
    vi.advanceTimersByTime(9999);
    expect(sockets[0]?.closedWith).toBeNull();
    vi.advanceTimersByTime(1);
    expect(sockets[0]?.closedWith).toBe(4000);
    vi.advanceTimersByTime(FIRST);
    expect(sockets).toHaveLength(2);
    // The next socket gets its own 10 s, and a later one waits longer (no READY in between).
    vi.advanceTimersByTime(10_000);
    expect(sockets[1]?.closedWith).toBe(4000);
    vi.advanceTimersByTime(nextDelay(1, () => 0.5));
    expect(sockets).toHaveLength(3);
    gw.stop();
  });

  it("keeps a socket that said HELLO in time", () => {
    const { gw, sockets, current } = setup();
    gw.start();
    vi.advanceTimersByTime(9000);
    current().receive(hello(30_000));
    vi.advanceTimersByTime(20_000);
    expect(sockets[0]?.closedWith).toBeNull();
    expect(sockets).toHaveLength(1);
    gw.stop();
  });

  it("keeps going when a listener of the connection throws", () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const { gw, statuses, dispatched, current } = setup();
    const onReady = vi.fn();
    gw.listen({
      onDispatch: () => {
        throw new Error("boom");
      },
    });
    const after = vi.fn();
    gw.listen({ onDispatch: after });
    gw.on("READY", onReady);
    gw.start();
    current().receive(hello(1000));
    current().receive(readyFrame());
    expect(dispatched.map(([t]) => t)).toEqual(["READY"]);
    expect(after).toHaveBeenCalledTimes(1);
    expect(statuses).toEqual(["ready"]);
    expect(onReady).toHaveBeenCalledTimes(1);
    expect(error).toHaveBeenCalledTimes(1);
    gw.stop();
    error.mockRestore();
  });

  it("does not take a malformed READY: logs it, drops the socket and reconnects", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { gw, sockets, statuses, dispatched, current } = setup();
    gw.start();
    current().receive(hello(1000));
    current().receive({ op: 0, t: "READY", s: 1, d: { user: { id: "1" }, guilds: "nope", presences: [] } });
    expect(warn).toHaveBeenCalledTimes(1);
    expect(dispatched).toEqual([]);
    expect(statuses).toEqual([]);
    expect(sockets[0]?.closedWith).toBe(4000);
    vi.advanceTimersByTime(FIRST);
    expect(sockets).toHaveLength(2);
    // A good READY on the new socket is taken as usual.
    current().receive(hello(1000));
    current().receive(readyFrame());
    expect(statuses).toEqual(["ready"]);
    gw.stop();
    warn.mockRestore();
  });

  it("logs and drops a binary frame", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    const { gw, dispatched, current } = setup();
    gw.start();
    current().receiveRaw(new ArrayBuffer(4));
    expect(warn).toHaveBeenCalledTimes(1);
    expect(dispatched).toEqual([]);
    gw.stop();
    warn.mockRestore();
  });
});
