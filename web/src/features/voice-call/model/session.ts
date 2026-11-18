import {
  type CallTransport,
  type CameraResult,
  canRestrictOwnAudio,
  clearCall,
  currentBrowser,
  type EndCause,
  getCall,
  getVolumes,
  loadLiveKitTransport,
  type NativeShareStatus,
  nativeShare,
  onVolumes,
  readVoicePrefs,
  type ScreenShareResult,
  type ShareSource,
  setCall,
  VOLUME_DEFAULT,
} from "@/entities/call";
import { computeChannel, getGuildsState, has, myVoice, PERM } from "@/entities/guild";
import { api, errorText, unwrap } from "@/shared/api";
import { playSound } from "@/shared/lib";
import { getNativeShare, resetNativeShareState, setNativeShare } from "./native-share";
import { shareNotice } from "./notice";
import { startPushToTalk } from "./push-to-talk";

export const CALL_REPLACED = "Вы подключились к голосу в другом месте";
export const CALL_REMOVED = "Вас отключили от голосового канала";
export const CALL_FAILED = "Не удалось подключиться к голосу";
export const MIC_DENIED = "Нет доступа к микрофону";
export const NO_CONNECT = "Нет права подключаться к этому каналу";
export const NO_STREAM = "Нет права на демонстрацию экрана";
export const SHARE_FAILED = "Не удалось начать демонстрацию";
export const SHARE_SOURCE_CLOSED = "Окно демонстрации закрыто";
export const DEVICE_FAILED = "Не удалось переключить устройство";
export const NO_VIDEO = "Нет права на видео";
export const CAMERA_DENIED = "Нет доступа к камере: разрешите её в настройках сайта";
export const CAMERA_MISSING = "Камера не найдена";
export const CAMERA_FAILED = "Не удалось включить камеру";

/** How long a removal waits for the server to say whether the user left voice (a moderator) or not (a newer join). */
const REMOVAL_SETTLE_MS = 2000;

type Factory = () => CallTransport | Promise<CallTransport>;

let factory: Factory = async () => (await loadLiveKitTransport())(readVoicePrefs);
let transport: CallTransport | null = null;
let transportPromise: Promise<CallTransport> | null = null;
/** Bumped by every join and leave: a join that is no longer the latest stays silent. */
let generation = 0;
/** A join is between its token request and its connection: the old room's disconnect is part of it. */
let joining = false;
const notices = new Set<(text: string) => void>();

/** How calls are made: LiveKit by default, a fake in tests. Forgets the current transport. */
export const setTransportFactory = (f: Factory): void => {
  factory = f;
  transport = null;
  transportPromise = null;
  sentVolumes = { voice: {}, stream: {} };
};

/** The volumes the transport was told last: only what differs is sent again. */
let sentVolumes: ReturnType<typeof getVolumes> = { voice: {}, stream: {} };

/** Tells the transport every volume that changed since it was told last (one never set is 100 %). */
const syncVolumes = (t: CallTransport): void => {
  const next = getVolumes();
  for (const kind of ["voice", "stream"] as const) {
    for (const id of new Set([...Object.keys(sentVolumes[kind]), ...Object.keys(next[kind])])) {
      const now = next[kind][id] ?? VOLUME_DEFAULT;
      if ((sentVolumes[kind][id] ?? VOLUME_DEFAULT) !== now) t.setVolume(id, kind, now / 100);
    }
  }
  sentVolumes = next;
};

// A slider moved, in a call or not: the call hears it at once.
onVolumes(() => {
  if (transport) syncVolumes(transport);
});

/** Calls `fn` with each message the user should see about the call (a toast). */
export const onCallNotice = (fn: (text: string) => void): (() => void) => {
  notices.add(fn);
  return () => {
    notices.delete(fn);
  };
};

const notify = (text: string) => {
  for (const fn of notices) fn(text);
};

let pttHeld = false;
let stopPtt: (() => void) | null = null;

/** Push-to-talk by choice, or because the call's channel does not allow voice activity. */
const pushToTalk = (): boolean => {
  if (readVoicePrefs().mode === "ptt") return true;
  const { guildId, channelId } = getCall();
  const s = getGuildsState();
  const entry = guildId === null ? undefined : s.guilds[guildId];
  if (!entry || channelId === null || s.me === null) return false;
  return !has(computeChannel(entry, channelId, s.me.id), PERM.USE_VOICE_ACTIVITY);
};

