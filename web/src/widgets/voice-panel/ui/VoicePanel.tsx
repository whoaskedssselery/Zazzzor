import { Link } from "@tanstack/react-router";
import { PhoneOff } from "lucide-react";
import { useCall } from "@/entities/call";
import { privateName, useGuilds } from "@/entities/guild";
import { CameraButton, leaveVoice, ScreenShareButton } from "@/features/voice-call";
import { cn, IconButton } from "@/shared/ui";

const STATUS = {
  connecting: "Подключение…",
  connected: "Голос подключён",
  reconnecting: "Переподключение…",
} as const;

/**
 * The call above the user panel, as in Discord: how the link is, which
 * channel of which server (a link back to it), and the way out.
 */
export const VoicePanel = () => {
  const status = useCall((s) => s.status);
  const guildId = useCall((s) => s.guildId);
  const channelId = useCall((s) => s.channelId);
  const guild = useGuilds((s) => (guildId === null ? undefined : s.guilds[guildId]?.guild));
  const channel = useGuilds((s) =>
    guildId === null || channelId === null ? undefined : s.guilds[guildId]?.channels[channelId],
  );
  const dm = useGuilds((s) => (guildId === null && channelId !== null ? s.privates[channelId]?.channel : undefined));
  if (status === "idle" || channelId === null) return null;
  return (
    <section
      aria-label="Голосовая связь"
      className="flex shrink-0 items-center gap-1 border-b border-border bg-panel px-2 py-1.5"
    >
      <div className="min-w-0 flex-1 pl-1">
        <p className={cn("text-sm font-semibold", status === "connected" ? "text-fg-success" : "text-fg-warning")}>
          {STATUS[status]}
        </p>
        {guildId === null ? (
          <Link
            to="/channels/@me/$channelId"
            params={{ channelId }}
            className="block truncate text-xs text-fg-muted hover:text-fg hover:underline"
          >
            {dm ? `Звонок: ${privateName(dm)}` : "Звонок"}
          </Link>
        ) : (
          <Link
            to="/channels/$guildId/$channelId"
            params={{ guildId, channelId }}
            className="block truncate text-xs text-fg-muted hover:text-fg hover:underline"
          >
            {channel?.name ?? "Голосовой канал"} / {guild?.name ?? "Сервер"}
          </Link>
        )}
      </div>
      <CameraButton />
      <ScreenShareButton />
      <IconButton
        label="Отключиться"
        icon={PhoneOff}
        onClick={() => {
          void leaveVoice();
        }}
      />
    </section>
  );
};
