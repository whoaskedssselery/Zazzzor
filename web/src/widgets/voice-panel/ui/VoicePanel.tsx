import { Link } from "@tanstack/react-router";
import { ChevronDown, PhoneOff } from "lucide-react";
import { useCall } from "@/entities/call";
import { privateName, useGuilds } from "@/entities/guild";
import { CameraButton, leaveVoice, ScreenShareButton } from "@/features/voice-call";
import { t, textTable } from "@/shared/i18n";
import { cn, IconButton, Popover, PopoverContent, PopoverTrigger } from "@/shared/ui";
import { LinkDetails } from "./LinkDetails";

const STATUS = textTable({
  connecting: () => t("common:connecting"),
  connected: () => t("shell:voiceConnected"),
  reconnecting: () => t("common:reconnecting"),
});

/**
 * The call above the user panel, as in Discord: how the link is (a click
 * on it shows the figures), which channel of which server (a link back to
 * it), and the way out.
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
      aria-label={t("shell:voiceConnection")}
      className="flex shrink-0 items-center gap-1 border-b border-border bg-panel px-2 py-1.5"
    >
      <div className="min-w-0 flex-1 pl-1">
        <Popover>
          <PopoverTrigger
            aria-label={`${STATUS[status]}. ${t("shell:link.details")}`}
            className={cn(
              "group/link -ml-1 flex cursor-pointer items-center gap-0.5 rounded-sm px-1 text-sm font-semibold hover:underline focus-visible:outline-2 focus-visible:outline-focus",
              status === "connected" ? "text-fg-success" : "text-fg-warning",
            )}
          >
            {STATUS[status]}
            <ChevronDown
              aria-hidden
              className="size-3.5 transition-transform duration-(--duration-fast) group-data-[state=open]/link:rotate-180"
            />
          </PopoverTrigger>
          <PopoverContent side="top" align="start">
            <LinkDetails />
          </PopoverContent>
        </Popover>
        {guildId === null ? (
          <Link
            to="/channels/@me/$channelId"
            params={{ channelId }}
            className="block truncate text-xs text-fg-muted hover:text-fg hover:underline"
          >
            {dm ? t("shell:callWith", { name: privateName(dm) }) : t("shell:call")}
          </Link>
        ) : (
          <Link
            to="/channels/$guildId/$channelId"
            params={{ guildId, channelId }}
            className="block truncate text-xs text-fg-muted hover:text-fg hover:underline"
          >
            {channel?.name ?? t("shell:voiceChannelFallback")} / {guild?.name ?? t("shell:serverFallback")}
          </Link>
        )}
      </div>
      <CameraButton />
      <ScreenShareButton />
      <IconButton
        label={t("shell:disconnect")}
        icon={PhoneOff}
        onClick={() => {
          void leaveVoice();
        }}
      />
    </section>
  );
};
