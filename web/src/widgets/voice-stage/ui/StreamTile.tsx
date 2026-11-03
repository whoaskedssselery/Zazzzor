import { Maximize, Monitor } from "lucide-react";
import { useEffect, useRef } from "react";
import { attachStream, watchStream } from "@/features/voice-call";
import { Button } from "@/shared/ui";

/**
 * Someone's screen share. Nothing is received until «Смотреть», as in
 * Discord: a stream costs the server 6–8 Mbit/s per viewer. While watched,
 * the video plays here and can go full screen (Esc leaves it).
 */
export const StreamTile = ({ userId, name, watching }: { userId: string; name: string; watching: boolean }) => {
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
      className="relative flex aspect-video min-w-0 flex-col items-center justify-center overflow-hidden rounded-lg bg-rail"
    >
      {watching ? (
        <>
          {/* The stream's sound plays with the call's, so deafening silences it too; the video itself is muted. */}
          <video ref={video} autoPlay muted playsInline className="size-full bg-black object-contain" />
          <div className="absolute right-2 bottom-2 flex gap-2">
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
          <Monitor aria-hidden className="size-10 text-fg-faint" strokeWidth={1.5} />
          <p className="mt-2 max-w-full truncate px-4 text-sm text-fg-muted">{title}</p>
          <Button
            className="mt-3"
            onClick={() => {
              watchStream(userId, true);
            }}
          >
            Смотреть
          </Button>
        </>
      )}
    </article>
  );
};
