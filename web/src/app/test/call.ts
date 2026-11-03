import type { CallTransport, CallTransportEvents, ScreenShareResult } from "@/entities/call";

type Call = { method: string; args: unknown[] };

/**
 * A CallTransport for tests: records what the app asked of it in `calls`,
 * and `emit` plays what LiveKit would report. `screenResult` is what the
 * next startScreenShare gives.
 */
export class FakeTransport implements CallTransport {
  readonly calls: Call[] = [];
  screenResult: ScreenShareResult = { ok: true, surface: "browser", audio: true };
  private readonly listeners = new Map<keyof CallTransportEvents, Set<(...args: never[]) => void>>();

  private record(method: string, ...args: unknown[]) {
    this.calls.push({ method, args });
  }

  /** The calls of `method`, as argument lists. */
  argsOf(method: string): unknown[][] {
    return this.calls.filter((c) => c.method === method).map((c) => c.args);
  }

  emit<K extends keyof CallTransportEvents>(event: K, ...args: Parameters<CallTransportEvents[K]>): void {
    for (const fn of this.listeners.get(event) ?? [])
      (fn as (...a: Parameters<CallTransportEvents[K]>) => void)(...args);
  }

  connect(url: string, token: string): Promise<void> {
    this.record("connect", url, token);
    this.emit("link", "connected");
    return Promise.resolve();
  }

  disconnect(): Promise<void> {
    this.record("disconnect");
    return Promise.resolve();
  }

  setMicrophone(on: boolean): Promise<void> {
    this.record("setMicrophone", on);
    return Promise.resolve();
  }

  setListening(on: boolean): void {
    this.record("setListening", on);
  }

  startScreenShare(): Promise<ScreenShareResult> {
    this.record("startScreenShare");
    return Promise.resolve(this.screenResult);
  }

  stopScreenShare(): Promise<void> {
    this.record("stopScreenShare");
    return Promise.resolve();
  }

  watch(userId: string, on: boolean): void {
    this.record("watch", userId, on);
  }

  attachScreen(userId: string, el: HTMLVideoElement): () => void {
    this.record("attachScreen", userId, el);
    return () => {
      this.record("detachScreen", userId, el);
    };
  }

  useDevice(kind: "audioinput" | "audiooutput", deviceId: string): Promise<void> {
    this.record("useDevice", kind, deviceId);
    return Promise.resolve();
  }

  on<K extends keyof CallTransportEvents>(event: K, fn: CallTransportEvents[K]): () => void {
    const set = this.listeners.get(event) ?? new Set();
    set.add(fn);
    this.listeners.set(event, set);
    return () => {
      set.delete(fn);
    };
  }
}
