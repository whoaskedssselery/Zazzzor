import {
  type CallTransport,
  canRestrictOwnAudio,
  clearCall,
  currentBrowser,
  type EndCause,
  getCall,
  loadLiveKitTransport,
  readVoicePrefs,
  setCall,
} from "@/entities/call";
import { computeChannel, getGuildsState, has, myVoice, PERM } from "@/entities/guild";
import { api, errorText, unwrap } from "@/shared/api";
import { shareNotice } from "./notice";
import { startPushToTalk } from "./push-to-talk";

export const CALL_REPLACED = "Вы подключились к голосу в другом месте";
export const CALL_REMOVED = "Вас отключили от голосового канала";
export const CALL_FAILED = "Не удалось подключиться к голосу";
export const MIC_DENIED = "Нет доступа к микрофону";
export const NO_CONNECT = "Нет права подключаться к этому каналу";
export const NO_STREAM = "Нет права на демонстрацию экрана";
export const SHARE_FAILED = "Не удалось начать демонстрацию";
export const DEVICE_FAILED = "Не удалось переключить устройство";

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
};

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

const ended = (cause: EndCause) => {
  if (getCall().status === "idle") return;
  clearCall();
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
    setCall({ streams, watching: getCall().watching.filter((id) => streams.includes(id)) });
  });
  t.on("micDenied", () => {
    if (getCall().micDenied) return;
    setCall({ micDenied: true });
    notify(MIC_DENIED);
  });
  t.on("screenEnded", () => {
    setCall({ sharing: false });
  });
  t.on("noise", (noise) => {
    if (getCall().status !== "idle") setCall({ noise });
  });
  transport = t;
  return t;
};

/**
 * Joins voice channel `channelId` of server `guildId`: a fresh token (the
 * server drops the user from any other channel), then one connection with
 * the user's switches applied. A refused token is said in a notice.
 */
export const joinVoice = async (guildId: string, channelId: string): Promise<void> => {
  const mine = ++generation;
  const latest = () => mine === generation;
  joining = true;
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
    setCall({ status: "connected" });
    t.setListening(!getCall().selfDeaf);
    const { outputId } = readVoicePrefs();
    if (outputId !== null) await switchAudioDevice("audiooutput", outputId);
    applyVoicePrefs();
  } finally {
    if (latest()) joining = false;
  }
};

/** Leaves voice: the server forgets the state at once, and the connection goes. */
export const leaveVoice = async (): Promise<void> => {
  generation += 1;
  joining = false;
  clearCall();
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

/** The microphone switch; applied at once in a call. */
export const setSelfMute = (on: boolean): void => {
  setCall({ selfMute: on });
  if (getCall().status !== "idle") void transport?.setMicrophone(micOn());
  void reconcileVoiceFlags();
};

/** The sound switch; deafened also means no microphone, as in Discord. */
export const setSelfDeaf = (on: boolean): void => {
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

/**
 * Starts sharing a screen, a window or a tab (the browser's picker) and says
 * what the viewers will hear. A closed picker changes nothing.
 */
export const startScreenShare = async (): Promise<void> => {
  if (!transport || getCall().status === "idle") return;
  const t = transport;
  const mine = generation;
  const result = await t.startScreenShare();
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
  const notice = shareNotice(result, currentBrowser(), canRestrictOwnAudio());
  if (notice !== null) notify(notice);
};

export const stopScreenShare = async (): Promise<void> => {
  setCall({ sharing: false });
  await transport?.stopScreenShare();
};
