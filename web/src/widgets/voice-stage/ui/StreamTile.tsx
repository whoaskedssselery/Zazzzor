import { AppWindow, ArrowLeftRight, Maximize, Minimize, Monitor, X } from "lucide-react";
import { type ReactNode, type RefObject, useEffect, useRef, useState } from "react";
import { VolumeSlider } from "@/entities/call";
import {
  attachOwnStream,
  attachStream,
  stopScreenShare,
  switchScreenShare,
  useNativeShare,
  watchStream,
} from "@/features/voice-call";
import { t } from "@/shared/i18n";
import { Badge, Button, cn, IconButton } from "@/shared/ui";

/**
 * A video framed to its own shape, as large as its tile allows: the frame,
 * the badge and the buttons stay on the picture whatever the tile's shape
 * (a tall phone screen, a wide monitor), instead of floating in the black
 * bars around a letterboxed video.
 */
const VideoFrame = ({ video, children }: { video: RefObject<HTMLVideoElement | null>; children: ReactNode }) => {
  const [ratio, setRatio] = useState(16 / 9);
  useEffect(() => {
    const el = video.current;
    if (!el) return;
    // "resize" fires when the first frame arrives and whenever the shared window changes size.
    const sync = () => {
      if (el.videoWidth > 0 && el.videoHeight > 0) setRatio(el.videoWidth / el.videoHeight);
    };
    sync();
    el.addEventListener("resize", sync);
    el.addEventListener("loadedmetadata", sync);
    return () => {
      el.removeEventListener("resize", sync);
      el.removeEventListener("loadedmetadata", sync);
    };
  }, [video]);
  return (
    <div className="flex size-full items-center justify-center [container-type:size]">
      <div
        data-video-frame
        className="relative overflow-hidden rounded-lg bg-[#000]"
        style={{ aspectRatio: ratio, width: `min(100cqw, calc(100cqh * ${String(ratio)}))` }}
      >
        {/* The sound plays with the call's, so deafening silences a stream too; the video itself is muted. */}
        <video ref={video} autoPlay muted playsInline className="size-full object-contain" />
        {children}
        {/* The stage's hover and focus ring, drawn round the picture rather than round its tile. */}
        <span
          aria-hidden
          className="pointer-events-none absolute inset-0 rounded-lg ring-inset transition-shadow duration-(--duration-fast) group-hover/tile:ring-2 group-hover/tile:ring-border-strong group-has-[>button:focus-visible]/tile:ring-2 group-has-[>button:focus-visible]/tile:ring-focus"
        />
      </div>
    </div>
  );
};

/** An icon button on the picture: no plate of its own, white with a shadow so it reads on any frame. */
const ON_PICTURE = "text-[#fff] hover:bg-[#000]/40 hover:text-[#fff] drop-shadow-[0_1px_2px_rgb(0_0_0/0.8)]";

/** The corner of the picture that holds its icon buttons, shown while the pointer is on the tile. */
const CORNER =
  "absolute top-2 right-2 z-10 flex gap-1 opacity-0 transition-opacity duration-(--duration-base) group-focus-within:opacity-100 group-hover:opacity-100 group-hover/tile:opacity-100";

/** The bottom-right corner, for the stream's volume. */
const BOTTOM =
  "absolute right-2 bottom-2 z-10 flex gap-2 opacity-0 transition-opacity duration-(--duration-base) group-focus-within:opacity-100 group-hover:opacity-100 group-hover/tile:opacity-100";

/**
 * Puts `target` on the whole screen, and in full screen takes it back: a way
 * out on the picture itself, not only Esc. Full screen takes the tile out of
 * the stage, whose button under it no longer catches the pointer, so the
 * tile catches it itself there (`[&:fullscreen]:pointer-events-auto`).
 */
const FullscreenButton = ({ target }: { target: RefObject<HTMLElement | null> }) => {
  const [full, setFull] = useState(false);
  useEffect(() => {
    const sync = () => {
      setFull(document.fullscreenElement != null && document.fullscreenElement === target.current);
    };
    // Also now: a tile drawn again while on the whole screen (the stage
    // rearranges as full screen comes and goes) must not start as "off".
    sync();
    document.addEventListener("fullscreenchange", sync);
    return () => {
      document.removeEventListener("fullscreenchange", sync);
    };
  }, [target]);
  return (
    <IconButton
      icon={full ? Minimize : Maximize}
      label={full ? t("voice:stage.exitFullscreen") : t("voice:stage.fullscreen")}
      tooltipSide="bottom"
      className={ON_PICTURE}
      onClick={() => {
        // What the document says, not what this button last heard: a press
        // always leaves full screen while it is on.
        if (document.fullscreenElement) void document.exitFullscreen();
        else void target.current?.requestFullscreen();
      }}
    />
  );
};

/**
 * The desktop app's share of the user, which the page has no picture of:
 * what is shared and how it goes, from the app's figures.
 */