/**
 * The microphone sends when the user has it on, is not deafened, is not
 * muted by a moderator, and holds the key in push-to-talk.
 */
const micOn = (): boolean => {
  const { selfMute, selfDeaf } = getCall();
  if (selfMute || selfDeaf || (myVoice(getGuildsState())?.mute ?? false)) return false;
  return !pushToTalk() || pttHeld;
};

/**
 * Applies the voice settings to the call: push-to-talk listening on or
 * off, and the microphone accordingly. The settings tab calls it after a
 * change; joining calls it too.
 */
export const applyVoicePrefs = (): void => {
  stopPtt?.();
  stopPtt = null;
  pttHeld = false;
  if (getCall().status !== "idle" && pushToTalk()) {
    stopPtt = startPushToTalk(readVoicePrefs().pttKey, (held) => {
      pttHeld = held;
      void transport?.setMicrophone(micOn());
    });
  }
  if (getCall().status !== "idle") void transport?.setMicrophone(micOn());
};

/** Switches the microphone or the speakers of the call (the setting is saved by the caller). */
export const switchAudioDevice = async (kind: "audioinput" | "audiooutput", deviceId: string): Promise<void> => {
  try {
    await transport?.useDevice(kind, deviceId);
  } catch {
    notify(DEVICE_FAILED);
  }
};

let pendingRemoval: ReturnType<typeof setTimeout> | null = null;

/** Says why a removed call ended once the server tells: it dropped the user from voice (a moderator) or kept them (a newer join elsewhere). */
const settleRemoval = (final: boolean) => {
  if (pendingRemoval === null) return;
  const inVoice = myVoice(getGuildsState()) !== null;
  if (inVoice && !final) return;
  clearTimeout(pendingRemoval);
  pendingRemoval = null;
  notify(inVoice ? CALL_REPLACED : CALL_REMOVED);
};

/** A join or a leave makes a removal still being settled beside the point. */
const forgetRemoval = () => {
  if (pendingRemoval !== null) clearTimeout(pendingRemoval);
  pendingRemoval = null;
};

const ended = (cause: EndCause) => {
  if (getCall().status === "idle") return;
  clearCall();
  void dropNativeShare();
  playSound("leave");
  applyVoicePrefs();
  if (cause === "replaced") notify(CALL_REPLACED);
  else if (cause === "failed") notify(CALL_FAILED);
  else if (cause === "removed") {
    // LiveKit says "removed" for a moderator and for a newer token alike, and
    // the server announces the leave only after removing: wait for it.
    if (pendingRemoval !== null) clearTimeout(pendingRemoval);
    pendingRemoval = setTimeout(() => {
      settleRemoval(true);
    }, REMOVAL_SETTLE_MS);
  }
};

/** The transport, made once and wired to the call store. */
const ensureTransport = (): Promise<CallTransport> => {
  transportPromise ??= Promise.resolve(factory()).then(wire);
  return transportPromise;
};

const wire = (t: CallTransport): CallTransport => {
  t.on("link", (state, cause) => {
    // During a join the old room goes away on purpose (or the server drops it first).
    if (state === "disconnected") {
      if (!joining) ended(cause ?? "failed");
    } else if (getCall().status !== "idle") setCall({ status: state });
  });
  t.on("speaking", (speaking) => {
    setCall({ speaking });
  });
  t.on("streams", (streams) => {
    const { watching, streams: before } = getCall();
    if (streams.some((id) => !before.includes(id))) playSound("streamStart");
    else if (before.some((id) => !streams.includes(id))) playSound("streamStop");
    setCall({ streams, watching: watching.filter((id) => streams.includes(id)) });
    // A share that ends is not received again when it comes back, until «Смотреть».
    for (const id of watching) if (!streams.includes(id)) t.watch(id, false);
  });
  t.on("micDenied", () => {
    if (getCall().micDenied) return;
    setCall({ micDenied: true });
    notify(MIC_DENIED);
  });
  t.on("screenEnded", () => {
    setCall({ sharing: false });
    playSound("streamStop");
  });
  t.on("cameras", (cameras) => {
    setCall({ cameras });
  });
  t.on("cameraEnded", () => {
    setCall({ camera: false });
  });
  t.on("noise", (noise) => {
    if (getCall().status !== "idle") setCall({ noise });
  });
  syncVolumes(t);
  transport = t;
  return t;
};

