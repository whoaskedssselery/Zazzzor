import type { CallTransport, CallTransportEvents, CameraResult, ScreenShareResult } from "@/shared/lib";

type Call = { method: string; args: unknown[] };

/**
 * A CallTransport for tests: records what the app asked of it in `calls`,
 * and `emit` plays what LiveKit would report. Like LiveKit, dropping a live
 * connection reports it ("disconnected", "left"), and a disconnect cancels a
 * connect still in progress. `connectGate` holds connects back until it
 * resolves; `screenResult` is what the next startScreenShare gives (or a
 * promise of it), `cameraResult` the same for startCamera; `deviceError` makes useDevice fail.
 */
export class FakeTransport implements CallTransport {
  readonly calls: Call[] = [];
  screenResult: ScreenShareResult | Promise<ScreenShareResult> = { ok: true, surface: "browser", audio: true };
  cameraResult: CameraResult | Promise<CameraResult> = { ok: true };
  connectGate: Promise<void> | null = null;
  deviceError: Error | null = null;
  private connected = false;
  private attempt = 0;
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

  async connect(url: string, token: string): Promise<void> {
    this.record("connect", url, token);
    const attempt = ++this.attempt;
    if (this.connectGate) await this.connectGate;
    if (attempt !== this.attempt) throw new Error("cancelled by a newer connect or a disconnect");
    this.connected = true;
    this.emit("link", "connected");
  }

  disconnect(): Promise<void> {
    this.record("disconnect");
    this.attempt += 1;
    if (this.connected) {
      this.connected = false;
      this.emit("link", "disconnected", "left");
    }
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

  attachOwnScreen(el: HTMLVideoElement): () => void {
    this.record("attachOwnScreen", el);
    return () => {
      this.record("detachOwnScreen", el);
    };
  }

  startCamera(): Promise<CameraResult> {
    this.record("startCamera");
    return Promise.resolve(this.cameraResult);
  }

  stopCamera(): Promise<void> {
    this.record("stopCamera");
    return Promise.resolve();
  }

  attachCamera(userId: string, el: HTMLVideoElement): () => void {
    this.record("attachCamera", userId, el);
    return () => {
      this.record("detachCamera", userId, el);
    };
  }

  attachOwnCamera(el: HTMLVideoElement): () => void {
    this.record("attachOwnCamera", el);
    return () => {
      this.record("detachOwnCamera", el);
    };
  }

  useDevice(kind: "audioinput" | "audiooutput", deviceId: string): Promise<void> {
    this.record("useDevice", kind, deviceId);
    return this.deviceError ? Promise.reject(this.deviceError) : Promise.resolve();
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
