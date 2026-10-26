import { useState } from "react";
import { cn } from "../cn";

export type Presence = "online" | "offline";
export type AvatarSize = 20 | 24 | 32 | 40 | 80;

const graphemes = new Intl.Segmenter("ru", { granularity: "grapheme" });

/** The first user-perceived character (a letter with its accents, a whole emoji). */
export function firstGrapheme(s: string): string {
  for (const { segment } of graphemes.segment(s)) return segment;
  return "";
}

/** Up to two letters: the first letter of each of the first two words, upper-cased. */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  const letters = words.slice(0, 2).map(firstGrapheme);
  return letters.join("").toLocaleUpperCase("ru") || "?";
}

/** A stable fill for a name, one of six muted avatar tones. */
export function avatarTone(name: string): string {
  let h = 0;
  for (const ch of name) h = (h * 31 + (ch.codePointAt(0) ?? 0)) >>> 0;
  return `var(--avatar-${String((h % 6) + 1)})`;
}

const presenceText: Record<Presence, string> = { online: "в сети", offline: "не в сети" };

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
export function Avatar({ name, src, size = 40, status, className }: AvatarProps) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const label = status ? `${name}, ${presenceText[status]}` : name;
  const showImage = Boolean(src) && failedSrc !== src;
  const dot = Math.max(8, Math.round(size * 0.3));

  return (
    <span className={cn("relative inline-flex shrink-0", className)} style={{ width: size, height: size }}>
      {showImage && src ? (
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
        <span
          aria-hidden
          data-status={status}
          className={cn(
            "absolute right-0 bottom-0 translate-x-[12%] translate-y-[12%] rounded-full",
            status === "online" ? "bg-online" : "bg-offline",
          )}
          style={{
            width: dot,
            height: dot,
            boxShadow: `0 0 0 ${String(size >= 40 ? 3 : 2)}px var(--avatar-cutout, var(--bg-sidebar))`,
          }}
        >
          {status === "offline" && (
            <span className="absolute inset-[28%] rounded-full" style={{ background: "var(--avatar-cutout, var(--bg-sidebar))" }} />
          )}
        </span>
      )}
    </span>
  );
}