/**
 * Joins voice channel `channelId` of server `guildId`, or the call of a
 * DM or a group when `guildId` is null: a fresh token (the server drops
 * the user from any other channel, and a call starts ringing the others),
 * then one connection with the user's switches applied. A refused token is
 * said in a notice.
 */
export const joinVoice = async (guildId: string | null, channelId: string): Promise<void> => {
  const mine = ++generation;
  const latest = () => mine === generation;
  joining = true;
  forgetRemoval();
  try {
    let joined: { token: string; url: string };
    try {
      joined = unwrap(await api.POST("/channels/{channel_id}/voice", { params: { path: { channel_id: channelId } } }));
    } catch (e) {
      if (latest()) notify(errorText(e));
      return;
    }
    let t: CallTransport;
    try {
      t = await ensureTransport();
    } catch {
      transportPromise = null;
      if (latest()) notify(CALL_FAILED);
      return;
    }
    if (!latest()) return;
    await t.disconnect();
    void dropNativeShare();
    if (!latest()) return;
    setCall({
      status: "connecting",
      guildId,
      channelId,
      micDenied: false,
      speaking: [],
      streams: [],
      watching: [],
      sharing: false,
      cameras: [],
      camera: false,
      noise: null,
    });
    try {
      await t.connect(joined.url, joined.token);
    } catch {
      // A newer join or a leave cancelled this one: it says nothing.
      if (!latest()) return;
      clearCall();
      notify(CALL_FAILED);
      return;
    }
    if (!latest()) return;
    // Connected: from here a disconnect is this call's own, and ends it.
    joining = false;
    setCall({ status: "connected" });
    playSound("join");
    t.setListening(!getCall().selfDeaf);
    const { outputId } = readVoicePrefs();
    if (outputId !== null) {
      try {
        await t.useDevice("audiooutput", outputId);
      } catch {
        if (!latest() || getCall().status === "idle") return;
        // A saved device can disappear. Keep the call on the browser's default output.
        await t.useDevice("audiooutput", "default").catch(() => {});
      }
    }
    if (!latest() || getCall().status === "idle") return;
    applyVoicePrefs();
  } finally {
    if (latest()) joining = false;
  }
};

/** Leaves voice: the server forgets the state at once, and the connection goes. */
export const leaveVoice = async (): Promise<void> => {
  generation += 1;
  joining = false;
  forgetRemoval();
  if (getCall().status !== "idle") playSound("leave");
  clearCall();
  void dropNativeShare();
  applyVoicePrefs();
  await transport?.disconnect();
  try {
    unwrap(await api.DELETE("/users/@me/voice"));
  } catch (e) {
    notify(errorText(e));
  }
};

/** Ends the call here without telling the server (the session ended; the server knows). */
export const endCall = async (cause: EndCause): Promise<void> => {
  generation += 1;
  joining = false;
  ended(cause);
  clearCall();
  applyVoicePrefs();
  await transport?.disconnect();
};

/** VOICE_MOVE: a moderator moved the user; follow them to `channelId` of the same server. */
export const onVoiceMove = async (channelId: string): Promise<void> => {
  const { guildId } = getCall();
  if (guildId !== null) await joinVoice(guildId, channelId);
};

/** Stops the call of DM or group `channelId` ringing the user; a failure is said in a notice. */
export const declineCall = async (channelId: string): Promise<void> => {
  try {
    unwrap(await api.POST("/channels/{channel_id}/call/decline", { params: { path: { channel_id: channelId } } }));
  } catch (e) {
    notify(errorText(e));
  }
};

/** The microphone switch; applied at once in a call. */
export const setSelfMute = (on: boolean): void => {
  if (getCall().selfMute !== on) playSound(on ? "mute" : "unmute");
  setCall({ selfMute: on });
  if (getCall().status !== "idle") void transport?.setMicrophone(micOn());
  void reconcileVoiceFlags();
};

/** The sound switch; deafened also means no microphone, as in Discord. */
export const setSelfDeaf = (on: boolean): void => {
  if (getCall().selfDeaf !== on) playSound(on ? "deafen" : "undeafen");
  setCall({ selfDeaf: on });
  if (getCall().status !== "idle") {
    transport?.setListening(!on);
    void transport?.setMicrophone(micOn());
  }
  void reconcileVoiceFlags();
};

let syncing = false;
/** The moderator's mute as last applied to the microphone. */
let modMuted = false;

