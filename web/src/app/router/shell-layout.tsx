import { Outlet, useParams } from "@tanstack/react-router";
import { CHANNEL_TYPE } from "@/entities/channel";
import { PRIVATE_TYPE, useGuilds } from "@/entities/guild";
import { ServerMembers, ServerSidebar } from "@/pages/channel";
import { HomeSidebar } from "@/pages/channels-home";
import { AppShell } from "@/widgets/app-shell";
import { GuildRail } from "@/widgets/guild-rail";
import { PrivateMembers } from "@/widgets/private-list";
import { UserPanel } from "@/widgets/user-panel";
import { VoicePanel } from "@/widgets/voice-panel";

/**
 * The layout of home and the server screens: the app shell, rendered once
 * for all of them. Switching servers or channels only replaces the main
 * area (the child route); the rail, the channel list, the user panel and the
 * member toggle stay mounted, so nothing flashes and no state resets.
 */
export const ShellLayout = () => {
  const { guildId, channelId } = useParams({ strict: false });
  // No member list (and no toggle for it) until the server is in the store.
  const known = useGuilds((s) => guildId !== undefined && guildId in s.guilds);
  // A voice channel has its own people on the stage: no member list there.
  const voice = useGuilds(
    (s) =>
      guildId !== undefined &&
      channelId !== undefined &&
      s.guilds[guildId]?.channels[channelId]?.type === CHANNEL_TYPE.VOICE,
  );
  // A group shows its people in the member column; a DM has none.
  const group = useGuilds(
    (s) =>
      guildId === undefined && channelId !== undefined && s.privates[channelId]?.channel.type === PRIVATE_TYPE.GROUP,
  );
  let members = {};
  if (group && channelId !== undefined) members = { members: <PrivateMembers channelId={channelId} /> };
  else if (guildId !== undefined && known && !voice) members = { members: <ServerMembers guildId={guildId} /> };
  return (
    <AppShell
      rail={<GuildRail activeGuildId={guildId ?? null} />}
      sidebar={
        guildId === undefined ? <HomeSidebar /> : <ServerSidebar guildId={guildId} channelId={channelId ?? null} />
      }
      userPanel={
        <>
          <VoicePanel />
          <UserPanel />
        </>
      }
      {...members}
    >
      <Outlet />
    </AppShell>
  );
};
