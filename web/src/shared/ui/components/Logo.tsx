import { type CSSProperties, useLayoutEffect, useRef } from "react";
import { cn } from "../cn";

/**
 * The three letters of the mark, largest first: a stroke and the offset of
 * its darker extrusion. index.html (the boot splash), favicon.svg and the
 * desktop setup page draw the same paths; change them together.
 */
const LETTERS = [
  { d: "M6 24H25L6 42H25", width: 6.5, dx: 1.6, dy: 2 },
  { d: "M29 14H38L29 23H38", width: 4, dx: 1.1, dy: 1.4 },
  { d: "M37 4H43L37 10H43", width: 2.8, dx: 0.8, dy: 1 },
] as const;

interface LogoProps {
  size?: number;
  /**
   * The loading animation: the letters rise one by one off their shadows,
   * then settle back. Still under prefers-reduced-motion.
   */
  animated?: boolean;
  /** Spoken name; omit when the mark sits next to text that already says it. */
  label?: string;
  className?: string;
}

/**
 * The Zazzzor mark: «zzz», three letters climbing to the upper right, each
 * standing on a darker extrusion. Colours come from --logo-face and
 * --logo-shadow, so a parent can recolour it (the rail's home tile does on
 * the accent fill). Every animated logo on the page runs on the clock of
 * index.html's boot splash (or of the page, once that is gone), so swapping
 * one for another shows no jump.
 */
export const Logo = ({ size = 32, animated = false, label, className }: LogoProps) => {
  const ref = useRef<SVGSVGElement>(null);
  useLayoutEffect(() => {
    const svg = ref.current;
    if (!animated || !svg?.getAnimations) return;
    const boot = document.getElementById("boot-splash")?.getAnimations({ subtree: true })[0];
    const start = boot?.startTime ?? 0;
    for (const a of svg.getAnimations({ subtree: true })) a.startTime = start;
  }, [animated]);
  return (
    // biome-ignore lint/a11y/noSvgWithoutTitle: the spread below gives it role="img" with aria-label, or aria-hidden.
    <svg
      ref={ref}
      viewBox="0 0 48 48"
      width={size}
      height={size}
      fill="none"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={cn("shrink-0", animated && "logo-animated", className)}
      {...(label ? { role: "img", "aria-label": label } : { "aria-hidden": true })}
    >
      {LETTERS.map(({ d, width, dx, dy }, i) => (
        <g
          key={d}
          className="logo-letter"
          style={{ "--logo-i": i, "--logo-dx": `${String(dx)}px`, "--logo-dy": `${String(dy)}px` } as CSSProperties}
        >
          <path
            d={d}
            strokeWidth={width}
            stroke="var(--logo-shadow)"
            transform={`translate(${String(dx)} ${String(dy)})`}
          />
          <path d={d} strokeWidth={width} stroke="var(--logo-face)" className="logo-face" />
        </g>
      ))}
    </svg>
  );
};

/** Golos Text's cap height, in em (measured: 70 px at 100 px). */
const CAP_HEIGHT = 0.7;
/** The large Z's face, in the mark's 48 units: 24.5 tall, its bottom 2.75 above the mark's. */
const BIG_Z_HEIGHT = 24.5 / 48;
const BIG_Z_INSET = 2.75 / 48;

/**
 * The mark and the name together, lined up on the large first Z: the capitals
 * are as tall as the Z, and the name's baseline is the Z's bottom stroke. The
 * small z's rise above the line. `fontSize` in px sets the scale.
 */
export const LogoLockup = ({ fontSize = 24, className }: { fontSize?: number; className?: string }) => {
  const size = Math.round((fontSize * CAP_HEIGHT) / BIG_Z_HEIGHT);
  return (
    <span
      translate="no"
      className={cn("inline-flex items-baseline font-bold tracking-tight text-fg", className)}
      style={{ fontSize, lineHeight: 1, gap: Math.round(fontSize * 0.3) }}
    >
      {/* The svg's baseline is its bottom edge: move it down so the Z's bottom stroke sits on the line. */}
      <span className="relative self-baseline" style={{ top: size * BIG_Z_INSET }}>
        <Logo size={size} className="block" />
      </span>
      Zazzzor
    </span>
  );
};
