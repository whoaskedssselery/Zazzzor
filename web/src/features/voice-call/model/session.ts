import { type CallTransport, clearCall, type EndCause, getCall, loadLiveKitTransport, setCall } from "@/entities/call";
import { getGuildsState, myVoice } from "@/entities/guild";
import { api, errorText, unwrap } from "@/shared/api";

export const CALL_REPLACED = "Вы подключились к голосу в другом месте";
export const CALL_REMOVED = "Вас отключили от голосового канала";
export const CALL_FAILED = "Не удалось подключиться к голосу";
export const MIC_DENIED = "Нет доступа к микрофону";
export const NO_CONNECT = "Нет права подключаться к этому каналу";

type Factory = () => CallTransport | Promise<CallTransport>;

let factory: Factory = async () => (await loadLiveKitTransport())(() => ({ inputId: null, noise: "standard" }));
let transport: CallTransport | null = null;
const notices = new Set<(text: string) => void>();

/** How calls are made: LiveKit by default, a fake in tests. Forgets the current transport. */
export const setTransportFactory = (f: Factory): void => {
  factory = f;
  transport = null;
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

/** The microphone sends when the user has it on, is not deafened and is not muted by a moderator. */
const micOn = (): boolean => {
  const { selfMute, selfDeaf } = getCall();
  return !selfMute && !selfDeaf && !(myVoice(getGuildsState())?.mute ?? false);
};

const ended = (cause: EndCause) => {
  if (getCall().status === "idle") return;
  clearCall();
  if (cause === "replaced") notify(CALL_REPLACED);
  else if (cause === "removed") {
    // LiveKit says "removed" both for a moderator and for a newer token of
    // the same user: if the server still has the user in voice, it is the latter.
    notify(myVoice(getGuildsState()) === null ? CALL_REMOVED : CALL_REPLACED);
  } else if (cause === "failed") notify(CALL_FAILED);
};

/** The transport, made once and wired to the call store. */
const ensureTransport = async (): Promise<CallTransport> => {
  if (transport) return transport;
  const t = await factory();
  t.on("link", (state, cause) => {
    if (state === "disconnected") ended(cause ?? "failed");
    else if (getCall().status !== "idle") setCall({ status: state });
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
  transport = t;
  return t;
};

/**
 * Joins voice channel `channelId` of server `guildId`: a fresh token (the
 * server drops the user from any other channel), then one connection with
 * the user's switches applied. A refused token is said in a notice.
 */
export const joinVoice = async (guildId: string, channelId: string): Promise<void> => {
  let joined: { token: string; url: string };
  try {
    joined = unwrap(await api.POST("/channels/{channel_id}/voice", { params: { path: { channel_id: channelId } } }));
  } catch (e) {
    notify(errorText(e));
    return;
  }
  const t = await ensureTransport();
  await t.disconnect();
  setCall({ status: "connecting", guildId, channelId, micDenied: false, speaking: [], streams: [], watching: [] });
  try {
    await t.connect(joined.url, joined.token);
  } catch {
    clearCall();
    notify(CALL_FAILED);
    return;
  }
  setCall({ status: "connected" });
  t.setListening(!getCall().selfDeaf);
  await t.setMicrophone(micOn());
};

/** Leaves voice: the server forgets the state at once, and the connection goes. */
export const leaveVoice = async (): Promise<void> => {
  clearCall();
  await transport?.disconnect();
  try {
    unwrap(await api.DELETE("/users/@me/voice"));
  } catch (e) {
    notify(errorText(e));
  }
};

/** Ends the call here without telling the server (the session ended; the server knows). */
export const endCall = async (cause: EndCause): Promise<void> => {
  ended(cause);
  clearCall();
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
  void transport?.setMicrophone(micOn());
  void reconcileVoiceFlags();
};

/** The sound switch; deafened also means no microphone, as in Discord. */
export const setSelfDeaf = (on: boolean): void => {
  setCall({ selfDeaf: on });
  transport?.setListening(!on);
  void transport?.setMicrophone(micOn());
  void reconcileVoiceFlags();
};

let syncing = false;

/**
 * Tells the server the user's switches when the voice state it has differs:
 * after joining (the state starts with both off) and after READY (the API
 * keeps them in memory only). Also turns the microphone off when a moderator
 * mutes the user.
 */
export const reconcileVoiceFlags = async (): Promise<void> => {
  const mine = myVoice(getGuildsState());
  const { status, channelId, selfMute, selfDeaf } = getCall();
  if (status === "idle" || mine?.channel_id !== channelId || syncing) return;
  if (mine.mute) void transport?.setMicrophone(false);
  if (mine.self_mute === selfMute && mine.self_deaf === selfDeaf) return;
  syncing = true;
  try {
    unwrap(await api.PATCH("/users/@me/voice-state", { body: { self_mute: selfMute, self_deaf: selfDeaf } }));
  } catch {
    // The next state from the server, or the next switch, tries again.
  } finally {
    syncing = false;
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
