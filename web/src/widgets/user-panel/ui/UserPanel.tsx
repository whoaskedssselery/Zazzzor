import { useNavigate } from "@tanstack/react-router";
import { HeadphoneOff, Headphones, Mic, MicOff, Settings } from "lucide-react";
import type { CSSProperties } from "react";
import { useCall } from "@/entities/call";
import { myVoice, useConnectionStatus, useGuilds } from "@/entities/guild";
import { useSession } from "@/entities/session";
import { LogoutButton } from "@/features/auth";
import { setSelfDeaf, setSelfMute } from "@/features/voice-call";
import { Avatar, cn, IconButton } from "@/shared/ui";

export const MODERATOR_MUTED = "Модератор выключил вам микрофон";
export const MODERATOR_DEAFENED = "Модератор выключил вам звук";

/**
 * The signed-in user at the foot of the channel list: avatar with presence,
 * name, status, then the microphone and sound switches, sign-out and the user
 * settings. The user's own presence is the gateway's: online while connected
 * (the server tells others, not the user). The switches work outside a call
 * too, as in Discord; a switch a moderator turned off is locked.
 */
export const UserPanel = () => {
  const { user } = useSession();
  const online = useConnectionStatus() === "ready";
  const navigate = useNavigate();
  const selfMute = useCall((s) => s.selfMute);
  const selfDeaf = useCall((s) => s.selfDeaf);
  const voice = useGuilds(myVoice);
  if (!user) return null;
  const modMute = voice?.mute ?? false;
  const modDeaf = voice?.deaf ?? false;
  const micOff = selfMute || selfDeaf || modMute;
  const soundOff = selfDeaf || modDeaf;
  return (
    <section
      aria-label="Ваш профиль"
      className="flex h-(--user-panel-height) shrink-0 items-center gap-1 bg-panel px-2"
      style={{ "--avatar-cutout": "var(--bg-panel)" } as CSSProperties}
    >
      <div className="flex min-w-0 flex-1 items-center gap-2 py-1 pr-1 pl-0.5">
        <Avatar name={user.display_name} src={user.avatar_url} size={32} status={online ? "online" : "offline"} />
        <span className="min-w-0">
          <span data-user-name className="block truncate text-sm leading-4 font-semibold">
            {user.display_name}
          </span>
          <span data-user-status className="block truncate text-xs text-fg-faint">
            {online ? "В сети" : "Не в сети"}
          </span>
        </span>
      </div>
      <IconButton
        label={modMute ? MODERATOR_MUTED : micOff ? "Включить микрофон" : "Выключить микрофон"}
        icon={micOff ? MicOff : Mic}
        aria-pressed={micOff}
        disabled={modMute}
        className={cn(micOff && "text-fg-danger")}
        onClick={() => {
          // The microphone while deafened brings the sound back too, as in Discord.
          if (selfDeaf) {
            setSelfDeaf(false);
            setSelfMute(false);
          } else setSelfMute(!selfMute);
        }}
      />
      <IconButton
        label={modDeaf ? MODERATOR_DEAFENED : soundOff ? "Включить звук" : "Выключить звук"}
        icon={soundOff ? HeadphoneOff : Headphones}
        aria-pressed={soundOff}
        disabled={modDeaf}
        className={cn(soundOff && "text-fg-danger")}
        onClick={() => {
          setSelfDeaf(!selfDeaf);
        }}
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
