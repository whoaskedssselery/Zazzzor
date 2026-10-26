import { type RemoteTrack, Room, RoomEvent, Track } from "livekit-client";
import { detectBrowser } from "./browser";
import {
  type ConnectionEvent,
  type ConnectionEvents,
  countConnectionEvent,
  disconnectMessage,
  emptyConnectionEvents,
} from "./connection-events";
import { isPickerCancelled, roomOptions, screenShareOptions } from "./media-options";
import { aggregateSamples, formatReport } from "./report";
import { type ShareEvent, type ShareState, shareReducer } from "./share-state";
import { signalUrl } from "./signal-url";
import {
  type ConnectivitySummary,
  type InboundVideoStat,
  mergeSummaries,
  type RTCStatsLike,
  summarizeStats,
} from "./stats";
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
  const room = new Room(roomOptions());
  // One measurement phase (e.g. «одна демка»): reset by «Новый замер», the connection stays.
  const samples: ConnectivitySummary[] = [];
  let prevInbound = new Map<string, InboundVideoStat>();
  let events: ConnectionEvents = emptyConnectionEvents();
  let startedAt = new Date();
  let phaseStarted = false;
  const labels = new Map<string, string>();
  let identity = "";
  let share: ShareState = { status: "idle" };
  let timer: number | undefined;

  const status = h("p", { textContent: "Не подключено" });
  const connectBtn = h("button", { textContent: "Подключиться" });
  const shareBtn = h("button", { textContent: "Начать демку" });
  const copyBtn = h("button", { textContent: "Скопировать отчёт" });
  const resetBtn = h("button", { textContent: "Новый замер" });
  const shareError = h("p", { className: "warn" });
  const participants = h("p");
  const videos = h("div", { className: "videos" });
  const live = h("div");
  const reportOut = h("pre");
  shareBtn.disabled = true;
  resetBtn.disabled = true;

  if (!browser.reliable60fps) {
    root.append(h("p", { className: "banner", textContent: "Для проверки 60 fps открой ссылку в Chrome или Edge" }));
  }
  root.append(
    status,
    h("p", {}, connectBtn, " ", shareBtn),
    shareError,
    participants,
    videos,
    live,
    h("p", {}, copyBtn, " ", resetBtn),
    reportOut,
  );

  const renderParticipants = (): void => {
    if (room.state !== "connected") return;
    const names = [room.localParticipant.identity, ...[...room.remoteParticipants.values()].map((p) => p.identity)];
    participants.textContent = `Участники: ${names.join(", ")}`;
  };

  const dispatch = (event: ShareEvent): void => {
    share = shareReducer(share, event);
    shareBtn.textContent =
      share.status === "sharing" ? "Остановить" : share.status === "starting" ? "Запуск…" : "Начать демку";
    shareBtn.disabled = room.state !== "connected" || share.status === "starting";
    shareError.textContent = share.status === "error" ? `Не удалось начать демку: ${share.message}` : "";
  };

  const renderLive = (summary: ConnectivitySummary | null): void => {
    live.replaceChildren(
      renderLiveStats(summary, { prevInbound, agg: samples.length ? aggregateSamples(samples) : null, events, labels }),
    );
  };

  const record = (event: ConnectionEvent): void => {
    events = countConnectionEvent(events, event);
    renderLive(samples[samples.length - 1] ?? null);
  };

  const attach = (track: RemoteTrack, label: string): void => {
    const media = track.attach();
    if (media instanceof HTMLVideoElement) {
      // getStats() names inbound streams by this id (trackIdentifier).
      labels.set(track.mediaStreamTrack.id, label);
      media.className = "demo";
      media.playsInline = true;
      // The video track has no sound (demo audio is a separate track with its
      // own <audio>); muted also lets the browser autoplay it.
      media.muted = true;
      media.title = "Двойной клик — во весь экран";
      media.addEventListener("dblclick", () => {
        if (document.fullscreenElement) void document.exitFullscreen();
        else media.requestFullscreen().catch((err: unknown) => console.warn("fullscreen failed", err));
      });
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
    .on(RoomEvent.Reconnecting, () => {
      status.textContent = "Связь прервалась, переподключаемся…";
      record({ type: "reconnecting" });
    })
    .on(RoomEvent.Reconnected, () => {
      status.textContent = `Подключено как ${identity} (после переподключения)`;
      record({ type: "reconnected" });
    })
    .on(RoomEvent.Disconnected, (reason) => {
      window.clearInterval(timer);
      status.textContent = disconnectMessage(reason);
      connectBtn.disabled = false;
      dispatch({ type: "stopped" });
      record({ type: "disconnected", reason });
    });

  const refreshReport = (): string => {
    const text = samples.length
      ? formatReport(
          { name: identity, browser: browser.name, userAgent: navigator.userAgent, startedAt, now: new Date() },
          aggregateSamples(samples),
          events,
        )
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
    if (!pub && !sub) return;
    const summary = mergeSummaries(pub, sub);
    samples.push(summary);
    renderLive(summary);
    prevInbound = new Map(summary.inbound.map((i) => [i.trackId, i]));
  };

  connectBtn.addEventListener("click", async () => {
    connectBtn.disabled = true;
    status.textContent = "Подключаемся…";
    try {
      await room.connect(signalUrl(location), accessToken);
    } catch (err) {
      status.textContent = `Не удалось подключиться: ${errorMessage(err)}`;
      connectBtn.disabled = false;
      return;
    }
    identity = room.localParticipant.identity;
    // Reconnecting after a drop continues the same phase, so the drop stays in the report.
    if (!phaseStarted) {
      startedAt = new Date();
      phaseStarted = true;
    }
    status.textContent = `Подключено как ${identity}`;
    resetBtn.disabled = false;
    renderParticipants();
    dispatch({ type: "stopped" });
    try {
      await room.localParticipant.setMicrophoneEnabled(true);
    } catch {
      status.textContent += " (микрофон недоступен)";
    }
    window.clearInterval(timer);
    timer = window.setInterval(() => {
      tick().catch((err: unknown) => console.warn("stats tick failed", err));
    }, STATS_INTERVAL_MS);
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

  resetBtn.addEventListener("click", () => {
    samples.length = 0;
    prevInbound = new Map();
    events = emptyConnectionEvents();
    startedAt = new Date();
    copyBtn.textContent = "Скопировать отчёт";
    reportOut.textContent = "";
    renderLive(null);
  });

  window.addEventListener("pagehide", () => {
    window.clearInterval(timer);
    void room.disconnect();
  });
}
