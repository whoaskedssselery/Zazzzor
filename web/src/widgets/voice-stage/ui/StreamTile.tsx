import { Maximize, Monitor } from "lucide-react";
import { useEffect, useRef } from "react";
import { attachOwnStream, attachStream, stopScreenShare, watchStream } from "@/features/voice-call";
import { Badge, Button, cn } from "@/shared/ui";

/** The user's own screen share as the others see it: a local preview, so it costs no bandwidth. */
export const OwnStreamTile = ({ compact = false }: { compact?: boolean }) => {
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
      aria-label="Ваша демонстрация экрана"
      className="group relative flex size-full min-w-0 overflow-hidden rounded-lg bg-black"
    >
      <video ref={video} autoPlay muted playsInline className="size-full object-contain" />
      <Badge variant="live" className="absolute top-2 left-2">
        В ЭФИРЕ
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
          Во весь экран
        </Button>
        <Button
          size="sm"
          variant="danger"
          onClick={() => {
            void stopScreenShare();
          }}
        >
          Остановить
        </Button>
      </div>
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

  const title = `${name} — демонстрация экрана`;
  return (
    <article
      ref={tile}
      aria-label={title}
      className="group relative flex size-full min-w-0 flex-col items-center justify-center overflow-hidden rounded-lg bg-rail"
    >
      {watching ? (
        <>
          {/* The stream's sound plays with the call's, so deafening silences it too; the video itself is muted. */}
          <video ref={video} autoPlay muted playsInline className="size-full bg-black object-contain" />
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
              Во весь экран
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                watchStream(userId, false);
              }}
            >
              Перестать смотреть
            </Button>
          </div>
        </>
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
              Смотреть
            </Button>
          )}
        </>
      )}
    </article>
  );
};
