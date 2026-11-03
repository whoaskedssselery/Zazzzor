import { Hash, Volume2 } from "lucide-react";
import { CHANNEL_TYPE } from "@/entities/channel";
import { type Channel, useGuilds } from "@/entities/guild";
import { Skeleton } from "@/shared/ui";
import { MembersButton, NavButton, ShellHeader } from "@/widgets/app-shell";
import { ChannelSidebar } from "@/widgets/channel-sidebar";
import { ChatView } from "@/widgets/chat";
import { MemberList } from "@/widgets/member-list";
import { VoiceStage } from "@/widgets/voice-stage";
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

/** The shell's channel list for server `guildId`: the real list once the server is known. */
export const ServerSidebar = ({ guildId, channelId }: { guildId: string; channelId: string | null }) => {
  const known = useGuilds((s) => guildId in s.guilds);
  return known ? <ChannelSidebar guildId={guildId} channelId={channelId} /> : <SidebarSkeleton />;
};

/** The shell's member list for server `guildId`, or nothing while the server is not known. */
export const ServerMembers = ({ guildId }: { guildId: string }) => {
  const known = useGuilds((s) => guildId in s.guilds);
  return known ? <MemberList guildId={guildId} /> : null;
};

/** The open text channel: its header (name, topic, the member toggle) and its chat. */
const ChannelView = ({ guildId, channel }: { guildId: string; channel: Channel }) => (
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
    <ChatView key={channel.id} guildId={guildId} channel={channel} />
  </>
);

/** The open voice channel: its header and its stage. */
const VoiceChannelView = ({ guildId, channel }: { guildId: string; channel: Channel }) => (
  <>
    <ShellHeader>
      <NavButton />
      <Volume2 aria-hidden className="size-6 shrink-0 text-fg-faint" strokeWidth={1.75} />
      <h1 className="min-w-0 truncate text-base font-semibold">{channel.name}</h1>
      <div className="ml-auto flex shrink-0 items-center">
        <MembersButton />
      </div>
    </ShellHeader>
    <VoiceStage guildId={guildId} channel={channel} />
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
 * The main area of /channels/$guildId/$channelId and /channels/$guildId,
 * inside the app shell (the app's layout route renders the shell once, with
 * ServerSidebar and ServerMembers). useChannelAccess keeps the address on
 * something the user may open.
 */
export const ChannelPage = ({ guildId, channelId = null }: { guildId: string; channelId?: string | null }) => {
  const openable = useChannelAccess(guildId, channelId);
  const known = useGuilds((s) => guildId in s.guilds);
  const channel = useGuilds((s) => (channelId === null ? undefined : s.guilds[guildId]?.channels[channelId]));
  if (openable && channel !== undefined) {
    return channel.type === CHANNEL_TYPE.VOICE ? (
      <VoiceChannelView guildId={guildId} channel={channel} />
    ) : (
      <ChannelView guildId={guildId} channel={channel} />
    );
  }
  return (
    <EmptyView text={known && channelId === null ? "На этом сервере нет текстовых каналов, доступных вам." : null} />
  );
};
