import { AppWindow, Maximize, Monitor } from "lucide-react";
import { type ReactNode, type RefObject, useEffect, useRef, useState } from "react";
import { VolumeSlider } from "@/entities/call";
import { attachOwnStream, attachStream, stopScreenShare, useNativeShare, watchStream } from "@/features/voice-call";
import { t } from "@/shared/i18n";
import { Badge, Button, cn } from "@/shared/ui";

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
        className="relative overflow-hidden rounded-lg bg-[#000]"
        style={{ aspectRatio: ratio, width: `min(100cqw, calc(100cqh * ${String(ratio)}))` }}
      >
        {/* The sound plays with the call's, so deafening silences a stream too; the video itself is muted. */}
        <video ref={video} autoPlay muted playsInline className="size-full object-contain" />
        {children}
      </div>
    </div>
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
        <Button
          size="sm"
          variant="danger"
          onClick={() => {
            void stopScreenShare();
          }}
        >
          {t("voice:stage.stop")}
        </Button>
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
    <article ref={tile} aria-label={t("voice:stage.yourShare")} className="group relative flex size-full min-w-0">
      <VideoFrame video={video}>
        <Badge variant="live" className="absolute top-2 left-2">
          {t("shell:live")}
        </Badge>
        <div
          className={cn(
            "absolute right-2 bottom-2 z-10 flex gap-2 opacity-0 transition-opacity duration-(--duration-base) group-focus-within:opacity-100 group-hover:opacity-100",
            compact && "hidden",
          )}
        >
          <Button
            size="sm"
            variant="secondary"
            onClick={() => {
              void tile.current?.requestFullscreen();
            }}
          >
            <Maximize aria-hidden className="size-4" />
            {t("voice:stage.fullscreen")}
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
      </VideoFrame>
    </article>
  );
};

/**
 * Someone's screen share. Nothing is received until «Смотреть», as in
 * Discord: a stream costs the server 6–8 Mbit/s per viewer. While watched,
 * the video plays here and can go full screen (Esc leaves it).
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
        "group relative flex size-full min-w-0 flex-col items-center justify-center overflow-hidden rounded-lg",
        !watching && "bg-rail",
      )}
    >
      {watching ? (
        <VideoFrame video={video}>
          <div
            className={cn(
              "absolute right-2 bottom-2 z-10 flex gap-2 opacity-0 transition-opacity duration-(--duration-base) group-focus-within:opacity-100 group-hover:opacity-100",
              compact && "hidden",
            )}
          >
            <VolumeSlider inline kind="stream" userId={userId} label={t("member:streamVolume")} />
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                void tile.current?.requestFullscreen();
              }}
            >
              <Maximize aria-hidden className="size-4" />
              {t("voice:stage.fullscreen")}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                watchStream(userId, false);
              }}
            >
              {t("voice:stage.stopWatching")}
            </Button>
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
