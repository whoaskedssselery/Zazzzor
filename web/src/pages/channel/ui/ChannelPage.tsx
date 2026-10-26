import { Hash } from "lucide-react";
import { type Channel, useGuilds } from "@/entities/guild";
import { Skeleton } from "@/shared/ui";
import { AppShell, MembersButton, NavButton, ShellHeader } from "@/widgets/app-shell";
import { ChannelSidebar } from "@/widgets/channel-sidebar";
import { GuildRail } from "@/widgets/guild-rail";
import { MemberList } from "@/widgets/member-list";
import { UserPanel } from "@/widgets/user-panel";
import { useChannelAccess } from "../model/useChannelAccess";

/** The channel list while the server is not known yet (before READY). */
const SidebarSkeleton = () => (
  <div aria-hidden className="flex flex-col gap-3 px-4 pt-4">
    <Skeleton className="h-5 w-32 rounded-sm" />
    {[0, 1, 2, 3].map((i) => (
      <Skeleton key={i} className="ml-2 h-4 w-36 rounded-sm" />
    ))}
  </div>
);

/**
 * The open text channel: its header (name and topic) and, until the chat
 * arrives (Task 6), the start of the channel where the messages will be.
 */
const ChannelView = ({ channel }: { channel: Channel }) => (
  <>
    <ShellHeader>
      <NavButton />
      <Hash aria-hidden className="size-6 shrink-0 text-fg-faint" strokeWidth={1.75} />
      <h1 className="min-w-0 shrink-0 truncate text-base font-semibold max-sm:shrink">{channel.name}</h1>
      {channel.topic && (
        <>
          <span aria-hidden className="mx-2 h-6 w-px shrink-0 bg-border max-sm:hidden" />
          <p className="min-w-0 flex-1 truncate text-sm text-fg-muted max-sm:hidden" title={channel.topic}>
            {channel.topic}
          </p>
        </>
      )}
      <div className="ml-auto flex shrink-0 items-center">
        <MembersButton />
      </div>
    </ShellHeader>
    <div className="flex min-h-0 flex-1 flex-col justify-end overflow-y-auto">
      <div className="mx-4 mt-6 mb-6">
        <span aria-hidden className="flex size-17 items-center justify-center rounded-full bg-control">
          <Hash className="size-10 text-fg" strokeWidth={1.75} />
        </span>
        <h2 className="mt-3 text-xl font-bold break-words">{`Добро пожаловать в #${channel.name}!`}</h2>
        <p className="mt-1 text-base text-fg-muted">{`Это начало канала #${channel.name}.`}</p>
      </div>
    </div>
  </>
);

/** The main area while the address is being checked, or of a server with no text channel to open. */
const EmptyView = ({ text }: { text: string | null }) => (
  <>
    <ShellHeader>
      <NavButton />
    </ShellHeader>
    <div className="flex min-h-0 flex-1 items-center justify-center p-6 text-center">
      {text && <p className="max-w-sm text-base text-fg-muted">{text}</p>}
    </div>
  </>
);

/**
 * /channels/$guildId/$channelId and /channels/$guildId: the server's
 * channels, the open channel and its members. useChannelAccess keeps the
 * address on something the user may open.
 */
export const ChannelPage = ({ guildId, channelId = null }: { guildId: string; channelId?: string | null }) => {
  const openable = useChannelAccess(guildId, channelId);
  const known = useGuilds((s) => guildId in s.guilds);
  const channel = useGuilds((s) => (channelId === null ? undefined : s.guilds[guildId]?.channels[channelId]));
  const showChannel = openable && channel !== undefined;
  return (
    <AppShell
      rail={<GuildRail activeGuildId={guildId} />}
      sidebar={known ? <ChannelSidebar guildId={guildId} channelId={channelId} /> : <SidebarSkeleton />}
      userPanel={<UserPanel />}
      {...(showChannel ? { members: <MemberList guildId={guildId} /> } : {})}
    >
      {showChannel ? (
        <ChannelView channel={channel} />
      ) : (
        <EmptyView
          text={known && channelId === null ? "На этом сервере нет текстовых каналов, доступных вам." : null}
        />
      )}
    </AppShell>
  );
};
