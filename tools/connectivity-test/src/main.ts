import { Room, RoomEvent, Track, type RemoteTrack } from "livekit-client";
import { detectBrowser } from "./browser";
import { isPickerCancelled, screenShareOptions } from "./media-options";
import { aggregateSamples, formatReport } from "./report";
import { shareReducer, type ShareEvent, type ShareState } from "./share-state";
import { summarizeStats, type ConnectivitySummary, type InboundVideoStat, type RTCStatsLike } from "./stats";
import { isTokenExpired, readTokenFromHash } from "./token";
import { h, renderLiveStats } from "./view";

const STATS_INTERVAL_MS = 2000;
const app = document.getElementById("app");
if (!app) throw new Error("#app not found");

const browser = detectBrowser(navigator.userAgent);
const token = readTokenFromHash(location.hash);

app.append(h("h1", { textContent: "Проверка связи" }));

if (!token) {
  app.append(h("p", { className: "warn", textContent: "Нужна ссылка с приглашением" }));
} else if (isTokenExpired(token, new Date())) {
  app.append(h("p", { className: "warn", textContent: "Ссылка истекла, попроси новую" }));
} else {
  start(app, token);
}

type StatsSource = { getStats(): Promise<RTCStatsReport> | undefined } | undefined;

async function readSummary(pc: StatsSource): Promise<ConnectivitySummary | null> {
  const report = await pc?.getStats();
  return report ? summarizeStats([...report.values()] as RTCStatsLike[]) : null;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

function start(root: HTMLElement, accessToken: string): void {
  const room = new Room({ adaptiveStream: true, dynacast: true });
  const samples: ConnectivitySummary[] = [];
  let prevInbound = new Map<string, InboundVideoStat>();
  let startedAt = new Date();
  let identity = "";
  let share: ShareState = { status: "idle" };
  let timer: number | undefined;

  const status = h("p", { textContent: "Не подключено" });
  const connectBtn = h("button", { textContent: "Подключиться" });
  const shareBtn = h("button", { textContent: "Начать демку" });
  const copyBtn = h("button", { textContent: "Скопировать отчёт" });
  const shareError = h("p", { className: "warn" });
  const participants = h("p");
  const videos = h("div", { className: "videos" });
  const live = h("div");
  const reportOut = h("pre");
  shareBtn.disabled = true;

  if (!browser.reliable60fps) {
    root.append(h("p", { className: "banner", textContent: "Для проверки 60 fps открой ссылку в Chrome или Edge" }));
  }
  root.append(status, h("p", {}, connectBtn, " ", shareBtn), shareError, participants, videos, live, h("p", {}, copyBtn), reportOut);

  const renderParticipants = (): void => {
    if (room.state !== "connected") return;
    const names = [room.localParticipant.identity, ...[...room.remoteParticipants.values()].map((p) => p.identity)];
    participants.textContent = `Участники: ${names.join(", ")}`;
  };

  const dispatch = (event: ShareEvent): void => {
    share = shareReducer(share, event);
    shareBtn.textContent = share.status === "sharing" ? "Остановить" : share.status === "starting" ? "Запуск…" : "Начать демку";
    shareBtn.disabled = room.state !== "connected" || share.status === "starting";
    shareError.textContent = share.status === "error" ? `Не удалось начать демку: ${share.message}` : "";
  };

  const attach = (track: RemoteTrack, label: string): void => {
    const media = track.attach();
    if (track.kind === Track.Kind.Video) {
      media.className = "demo";
      videos.append(h("figure", {}, media, h("figcaption", { textContent: label })));
    } else {
      media.hidden = true;
      videos.append(media);
    }
  };

  room
    .on(RoomEvent.TrackSubscribed, (track, _pub, participant) => attach(track, participant.identity))
    .on(RoomEvent.TrackUnsubscribed, (track) => {
      for (const el of track.detach()) (el.closest("figure") ?? el).remove();
    })
    .on(RoomEvent.ParticipantConnected, renderParticipants)
    .on(RoomEvent.ParticipantDisconnected, renderParticipants)
    .on(RoomEvent.LocalTrackUnpublished, (pub) => {
      if (pub.source === Track.Source.ScreenShare) dispatch({ type: "stopped" });
    })
    .on(RoomEvent.Disconnected, () => {
      status.textContent = "Соединение потеряно";
      connectBtn.disabled = false;
      dispatch({ type: "stopped" });
    });

  const refreshReport = (): string => {
    const text = samples.length
      ? formatReport({ name: identity, browser: browser.name, startedAt }, aggregateSamples(samples))
      : "Данных пока нет: сначала подключись и подожди пару секунд";
    reportOut.textContent = text;
    return text;
  };

  const tick = async (): Promise<void> => {
    // pcManager в livekit-client 2.23.0 — внутренний API (RTCEngine.pcManager), может измениться.
    const pcs = room.engine.pcManager;
    if (!pcs) return;
    // Транспорты разбираем по отдельности: id записей getStats() у разных
    // RTCPeerConnection могут совпадать, и общий Map перепутал бы их.
    const [pub, sub] = await Promise.all([readSummary(pcs.publisher), readSummary(pcs.subscriber)]);
    const base = [pub, sub].find((s) => s && s.protocol !== "unknown") ?? pub ?? sub;
    if (!base) return;
    const summary: ConnectivitySummary = { ...base, outbound: pub?.outbound ?? null, inbound: sub?.inbound ?? [] };
    samples.push(summary);
    live.replaceChildren(renderLiveStats(summary, prevInbound, aggregateSamples(samples)));
    prevInbound = new Map(summary.inbound.map((i) => [i.trackId, i]));
  };

  connectBtn.addEventListener("click", async () => {
    connectBtn.disabled = true;
    status.textContent = "Подключаемся…";
    try {
      await room.connect(`wss://${location.host}`, accessToken);
    } catch (err) {
      status.textContent = `Не удалось подключиться: ${errorMessage(err)}`;
      connectBtn.disabled = false;
      return;
    }
    identity = room.localParticipant.identity;
    startedAt = new Date();
    status.textContent = `Подключено как ${identity}`;
    renderParticipants();
    dispatch({ type: "stopped" });
    try {
      await room.localParticipant.setMicrophoneEnabled(true);
    } catch {
      status.textContent += " (микрофон недоступен)";
    }
    for (const p of room.remoteParticipants.values()) {
      for (const pub of p.trackPublications.values()) if (pub.track) attach(pub.track as RemoteTrack, p.identity);
    }
    window.clearInterval(timer);
    timer = window.setInterval(() => void tick().catch(() => undefined), STATS_INTERVAL_MS);
  });

  shareBtn.addEventListener("click", async () => {
    if (share.status === "sharing") {
      await room.localParticipant.setScreenShareEnabled(false);
      dispatch({ type: "stopped" });
      return;
    }
    dispatch({ type: "start" });
    const { capture, publish } = screenShareOptions();
    try {
      await room.localParticipant.setScreenShareEnabled(true, capture, publish);
      dispatch({ type: "started" });
    } catch (err) {
      dispatch(isPickerCancelled(err) ? { type: "cancelled" } : { type: "failed", message: errorMessage(err) });
    }
  });

  copyBtn.addEventListener("click", async () => {
    const text = refreshReport();
    try {
      await navigator.clipboard.writeText(text);
      copyBtn.textContent = "Скопировано";
    } catch {
      copyBtn.textContent = "Скопируй текст ниже вручную";
    }
  });

  window.addEventListener("pagehide", () => {
    window.clearInterval(timer);
    void room.disconnect();
  });
}
