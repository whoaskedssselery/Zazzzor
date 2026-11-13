import { useState } from "react";
import { cn } from "../cn";

export type Presence = "online" | "idle" | "dnd" | "offline";
export type AvatarSize = 20 | 24 | 32 | 40 | 80;

const graphemes = new Intl.Segmenter("ru", { granularity: "grapheme" });

/** The first user-perceived character (a letter with its accents, a whole emoji). */
export const firstGrapheme = (s: string): string => {
  for (const { segment } of graphemes.segment(s)) return segment;
  return "";
};

/** Up to two letters: the first letter of each of the first two words, upper-cased. */
export const initials = (name: string): string => {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const letters = words.slice(0, 2).map(firstGrapheme);
  return letters.join("").toLocaleUpperCase("ru") || "?";
};

/** A stable fill for a name, one of six muted avatar tones. */
export const avatarTone = (name: string): string => {
  let h = 0;
  for (const ch of name) h = (h * 31 + (ch.codePointAt(0) ?? 0)) >>> 0;
  return `var(--avatar-${String((h % 6) + 1)})`;
};

const presenceText: Record<Presence, string> = {
  online: "в сети",
  idle: "нет на месте",
  dnd: "не беспокоить",
  offline: "не в сети",
};

const dotFill: Record<Presence, string> = {
  online: "bg-online",
  idle: "bg-idle",
  dnd: "bg-dnd",
  offline: "bg-offline",
};

interface AvatarProps {
  name: string;
  src?: string | null | undefined;
  size?: AvatarSize;
  /** Draws the presence dot and adds the status to the accessible name. */
  status?: Presence;
  className?: string;
}

const fontSize: Record<AvatarSize, string> = {
  20: "text-[9px]",
  24: "text-[10px]",
  32: "text-xs",
  40: "text-sm",
  80: "text-xl",
};

/**
 * A round picture, or initials on a tone derived from the name. The status
 * dot is cut out of the surface behind it: set `--avatar-cutout` on the
 * container to that surface (the sidebar's by default).
 */
export const Avatar = ({ name, src, size = 40, status, className }: AvatarProps) => {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const label = status ? `${name}, ${presenceText[status]}` : name;
  const showImage = Boolean(src) && failedSrc !== src;
  const dot = Math.max(8, Math.round(size * 0.3));

  return (
    <span className={cn("relative inline-flex shrink-0", className)} style={{ width: size, height: size }}>
      {showImage && src ? (
        // biome-ignore lint/a11y/noNoninteractiveElementInteractions: onError is the image failing to load, not a user interaction.
        <img
          src={src}
          alt={label}
          width={size}
          height={size}
          className="size-full rounded-full object-cover"
          onError={() => {
            setFailedSrc(src);
          }}
        />
      ) : (
        <span
          role="img"
          aria-label={label}
          className={cn(
            "flex size-full items-center justify-center rounded-full font-semibold text-on-avatar select-none",
            fontSize[size],
          )}
          style={{ backgroundColor: avatarTone(name) }}
        >
          <span aria-hidden>{initials(name)}</span>
        </span>
      )}
      {status && (
        <StatusDot
          status={status}
          size={dot}
          ring={size >= 40 ? 3 : 2}
          className="absolute right-0 bottom-0 translate-x-[12%] translate-y-[12%]"
        />
      )}
    </span>
  );
};

/**
 * A presence dot alone: green online, an amber crescent away, a red disc
 * with a bar for do not disturb, a grey ring offline. The shapes differ, so
 * it reads without colour. `ring` is the cut-out border in the colour of
 * `--avatar-cutout` (the surface behind it).
 */
export const StatusDot = ({
  status,
  size = 10,
  ring = 0,
  className,
}: {
  status: Presence;
  size?: number;
  ring?: number;
  className?: string;
}) => {
  const cutout = "var(--avatar-cutout, var(--bg-sidebar))";
  return (
    <span
      aria-hidden
      data-status={status}
      className={cn("relative inline-block shrink-0 overflow-hidden rounded-full", dotFill[status], className)}
      style={{ width: size, height: size, boxShadow: ring > 0 ? `0 0 0 ${String(ring)}px ${cutout}` : undefined }}
    >
      {status === "offline" && <span className="absolute inset-[28%] rounded-full" style={{ background: cutout }} />}
      {status === "idle" && (
        <span className="absolute -top-[12%] -left-[12%] size-[62%] rounded-full" style={{ background: cutout }} />
      )}
      {status === "dnd" && (
        <span
          className="absolute inset-x-[22%] top-1/2 h-[24%] -translate-y-1/2 rounded-full"
          style={{ background: cutout }}
        />
      )}
    </span>
  );
};
