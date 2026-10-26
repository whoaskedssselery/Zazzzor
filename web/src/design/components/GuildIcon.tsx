import { useState } from "react";
import { cn } from "../cn";
import { firstGrapheme } from "./Avatar";

/** Up to three letters, one per word, upper-cased (Discord's server acronym): «Вечерний сеанс» → «ВС». */
export function guildAcronym(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  return words.slice(0, 3).map(firstGrapheme).join("").toLocaleUpperCase("ru") || "?";
}

interface GuildIconProps {
  name: string;
  src?: string | null | undefined;
  /** The open server: square corners and the accent fill stay on. */
  active?: boolean;
  size?: 40 | 48;
  className?: string;
}

/**
 * A server's picture, or its acronym. Round at rest, a rounded square on
 * hover, focus and when active: the rail's one piece of motion. The hover and
 * focus states follow the nearest `group` (the rail item that holds it).
 * Decorative: the rail link around it carries the server name.
 */
export function GuildIcon({ name, src, active = false, size = 48, className }: GuildIconProps) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const showImage = Boolean(src) && failedSrc !== src;
  const acronym = guildAcronym(name);
  return (
    <span
      aria-hidden
      className={cn(
        "flex shrink-0 items-center justify-center overflow-hidden font-semibold select-none",
        "transition-[border-radius,background-color,color] duration-(--duration-base) ease-(--ease-out)",
        active ? "rounded-xl" : "rounded-[50%] group-hover:rounded-xl group-focus-visible:rounded-xl",
        !showImage &&
          (active
            ? "bg-accent text-on-accent"
            : "bg-guild text-on-guild group-hover:bg-accent group-hover:text-on-accent group-focus-visible:bg-accent group-focus-visible:text-on-accent"),
        acronym.length > 2 ? "text-sm" : "text-base",
        className,
      )}
      style={{ width: size, height: size }}
    >
      {showImage && src ? (
        <img
          src={src}
          alt=""
          className="size-full object-cover"
          onError={() => {
            setFailedSrc(src);
          }}
        />
      ) : (
        acronym
      )}
    </span>
  );
}