/**
 * Keeps the server's view of the user's switches in step: it tells the
 * server when the voice state there differs (after joining, the state starts
 * with both off; after READY, the API forgot them), one request at a time,
 * and again when a switch changed while one was in flight or it failed. Also
 * follows a moderator's mute both ways, and settles why a removed call ended.
 */
export const reconcileVoiceFlags = async (): Promise<void> => {
  settleRemoval(false);
  const mine = myVoice(getGuildsState());
  const { status, channelId, selfMute, selfDeaf } = getCall();
  if (status === "idle" || mine?.channel_id !== channelId) return;
  if (mine.mute !== modMuted) {
    modMuted = mine.mute;
    void transport?.setMicrophone(micOn());
  }
  if (syncing || (mine.self_mute === selfMute && mine.self_deaf === selfDeaf)) return;
  await sendFlags();
};

/** PATCHes the switches as they are now; then once more if they changed meanwhile, or a second later if it failed. */
const sendFlags = async (): Promise<void> => {
  const { selfMute, selfDeaf } = getCall();
  syncing = true;
  let failed = false;
  try {
    unwrap(await api.PATCH("/users/@me/voice-state", { body: { self_mute: selfMute, self_deaf: selfDeaf } }));
  } catch {
    failed = true;
  } finally {
    syncing = false;
  }
  const now = getCall();
  if (now.status === "idle") return;
  if (now.selfMute !== selfMute || now.selfDeaf !== selfDeaf) await sendFlags();
  else if (failed) {
    // The voice write limit is 2/s; the next state from the server also retries.
    setTimeout(() => void reconcileVoiceFlags(), 1000);
  }
};

/** Starts or stops receiving `userId`'s screen share (only watched streams cost bandwidth). */
export const watchStream = (userId: string, on: boolean): void => {
  const { watching } = getCall();
  setCall({ watching: on ? [...new Set([...watching, userId])] : watching.filter((id) => id !== userId) });
  transport?.watch(userId, on);
};

/** Shows `userId`'s screen share in `el`; the returned function lets go of it. */
export const attachStream = (userId: string, el: HTMLVideoElement): (() => void) =>
  transport?.attachScreen(userId, el) ?? (() => undefined);

/** Shows the user's own screen share in `el`; the returned function lets go of it. */
export const attachOwnStream = (el: HTMLVideoElement): (() => void) =>
  transport?.attachOwnScreen(el) ?? (() => undefined);

/**
 * Starts sharing a screen, a window or a tab (the browser's picker) and says
 * what the viewers will hear. A closed picker changes nothing. In the
 * desktop app the app's own picker opens instead (see `shareNatively`).
 */
let picking = false;

export const startScreenShare = async (): Promise<void> => {
  // One picker at a time: a second click would start a second capture.
  if (!transport || picking || getCall().status === "idle") return;
  const t = transport;
  const mine = generation;
  picking = true;
  let result: ScreenShareResult;
  try {
    if (await nativeShare.available()) {
      setNativeShare({ picking: true });
      return;
    }
    result = await t.startScreenShare();
  } finally {
    picking = false;
  }
  // The call ended or changed while the picker was open: stop what was picked.
  if (result.ok && (mine !== generation || getCall().status === "idle")) {
    await t.stopScreenShare();
    return;
  }
  if (!result.ok) {
    if (result.reason === "failed") notify(SHARE_FAILED);
    return;
  }
  setCall({ sharing: true });
  playSound("streamStart");
  const notice = shareNotice(result, currentBrowser(), canRestrictOwnAudio());
  if (notice !== null) notify(notice);
};

export const stopScreenShare = async (): Promise<void> => {
  if (getCall().sharing) playSound("streamStop");
  setCall({ sharing: false });
  if (getNativeShare().source !== null) {
    await dropNativeShare();
    return;
  }
  await transport?.stopScreenShare();
};

/** How often the page asks the app how its share goes. */
const NATIVE_POLL_MS = 2000;

let nativePoll: ReturnType<typeof setInterval> | null = null;

/** Forgets the app's share here and stops it there; nothing is said or played. */
const dropNativeShare = async (): Promise<void> => {
  if (nativePoll !== null) clearInterval(nativePoll);
  nativePoll = null;
  const wasSharing = getNativeShare().source !== null;
  resetNativeShareState();
  if (wasSharing) await nativeShare.stop().catch(() => undefined);
};

/**
 * Shares `source` through the desktop app: a companion token for the
 * call's channel from the server, handed to the app, which joins the call
 * as the user's screen. `sound` brings a window's application sound, or a
 * screen's whole system sound but the app's. A failure is said in a notice.
 */
