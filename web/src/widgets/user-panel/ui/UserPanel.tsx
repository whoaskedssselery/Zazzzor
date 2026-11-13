import { useNavigate } from "@tanstack/react-router";
import { HeadphoneOff, Headphones, Mic, MicOff, Settings } from "lucide-react";
import type { CSSProperties } from "react";
import { PRESENCE_LABEL, useConnectionStatus } from "@/entities/guild";
import { useSession } from "@/entities/session";
import { LogoutButton } from "@/features/auth";
import { StatusMenu } from "@/features/edit-profile";
import { useSelfSwitches } from "@/features/voice-call";
import { Avatar, cn, IconButton } from "@/shared/ui";

export { MODERATOR_DEAFENED, MODERATOR_MUTED } from "@/features/voice-call";

/**
 * The signed-in user at the foot of the channel list: avatar with presence,
 * name, status, then the microphone and sound switches, sign-out and the user
 * settings. The presence shown is the status the user chose while the
 * gateway is connected, offline otherwise; a click on it opens the status
 * menu. Their status line, if any, replaces the status text. The switches work outside a call
 * too, as in Discord; a switch a moderator turned off is locked.
 */
export const UserPanel = () => {
  const { user } = useSession();
  const connected = useConnectionStatus() === "ready";
  const navigate = useNavigate();
  const { mic, sound } = useSelfSwitches();
  if (!user) return null;
  const shown = !connected || user.status === "invisible" ? "offline" : user.status;
  const statusText = connected && user.status === "invisible" ? "Невидимка" : PRESENCE_LABEL[shown];
  return (
    <section
      aria-label="Ваш профиль"
      className="flex h-(--user-panel-height) shrink-0 items-center gap-1 bg-panel px-2"
      style={{ "--avatar-cutout": "var(--bg-panel)" } as CSSProperties}
    >
      <StatusMenu user={user}>
        <button
          type="button"
          aria-label={`Статус: ${statusText}`}
          className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 rounded-md py-1 pr-1 pl-0.5 text-left transition-colors duration-(--duration-fast) hover:bg-hover data-[state=open]:bg-selected"
        >
          <Avatar name={user.display_name} src={user.avatar_url} size={32} status={shown} />
          <span className="min-w-0">
            <span data-user-name title={user.display_name} className="block truncate text-sm leading-4 font-semibold">
              {user.display_name}
            </span>
            <span
              data-user-status
              title={connected && user.custom_status !== null ? user.custom_status : statusText}
              className="block truncate text-xs text-fg-faint"
            >
              {connected && user.custom_status !== null ? user.custom_status : statusText}
            </span>
          </span>
        </button>
      </StatusMenu>
      <IconButton
        label={mic.label}
        icon={mic.off ? MicOff : Mic}
        aria-pressed={mic.off}
        disabled={mic.locked}
        className={cn(mic.off && "text-fg-danger")}
        onClick={mic.toggle}
      />
      <IconButton
        label={sound.label}
        icon={sound.off ? HeadphoneOff : Headphones}
        aria-pressed={sound.off}
        disabled={sound.locked}
        className={cn(sound.off && "text-fg-danger")}
        onClick={sound.toggle}
      />
      <LogoutButton iconOnly />
      <IconButton
        label="Настройки пользователя"
        icon={Settings}
        onClick={() => {
          void navigate({ href: "/settings/profile" });
        }}
      />
    </section>
  );
};
