import { Link, useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useState } from "react";
import { CHANNEL_SETTINGS_TITLES, channelSettingsTabs, useConnectionStatus, useGuilds } from "@/entities/guild";
import { SettingsFrame, settingsNavItemClass, useLeaveGuard } from "@/shared/ui";
import { ChannelOverview } from "./ChannelOverview";
import { ChannelPermissions } from "./ChannelPermissions";

const CATEGORY = 4;

/**
 * A channel's (or category's) settings over the whole window: its overview
 * with MANAGE_CHANNELS, its permissions with MANAGE_ROLES. Leaving unsaved
 * changes asks first. When the channel goes, or the user loses every
 * section, they are taken back to the server.
 */
export const ChannelSettings = ({ guildId, channelId, tab }: { guildId: string; channelId: string; tab: string }) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const meId = useGuilds((s) => s.me?.id ?? null);
  const status = useConnectionStatus();
  const navigate = useNavigate();
  const channel = entry?.channels[channelId];
  const tabs = entry && meId !== null ? channelSettingsTabs(entry, channelId, meId) : [];
  const current = tabs.find((t) => t === tab);
  const [dirty, setDirty] = useState(false);
  const { guard, dialog } = useLeaveGuard(dirty, () => {
    setDirty(false);
  });

  const close = useCallback(() => {
    if (channel && channel.type !== CATEGORY) {
      void navigate({ to: "/channels/$guildId/$channelId", params: { guildId, channelId } });
    } else {
      void navigate({ to: "/channels/$guildId", params: { guildId } });
    }
  }, [navigate, guildId, channelId, channel]);

  const first = tabs[0];
  useEffect(() => {
    if (status !== "ready") return;
    if (first === undefined) void navigate({ to: "/channels/$guildId", params: { guildId }, replace: true });
    else if (current === undefined) {
      void navigate({
        to: "/guilds/$guildId/channels/$channelId/settings/$tab",
        params: { guildId, channelId, tab: first },
        replace: true,
      });
    }
  }, [status, first, current, guildId, channelId, navigate]);

  const title = channel ? (channel.type === CATEGORY ? channel.name : `#${channel.name}`) : null;
  return (
    <>
      <SettingsFrame
        navLabel="Настройки канала"
        title={title}
        onClose={() => {
          guard(close);
        }}
        nav={tabs.map((t) => (
          <Link
            key={t}
            to="/guilds/$guildId/channels/$channelId/settings/$tab"
            params={{ guildId, channelId, tab: t }}
            aria-current={t === current ? "page" : undefined}
            className={settingsNavItemClass(t === current)}
            onClick={(e) => {
              if (!dirty || t === current) return;
              e.preventDefault();
              guard(() => {
                void navigate({
                  to: "/guilds/$guildId/channels/$channelId/settings/$tab",
                  params: { guildId, channelId, tab: t },
                });
              });
            }}
          >
            {CHANNEL_SETTINGS_TITLES[t]}
          </Link>
        ))}
      >
        {entry && channel && meId !== null && current === "overview" && (
          <ChannelOverview key={channel.id} channel={channel} onDirtyChange={setDirty} />
        )}
        {entry && channel && meId !== null && current === "permissions" && (
          <ChannelPermissions key={channel.id} entry={entry} meId={meId} channel={channel} onDirtyChange={setDirty} />
        )}
      </SettingsFrame>
      {dialog}
    </>
  );
};
