import { Volume2, VolumeX } from "lucide-react";
import { type KeyboardEvent, useId, useRef } from "react";
import { cn } from "@/shared/ui";
import { setVolume, useVolume, VOLUME_DEFAULT, VOLUME_MAX, type VolumeKind } from "../model/volumes";

/** Within this many percent of 100 a dragged slider lands on 100, as Discord's does. */
const SNAP = 4;

/** `value` as the slider takes it from a drag: 100 when close to it. */
export const snapVolume = (value: number): number =>
  Math.abs(value - VOLUME_DEFAULT) <= SNAP ? VOLUME_DEFAULT : value;

/** Inside a menu the arrow keys are the slider's, not the menu's; Escape and Tab stay the menu's. */
const keepKeys = (e: KeyboardEvent) => {
  if (e.key !== "Escape" && e.key !== "Tab") e.stopPropagation();
};

/**
 * How loud the user hears `userId`: their voice or their screen share, from
 * 0 to 200 %, kept on this device. A double click returns it to 100 %.
 * `inline` is the short form for a bar of buttons: an icon, the slider and
 * the percent on one line, named by `label` for screen readers.
 */
export const VolumeSlider = ({
  kind,
  userId,
  label,
  inline = false,
  className,
}: {
  kind: VolumeKind;
  userId: string;
  /** «Громкость пользователя», «Громкость трансляции». */
  label: string;
  inline?: boolean;
  className?: string;
}) => {
  const value = useVolume(kind, userId);
  const id = useId();
  // Only a drag snaps to 100: an arrow key stepping off 100 must not be pulled back.
  const dragging = useRef(false);
  const Icon = value === 0 ? VolumeX : Volume2;
  const slider = (
    <input
      id={id}
      type="range"
      min={0}
      max={VOLUME_MAX}
      step={1}
      value={value}
      aria-label={inline ? label : undefined}
      aria-valuetext={`${String(value)} %`}
      className={cn("h-4 min-w-0 cursor-pointer accent-accent", inline ? "w-24" : "w-full")}
      onKeyDown={keepKeys}
      onPointerDown={() => {
        dragging.current = true;
      }}
      onPointerUp={() => {
        dragging.current = false;
      }}
      onBlur={() => {
        dragging.current = false;
      }}
      onChange={(e) => {
        const next = Number(e.target.value);
        setVolume(kind, userId, dragging.current ? snapVolume(next) : next);
      }}
      onDoubleClick={() => {
        setVolume(kind, userId, VOLUME_DEFAULT);
      }}
    />
  );
  if (inline) {
    return (
      <div className={cn("flex h-8 items-center gap-2 rounded-md bg-scrim px-2 text-[#fff]", className)}>
        <Icon aria-hidden className="size-4 shrink-0" />
        {slider}
        <output htmlFor={id} className="w-9 shrink-0 text-right text-xs font-medium tabular-nums">
          {value}%
        </output>
      </div>
    );
  }
  return (
    <div className={cn("flex flex-col gap-1 px-2 py-1.5", className)}>
      <div className="flex items-center justify-between gap-3 text-xs font-semibold text-fg-faint">
        <label htmlFor={id}>{label}</label>
        <output htmlFor={id} className="tabular-nums text-fg-muted">
          {value}%
        </output>
      </div>
      {slider}
    </div>
  );
};
