/**
 * A screen share's fallback from 1080p to 720p. The encoder keeps the
 * resolution (maintain-resolution), so when the link is short it drops
 * frames: measured, a sender whose bandwidth estimate fell to ~2 Mbit/s held
 * 1080p at 3–5 fps. A moving 720p at 30 fps reads better than that, so the
 * share steps down while frames are being lost, and back up once the link
 * has room for 1080p again.
 */

/** How often the sender's stats are read. */
export const SAMPLE_MS = 2_000;
/** A moving screen: the capture gives at least this many fps. */
const MOVING_FPS = 30;
/** The least a share may send: below it, 720p at full speed beats 1080p stuttering. */
const STARVED_FPS = 30;
/** Starved samples in a row before stepping down (4 s). */
const DOWN_AFTER = 2;
/** The bandwidth estimate that carries 1080p again, in bit/s. */
const ROOM_FOR_1080P = 6_000_000;
/** Samples with room in a row before stepping up (10 s), doubled per fall back, up to a minute. */
const UP_AFTER = 5;
const UP_AFTER_MAX = 30;

/** 1080p captured; 720p sent as 1080p scaled down by 1.5. */
export const SCALE_720P = 1.5;

/** One reading of the sender. */
export interface ScreenSample {
  /** Frames the capture gave, per second. */
  sourceFps: number;
  /** Frames the encoder sent, per second. */
  sentFps: number;
  /** The sender's bandwidth estimate in bit/s; null when the browser does not tell. */
  bandwidth: number | null;
}

export interface ScreenQuality {
  /** 1 for 1080p, SCALE_720P for 720p. */
  scale: number;
  /** Starved samples in a row (at 1080p). */
  starved: number;
  /** Samples in a row with room for 1080p (at 720p). */
  roomy: number;
  /** How many times the share has stepped down: each makes the way back up slower. */
  falls: number;
}

export const INITIAL_QUALITY: ScreenQuality = { scale: 1, starved: 0, roomy: 0, falls: 0 };

/** The quality after one more sample. */
export const nextQuality = (q: ScreenQuality, s: ScreenSample): ScreenQuality => {
  if (q.scale === 1) {
    const starved = s.sourceFps >= MOVING_FPS && s.sentFps < STARVED_FPS ? q.starved + 1 : 0;
    if (starved < DOWN_AFTER) return { ...q, starved };
    return { scale: SCALE_720P, starved: 0, roomy: 0, falls: q.falls + 1 };
  }
  const roomy = s.bandwidth !== null && s.bandwidth >= ROOM_FOR_1080P ? q.roomy + 1 : 0;
  const needed = Math.min(UP_AFTER * 2 ** (q.falls - 1), UP_AFTER_MAX);
  if (roomy < needed) return { ...q, roomy };
  return { ...q, scale: 1, starved: 0, roomy: 0 };
};

interface Counters {
  at: number;
  source: number;
  sent: number;
}

/** Frame counters and the bandwidth estimate from a sender's stats. */
const read = (report: RTCStatsReport): { counters: Counters; bandwidth: number | null } | null => {
  let source: number | null = null;
  let sent: number | null = null;
  let at = 0;
  let bandwidth: number | null = null;
  for (const s of report.values() as IterableIterator<Record<string, unknown>>) {
    if (s.type === "media-source" && s.kind === "video") source = Number(s.frames ?? 0);
    if (s.type === "outbound-rtp" && s.kind === "video") {
      sent = Number(s.framesEncoded ?? 0);
      at = Number(s.timestamp);
    }
    if (s.type === "candidate-pair" && s.nominated === true && typeof s.availableOutgoingBitrate === "number") {
      bandwidth = s.availableOutgoingBitrate;
    }
  }
  if (source === null || sent === null) return null;
  return { counters: { at, source, sent }, bandwidth };
};

/**
 * Keeps the screen share on `sender` at 1080p while the link carries it, at
 * 720p while it does not. Returns the stop.
 */
export const steerScreenQuality = (sender: RTCRtpSender): (() => void) => {
  let quality = INITIAL_QUALITY;
  let last: Counters | null = null;
  let busy = false;
  const tick = async () => {
    if (busy) return;
    busy = true;
    try {
      const now = read(await sender.getStats());
      if (now === null) return;
      const prev = last;
      last = now.counters;
      if (prev === null || now.counters.at <= prev.at) return;
      const seconds = (now.counters.at - prev.at) / 1000;
      const next = nextQuality(quality, {
        sourceFps: (now.counters.source - prev.source) / seconds,
        sentFps: (now.counters.sent - prev.sent) / seconds,
        bandwidth: now.bandwidth,
      });
      if (next.scale !== quality.scale) {
        const params = sender.getParameters();
        for (const e of params.encodings) e.scaleResolutionDownBy = next.scale;
        await sender.setParameters(params);
      }
      quality = next;
    } catch {
      // A sender that went away mid-read: the next tick, or the stop, settles it.
    } finally {
      busy = false;
    }
  };
  const timer = setInterval(() => {
    void tick();
  }, SAMPLE_MS);
  return () => {
    clearInterval(timer);
  };
};