export const shareNatively = async (source: ShareSource, sound: boolean, again = false): Promise<void> => {
  setNativeShare({ picking: false });
  const { channelId, status } = getCall();
  if (channelId === null || status === "idle" || picking) return;
  const mine = generation;
  const failed = (text: string) => {
    if (mine !== generation) return;
    notify(text);
    // A share that was going and could not come back has ended.
    if (again && getCall().sharing) {
      setCall({ sharing: false });
      playSound("streamStop");
    }
  };
  picking = true;
  try {
    let joined: { token: string; url: string };
    try {
      joined = unwrap(
        await api.POST("/channels/{channel_id}/voice/screen", { params: { path: { channel_id: channelId } } }),
      );
    } catch (e) {
      failed(errorText(e));
      return;
    }
    if (mine !== generation) return;
    try {
      await nativeShare.start({ url: joined.url, token: joined.token, id: source.id, sound });
    } catch {
      failed(SHARE_FAILED);
      return;
    }
  } finally {
    picking = false;
  }
  // The call ended or changed while the share was starting.
  if (mine !== generation || getCall().status === "idle") {
    await nativeShare.stop().catch(() => undefined);
    return;
  }
  setNativeShare({ source, stats: null });
  if (!again) {
    setCall({ sharing: true });
    playSound("streamStart");
  }
  // A share that came back is tried again only after it went well for a while.
  let retried = again;
  if (nativePoll !== null) clearInterval(nativePoll);
  nativePoll = setInterval(() => {
    void nativeShare
      .status()
      .then((s) => followNativeShare(s, mine, source, sound, retried))
      .then((healthy) => {
        if (healthy) retried = false;
      })
      .catch(() => undefined);
  }, NATIVE_POLL_MS);
};

/**
 * Takes one status of the app's share: its figures, or how it ended and
 * what follows. Says whether the share is going with figures.
 */
const followNativeShare = async (
  status: NativeShareStatus,
  mine: number,
  source: ShareSource,
  sound: boolean,
  retried: boolean,
): Promise<boolean> => {
  if (getNativeShare().source !== source) return false;
  if (status.state === "sharing") {
    setNativeShare({ stats: status.stats });
    return status.stats !== null;
  }
  if (nativePoll !== null) clearInterval(nativePoll);
  nativePoll = null;
  resetNativeShareState();
  const end = status.state === "ended" ? status.end : "disconnected";
  // The server drops the companion when the call itself reconnects: share again, quietly.
  if (end === "disconnected" && !retried && mine === generation && getCall().status === "connected") {
    await shareNatively(source, sound, true);
    return false;
  }
  if (!getCall().sharing) return false;
  setCall({ sharing: false });
  playSound("streamStop");
  if (end === "sourceClosed") notify(SHARE_SOURCE_CLOSED);
  return false;
};

const CAMERA_NOTICE = { denied: CAMERA_DENIED, missing: CAMERA_MISSING, failed: CAMERA_FAILED } as const;

let startingCamera = false;

/**
 * Turns the camera on: the browser asks for it the first time, and the
 * others see it in the user's tile. A refusal or a missing camera is said
 * in a notice.
 */
export const startCamera = async (): Promise<void> => {
  // One request at a time: a second click would open the camera twice.
  if (!transport || startingCamera || getCall().status === "idle") return;
  const t = transport;
  const mine = generation;
  startingCamera = true;
  let result: CameraResult;
  try {
    result = await t.startCamera();
  } finally {
    startingCamera = false;
  }
  // The call ended or changed while the browser asked: turn it back off.
  if (result.ok && (mine !== generation || getCall().status === "idle")) {
    await t.stopCamera();
    return;
  }
  if (!result.ok) {
    notify(CAMERA_NOTICE[result.reason]);
    return;
  }
  setCall({ camera: true });
};

export const stopCamera = async (): Promise<void> => {
  setCall({ camera: false });
  await transport?.stopCamera();
};

/** Shows `userId`'s camera in `el` (it is received only meanwhile); the returned function lets go of it. */
export const attachCamera = (userId: string, el: HTMLVideoElement): (() => void) =>
  transport?.attachCamera(userId, el) ?? (() => undefined);

/** Shows the user's own camera in `el`; the returned function lets go of it. */
export const attachOwnCamera = (el: HTMLVideoElement): (() => void) =>
  transport?.attachOwnCamera(el) ?? (() => undefined);
