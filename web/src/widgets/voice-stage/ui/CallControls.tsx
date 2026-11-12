import { HeadphoneOff, Headphones, Maximize, Mic, MicOff, Minimize, PhoneOff } from "lucide-react";
import { CameraButton, leaveVoice, ScreenShareButton, useSelfSwitches } from "@/features/voice-call";
import { cn, IconButton } from "@/shared/ui";

const pill =
  "size-12 rounded-xl bg-control text-fg transition-colors duration-(--duration-fast) hover:bg-control-hover [&>svg]:size-5.5 [@media(max-height:560px)]:size-10 [@media(max-height:560px)]:[&>svg]:size-5";

/**
 * The call's own controls under the stage, as in Discord: the microphone,
 * the sound, the camera, the screen share, and the way out in red; at the right, the
 * stage over the whole screen and back.
 */
export const CallControls = ({ fullscreen, onFullscreen }: { fullscreen: boolean; onFullscreen: () => void }) => {
  const { mic, sound } = useSelfSwitches();
  return (
    <div
      role="toolbar"
      aria-label="Управление звонком"
      className="relative flex shrink-0 items-center justify-center gap-2 px-4 pt-2 pb-4 [@media(max-height:560px)]:pb-2"
    >
      <IconButton
        label={mic.label}
        icon={mic.off ? MicOff : Mic}
        aria-pressed={mic.off}
        disabled={mic.locked}
        className={cn(pill, mic.off && "bg-danger/15 text-fg-danger hover:bg-danger/25")}
        onClick={mic.toggle}
      />
      <IconButton
        label={sound.label}
        icon={sound.off ? HeadphoneOff : Headphones}
        aria-pressed={sound.off}
        disabled={sound.locked}
        className={cn(pill, sound.off && "bg-danger/15 text-fg-danger hover:bg-danger/25")}
        onClick={sound.toggle}
      />
      <CameraButton className={pill} />
      <ScreenShareButton className={pill} />
      <IconButton
        label="Отключиться"
        icon={PhoneOff}
        className={cn(
          pill,
          "w-16 [@media(max-height:560px)]:w-14 bg-danger text-on-danger hover:bg-danger-hover hover:text-on-danger",
        )}
        onClick={() => {
          void leaveVoice();
        }}
      />
      <IconButton
        label={fullscreen ? "Выйти из полноэкранного режима" : "Во весь экран"}
        icon={fullscreen ? Minimize : Maximize}
        className="absolute right-4 text-fg-muted"
        onClick={onFullscreen}
      />
    </div>
  );
};
