import { type RemoteTrack, Room, RoomEvent, Track } from "livekit-client";
import { detectBrowser } from "./browser";
import {
  type ConnectionEvent,
  type ConnectionEvents,
  countConnectionEvent,
  disconnectMessage,
  emptyConnectionEvents,
} from "./connection-events";
import { type Language, translations } from "./locale";
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

const url = new URL(location.href);
const language: Language = url.searchParams.get("lang") === "ru" ? "ru" : "en";
const text = translations[language];
document.documentElement.lang = language;
document.title = `Zazzzor — ${text.title}`;

const languages = h("nav");
languages.setAttribute("aria-label", text.language);
for (const [locale, label] of [["en", "English"], ["ru", "Русский"]] as const) {
  if (locale === language) {
    const active = h("span", { textContent: label });
    active.lang = locale;
    active.setAttribute("aria-current", "true");
    languages.append(active);
  } else {
    const link = h("a", { textContent: label });
    url.searchParams.set("lang", locale);
    link.href = url.href;
    link.lang = locale;
    link.hreflang = locale;
    languages.append(link);
  }
  if (locale === "en") languages.append(" · ");
}
app.append(h("h1", { textContent: text.title }), languages);

if (!token) {
  app.append(h("p", { className: "warn", textContent: text.missingInvite }));
} else if (isTokenExpired(token, new Date())) {
  app.append(h("p", { className: "warn", textContent: text.expiredInvite }));
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
  // A measurement phase can be reset while the connection stays open.
  const samples: ConnectivitySummary[] = [];
  let prevInbound = new Map<string, InboundVideoStat>();
  let events: ConnectionEvents = emptyConnectionEvents();
  let startedAt = new Date();
  let phaseStarted = false;
  const labels = new Map<string, string>();
  let identity = "";
  let share: ShareState = { status: "idle" };
  let timer: number | undefined;

  const status = h("p", { textContent: text.disconnected });
  const connectBtn = h("button", { textContent: text.connect });
  const shareBtn = h("button", { textContent: text.startShare });
  const copyBtn = h("button", { textContent: text.copyReport });
  const resetBtn = h("button", { textContent: text.reset });
  const shareError = h("p", { className: "warn" });
  const participants = h("p");
  const videos = h("div", { className: "videos" });
  const live = h("div");
  const reportOut = h("pre");
  shareBtn.disabled = true;
  resetBtn.disabled = true;

  if (!browser.reliable60fps) {
    root.append(h("p", { className: "banner", textContent: text.browserWarning }));
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
    participants.textContent = text.participants + names.join(", ");
  };

  const dispatch = (event: ShareEvent): void => {
    share = shareReducer(share, event);
    shareBtn.textContent =
      share.status === "sharing" ? text.stopShare : share.status === "starting" ? text.startingShare : text.startShare;
    shareBtn.disabled = room.state !== "connected" || share.status === "starting";
    shareError.textContent = share.status === "error" ? text.shareFailed + share.message : "";
  };

  const renderLive = (summary: ConnectivitySummary | null): void => {
    live.replaceChildren(
      renderLiveStats(summary, language, { prevInbound, agg: samples.length ? aggregateSamples(samples) : null, events, labels }),
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
      media.title = text.fullscreen;
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
      status.textContent = text.reconnecting;
      record({ type: "reconnecting" });
    })
    .on(RoomEvent.Reconnected, () => {
      status.textContent = text.connectedAs + identity + text.reconnected;
      record({ type: "reconnected" });
    })
    .on(RoomEvent.Disconnected, (reason) => {
      window.clearInterval(timer);
      status.textContent = disconnectMessage(reason, language);
      connectBtn.disabled = false;
      dispatch({ type: "stopped" });
      record({ type: "disconnected", reason });
    });

  const refreshReport = (): string => {
    const report = samples.length
      ? formatReport(
          { name: identity, browser: browser.name, userAgent: navigator.userAgent, startedAt, now: new Date() },
          aggregateSamples(samples),
          events,
        )
      : text.reportEmpty;
    reportOut.textContent = report;
    return report;
  };

  const tick = async (): Promise<void> => {
    // RTCEngine.pcManager is internal to livekit-client and may change.
    const pcs = room.engine.pcManager;
    if (!pcs) return;
    // Read the transports separately because their getStats() IDs may overlap.
    const [pub, sub] = await Promise.all([readSummary(pcs.publisher), readSummary(pcs.subscriber)]);
    if (!pub && !sub) return;
    const summary = mergeSummaries(pub, sub);
    samples.push(summary);
    renderLive(summary);
    prevInbound = new Map(summary.inbound.map((i) => [i.trackId, i]));
  };

  connectBtn.addEventListener("click", async () => {
    connectBtn.disabled = true;
    status.textContent = text.connecting;
    try {
      await room.connect(signalUrl(location), accessToken);
    } catch (err) {
      status.textContent = text.connectionFailed + errorMessage(err);
      connectBtn.disabled = false;
      return;
    }
    identity = room.localParticipant.identity;
    // Reconnecting after a drop continues the same phase, so the drop stays in the report.
    if (!phaseStarted) {
      startedAt = new Date();
      phaseStarted = true;
    }
    status.textContent = text.connectedAs + identity;
    resetBtn.disabled = false;
    renderParticipants();
    dispatch({ type: "stopped" });
    try {
      await room.localParticipant.setMicrophoneEnabled(true);
    } catch {
      status.textContent += text.microphoneUnavailable;
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
    const report = refreshReport();
    try {
      await navigator.clipboard.writeText(report);
      copyBtn.textContent = text.copied;
    } catch {
      copyBtn.textContent = text.copyManually;
    }
  });

  resetBtn.addEventListener("click", () => {
    samples.length = 0;
    prevInbound = new Map();
    events = emptyConnectionEvents();
    startedAt = new Date();
    copyBtn.textContent = text.copyReport;
    reportOut.textContent = "";
    renderLive(null);
  });

  window.addEventListener("pagehide", () => {
    window.clearInterval(timer);
    void room.disconnect();
  });
}
