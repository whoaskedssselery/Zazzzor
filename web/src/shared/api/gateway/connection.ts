import { CLOSE, decodeFrame, HEARTBEAT_FRAME, OP, parseEvent } from "./protocol";

/**
 * Where the gateway is: the page's own host (`x-gateway.path` under
 * /api/v1), over wss on an https page and ws on an http one.
 */
export function gatewayUrl(location: Pick<Location, "protocol" | "host">): string {
  const scheme = location.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${location.host}/api/v1/gateway`;
}

const BASE_DELAY_MS = 1000;
const MAX_DELAY_MS = 30_000;
const JITTER = 0.3;

/** The base is kept here so that ±30 % of it is always within 1–30 s. */
const MIN_BASE_MS = BASE_DELAY_MS / (1 - JITTER);
const MAX_BASE_MS = MAX_DELAY_MS / (1 + JITTER);

/**
 * The wait before reconnect attempt `attempt` (0 for the first): a base of
 * 1 s doubling each attempt, clamped to [1 s / 0.7, 30 s / 1.3], then ±30 %
 * of jitter from `rand` (0..1). So every delay is within 1–30 s and the
 * jitter is never cut off by those bounds: attempt 0 waits 1–1.86 s, the
 * cap 16.2–30 s.
 */
export function nextDelay(attempt: number, rand: () => number): number {
  const doubled = BASE_DELAY_MS * 2 ** Math.max(0, attempt);
  const base = Math.min(MAX_BASE_MS, Math.max(MIN_BASE_MS, doubled));
  const jittered = Math.round(base * (1 - JITTER + 2 * JITTER * rand()));
  // Only guards against float rounding; the range above is already inside.
  return Math.min(MAX_DELAY_MS, Math.max(BASE_DELAY_MS, jittered));
}

/**
 * "connecting" until the first READY, "ready" while a connection that got
 * READY is open, "reconnecting" after such a connection was lost and until
 * the next READY.
 */
export type ConnectionStatus = "connecting" | "ready" | "reconnecting";

/** What the connection needs of a WebSocket (the browser's, or a test double). */
export interface GatewaySocket {
  onmessage: ((ev: MessageEvent) => void) | null;
  onclose: ((ev: CloseEvent) => void) | null;
  send(data: string): void;
  close(code?: number, reason?: string): void;
}

export interface GatewayTransport {
  /** The gateway URL, read on every connect. Default: `gatewayUrl(window.location)`. */
  url?: () => string;
  createSocket?: (url: string) => GatewaySocket;
  /** 0..1 for the jitter of the reconnect delays. Default: Math.random. */
  random?: () => number;
}

/** Who listens to the connection as a whole (the app's stores). */
export interface GatewayListener {
  /** Every DISPATCH, before the subscribers of its type. */
  onDispatch?: (t: string, d: unknown) => void;
  onStatus?: (status: ConnectionStatus) => void;
  /** The server closed with 4004: the session is gone. The connection stays stopped. */
  onUnauthorized?: () => void;
}

/**
 * Our own close code for a connection we give up on: no HELLO in time,
 * heartbeats unanswered, or a READY that does not match the contract.
 */
const GIVE_UP = 4000;
/** A new socket that has not said HELLO within this long is dropped (a hung connect after sleep or a network switch). */
const HELLO_TIMEOUT_MS = 10_000;

/**
 * The gateway connection (`x-gateway`): connects, heartbeats every
 * `heartbeat_interval_ms` from HELLO, reconnects after any close with the
 * delays of `nextDelay` (the attempt count starts over after a READY), and
 * hands every DISPATCH to its listeners and to the subscribers of its type.
 * A 4004 close ends it without reconnecting; `stop()` ends it too.
 *
 * It also gives up on a socket itself (closes it with 4000 and reconnects):
 * - no HELLO within 10 s of opening it;
 * - a heartbeat still unacknowledged when the next one is due. That is the
 *   plan's rule (no ACK for two intervals, counted from HELLO or from the
 *   last ACK), checked at each tick: a silent link loss shows within about
 *   one interval of the first unanswered heartbeat;
 * - a READY whose payload does not match the contract (logged), so no stale
 *   store is shown as "ready".
 */
export class GatewayConnection {
  private readonly url: () => string;
  private readonly createSocket: (url: string) => GatewaySocket;
  private readonly random: () => number;

  private readonly listeners = new Set<GatewayListener>();
  private readonly subscribers = new Map<string, Set<(d: unknown) => void>>();

  private active = false;
  private socket: GatewaySocket | null = null;
  private heartbeat: ReturnType<typeof setInterval> | null = null;
  private helloTimer: ReturnType<typeof setTimeout> | null = null;
  private reconnect: ReturnType<typeof setTimeout> | null = null;
  private awaitingAck = false;
  private attempt = 0;
  private everReady = false;
  private current: ConnectionStatus = "connecting";

  constructor(transport: GatewayTransport = {}) {
    this.url = transport.url ?? (() => gatewayUrl(window.location));
    this.createSocket = transport.createSocket ?? ((url) => new WebSocket(url));
    this.random = transport.random ?? Math.random;
  }

  get status(): ConnectionStatus {
    return this.current;
  }

  /** Connects, unless it already runs. */
  start(): void {
    if (this.active) return;
    this.active = true;
    this.connect();
  }

  /** Disconnects and cancels any pending reconnect. `start()` may follow. */
  stop(): void {
    this.active = false;
    this.clearReconnect();
    this.dropSocket(1000);
    this.attempt = 0;
    this.everReady = false;
    this.setStatus("connecting");
  }

  /** Subscribes `fn` to DISPATCH events of type `t`; returns the unsubscribe. */
  on(t: string, fn: (d: unknown) => void): () => void {
    let set = this.subscribers.get(t);
    if (!set) {
      set = new Set();
      this.subscribers.set(t, set);
    }
    set.add(fn);
    return () => {
      set.delete(fn);
    };
  }

  /** Adds a listener of the whole connection; returns its removal. */
  listen(listener: GatewayListener): () => void {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  }

  private connect(): void {
    let socket: GatewaySocket;
    try {
      socket = this.createSocket(this.url());
    } catch (e) {
      console.error("gateway: could not open the connection", e);
      this.scheduleReconnect();
      return;
    }
    this.socket = socket;
    this.helloTimer = setTimeout(() => {
      this.helloTimer = null;
      this.giveUp();
    }, HELLO_TIMEOUT_MS);
    socket.onmessage = (ev) => {
      this.receive(ev.data);
    };
    socket.onclose = (ev) => {
      this.closed(ev.code);
    };
  }

  private receive(data: unknown): void {
    const frame = decodeFrame(data);
    if (!frame) return;
    switch (frame.op) {
      case OP.HELLO:
        this.startHeartbeat(frame.d.heartbeat_interval_ms);
        return;
      case OP.HEARTBEAT_ACK:
        this.awaitingAck = false;
        return;
      case OP.DISPATCH:
        this.dispatch(frame.t, frame.d);
        return;
    }
  }

  private dispatch(t: string, d: unknown): void {
    if (t === "READY" && parseEvent("READY", d) === null) {
      this.giveUp();
      return;
    }
    for (const l of [...this.listeners]) {
      try {
        l.onDispatch?.(t, d);
      } catch (e) {
        console.error(`gateway: a listener failed on ${t}`, e);
      }
    }
    if (t === "READY") {
      this.attempt = 0;
      this.everReady = true;
      this.setStatus("ready");
    }
    for (const fn of [...(this.subscribers.get(t) ?? [])]) {
      try {
        fn(d);
      } catch (e) {
        console.error(`gateway: a ${t} subscriber failed`, e);
      }
    }
  }

  private startHeartbeat(intervalMs: number): void {
    this.clearTimers();
    this.awaitingAck = false;
    this.heartbeat = setInterval(() => {
      // The previous heartbeat had a whole interval to be acknowledged.
      if (this.awaitingAck) {
        this.giveUp();
        return;
      }
      this.awaitingAck = true;
      this.socket?.send(HEARTBEAT_FRAME);
    }, intervalMs);
  }

  /** Stops the heartbeat and the HELLO deadline of the current socket. */
  private clearTimers(): void {
    if (this.heartbeat !== null) clearInterval(this.heartbeat);
    this.heartbeat = null;
    if (this.helloTimer !== null) clearTimeout(this.helloTimer);
    this.helloTimer = null;
  }

  /** Drops the current socket from our side and reconnects with backoff. */
  private giveUp(): void {
    this.dropSocket(GIVE_UP);
    this.scheduleReconnect();
  }

  /** The server (or the network) closed the socket. */
  private closed(code: number): void {
    this.dropSocket(null);
    if (code === CLOSE.AUTHENTICATION_FAILED) {
      this.active = false;
      this.attempt = 0;
      this.everReady = false;
      this.setStatus("connecting");
      for (const l of [...this.listeners]) l.onUnauthorized?.();
      return;
    }
    if (code === CLOSE.DECODE_ERROR) {
      console.error("gateway: the server could not decode a frame of ours (4002); reconnecting");
    }
    this.scheduleReconnect();
  }

  /**
   * Forgets the current socket: no more of its events reach us, its
   * heartbeat stops, and it is closed with `code` unless it is closed already
   * (null).
   */
  private dropSocket(code: number | null): void {
    this.clearTimers();
    const socket = this.socket;
    this.socket = null;
    if (!socket) return;
    socket.onmessage = null;
    socket.onclose = null;
    if (code !== null) socket.close(code);
  }

  private scheduleReconnect(): void {
    if (!this.active) return;
    if (this.everReady) this.setStatus("reconnecting");
    this.clearReconnect();
    const delay = nextDelay(this.attempt, this.random);
    this.attempt += 1;
    this.reconnect = setTimeout(() => {
      this.reconnect = null;
      if (this.active) this.connect();
    }, delay);
  }

  private clearReconnect(): void {
    if (this.reconnect !== null) clearTimeout(this.reconnect);
    this.reconnect = null;
  }

  private setStatus(status: ConnectionStatus): void {
    if (status === this.current) return;
    this.current = status;
    for (const l of [...this.listeners]) l.onStatus?.(status);
  }
}
