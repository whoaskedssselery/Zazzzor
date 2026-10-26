import { CLOSE, decodeFrame, HEARTBEAT_FRAME, OP } from "./protocol";

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

/**
 * The wait before reconnect attempt `attempt` (0 for the first): 1 s doubling
 * up to 30 s, with ±30 % of jitter from `rand` (0..1), kept within 1–30 s.
 */
export function nextDelay(attempt: number, rand: () => number): number {
  const base = Math.min(MAX_DELAY_MS, BASE_DELAY_MS * 2 ** Math.max(0, attempt));
  const jittered = Math.round(base * (1 - JITTER + 2 * JITTER * rand()));
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

/** Our own close code for a connection whose heartbeats went unanswered. */
const HEARTBEAT_TIMEOUT = 4000;
/** Heartbeats in a row without an ACK after which the connection counts as dead. */
const MAX_UNACKED = 2;

/**
 * The gateway connection (`x-gateway`): connects, heartbeats every
 * `heartbeat_interval_ms` from HELLO, reconnects after any close with the
 * delays of `nextDelay` (the attempt count starts over after a READY), and
 * hands every DISPATCH to its listeners and to the subscribers of its type.
 * A 4004 close ends it without reconnecting; `stop()` ends it too.
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
  private reconnect: ReturnType<typeof setTimeout> | null = null;
  private unacked = 0;
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
        this.unacked = 0;
        return;
      case OP.DISPATCH:
        this.dispatch(frame.t, frame.d);
        return;
    }
  }

  private dispatch(t: string, d: unknown): void {
    for (const l of [...this.listeners]) l.onDispatch?.(t, d);
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
    this.stopHeartbeat();
    this.unacked = 0;
    this.heartbeat = setInterval(() => {
      if (this.unacked >= MAX_UNACKED) {
        this.dropSocket(HEARTBEAT_TIMEOUT);
        this.scheduleReconnect();
        return;
      }
      this.unacked += 1;
      this.socket?.send(HEARTBEAT_FRAME);
    }, intervalMs);
  }

  private stopHeartbeat(): void {
    if (this.heartbeat !== null) clearInterval(this.heartbeat);
    this.heartbeat = null;
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
    this.stopHeartbeat();
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