const NativeOwnStream = ({ compact }: { compact: boolean }) => {
  const source = useNativeShare((s) => s.source);
  const stats = useNativeShare((s) => s.stats);
  if (!source) return null;
  const Icon = source.kind === "screen" ? Monitor : AppWindow;
  const figures = stats
    ? [
        `${String(stats.width)}×${String(stats.height)}`,
        t("voice:share.fps", { fps: Math.round(stats.sentFps) }),
        t("voice:stage.mbits", { value: stats.mbitS.toFixed(1) }),
        stats.sound ? t("voice:stage.withSound") : t("voice:stage.withoutSound"),
      ].join(" · ")
    : t("common:connecting");
  return (
    <article
      aria-label={t("voice:stage.yourShare")}
      className="relative flex size-full min-w-0 flex-col items-center justify-center gap-3 rounded-lg bg-panel p-4 text-center"
    >
      <Badge variant="live" className="absolute top-2 left-2">
        {t("shell:live")}
      </Badge>
      <Icon aria-hidden className="size-10 text-fg-muted" strokeWidth={1.5} />
      <div className="min-w-0 max-w-full">
        <p className="truncate font-medium">{source.name}</p>
        {!compact && <p className="mt-1 text-sm text-fg-muted tabular-nums">{figures}</p>}
      </div>
      {!compact && (
        <div className="flex flex-wrap justify-center gap-2">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              void switchScreenShare();
            }}
          >
            <ArrowLeftRight aria-hidden className="size-4" />
            {t("voice:share.switch")}
          </Button>
          <Button
            size="sm"
            variant="danger"
            onClick={() => {
              void stopScreenShare();
            }}
          >
            {t("voice:stage.stop")}
          </Button>
        </div>
      )}
    </article>
  );
};

/** The user's own screen share as the others see it: a local preview, so it costs no bandwidth. */
export const OwnStreamTile = ({ compact = false }: { compact?: boolean }) => {
  const native = useNativeShare((s) => s.source !== null);
  return native ? <NativeOwnStream compact={compact} /> : <BrowserOwnStream compact={compact} />;
};

const BrowserOwnStream = ({ compact }: { compact: boolean }) => {
  const tile = useRef<HTMLElement>(null);
  const video = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    const el = video.current;
    if (!el) return;
    return attachOwnStream(el);
  }, []);
  return (
    <article
      ref={tile}
      aria-label={t("voice:stage.yourShare")}
      className="group relative flex size-full min-w-0 [&:fullscreen]:pointer-events-auto"
    >
      <VideoFrame video={video}>
        <Badge variant="live" className="absolute top-2 left-2">
          {t("shell:live")}
        </Badge>
        <div data-controls className={cn(CORNER, compact && "hidden")}>
          <IconButton
            icon={ArrowLeftRight}
            label={t("voice:share.switch")}
            tooltipSide="bottom"
            className={ON_PICTURE}
            onClick={() => {
              void switchScreenShare();
            }}
          />
          <FullscreenButton target={tile} />
        </div>
        <div data-controls className={cn(BOTTOM, compact && "hidden")}>
          <Button
            size="sm"
            variant="danger"
            onClick={() => {
              void stopScreenShare();
            }}
          >
            {t("voice:stage.stop")}
          </Button>
        </div>
      </VideoFrame>
    </article>
  );
};

/**
 * Someone's screen share. Nothing is received until «Смотреть», as in
 * Discord: a stream costs the server 6–8 Mbit/s per viewer. While watched,
 * the video plays here and can go full screen.
 */
export const StreamTile = ({
  userId,
  name,
  watching,
  compact = false,
  onWatch,
}: {
  userId: string;
  name: string;
  watching: boolean;
  /** A small tile in the strip under the focused one: no buttons, a click focuses it. */
  compact?: boolean;
  /** After «Смотреть»: the stage brings the stream to the front, as Discord does. */
  onWatch?: () => void;
}) => {
  const tile = useRef<HTMLElement>(null);
  const video = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    const el = video.current;
    if (!watching || !el) return;
    return attachStream(userId, el);
  }, [userId, watching]);

  const title = t("voice:stage.shareOf", { name });
  return (
    <article
      ref={tile}
      aria-label={title}
      className={cn(
        "group relative flex size-full min-w-0 flex-col items-center justify-center overflow-hidden rounded-lg [&:fullscreen]:pointer-events-auto",
        !watching && "bg-rail",
      )}
    >
      {watching ? (
        <VideoFrame video={video}>
          <div data-controls className={cn(CORNER, compact && "hidden")}>
            <FullscreenButton target={tile} />
            <IconButton
              icon={X}
              label={t("voice:stage.stopWatching")}
              tooltipSide="bottom"
              className={ON_PICTURE}
              onClick={() => {
                watchStream(userId, false);
              }}
            />
          </div>
          <div data-controls className={cn(BOTTOM, compact && "hidden")}>
            <VolumeSlider inline kind="stream" userId={userId} label={t("member:streamVolume")} />
          </div>
        </VideoFrame>
      ) : (
        <>
          <Monitor aria-hidden className={cn("text-fg-faint", compact ? "size-6" : "size-10")} strokeWidth={1.5} />
          <p className="mt-2 max-w-full truncate px-4 text-sm text-fg-muted">{compact ? name : title}</p>
          {!compact && (
            <Button
              className="relative z-10 mt-3"
              onClick={() => {
                watchStream(userId, true);
                onWatch?.();
              }}
            >
              {t("voice:stage.watch")}
            </Button>
          )}
        </>
      )}
    </article>
  );
};
