import { Link } from "@tanstack/react-router";
import { ChevronDown, ChevronRight, Hash, LogOut, UserPlus, Volume2 } from "lucide-react";
import { useState } from "react";
import { CHANNEL_TYPE, type ChannelGroup, useChannelUi, useVisibleChannels } from "@/entities/channel";
import { type Channel, computeChannel, type GuildEntry, has, PERM, useGuilds } from "@/entities/guild";
import { InviteDialog } from "@/features/create-invite";
import { LeaveGuildDialog } from "@/features/leave-guild";
import {
  cn,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  useToast,
} from "@/shared/ui";

const VOICE_LATER = "Голосовые каналы появятся в следующем обновлении";

/**
 * The channel to invite into: the open one when the user may invite there,
 * else the first text channel of the list where they may.
 */
const inviteChannel = (
  entry: GuildEntry,
  userId: string,
  groups: ChannelGroup[],
  openId: string | null,
): Channel | undefined => {
  const canInvite = (c: Channel) =>
    c.type === CHANNEL_TYPE.TEXT && has(computeChannel(entry, c.id, userId), PERM.CREATE_INVITE);
  const open = openId === null ? undefined : entry.channels[openId];
  if (open && canInvite(open)) return open;
  return groups.flatMap((g) => g.channels).find(canInvite);
};

const rowClass =
  "flex h-8 w-full min-w-0 items-center gap-1.5 rounded-md px-2 text-left transition-colors duration-(--duration-fast)";

const ChannelRow = ({ channel, selected }: { channel: Channel; selected: boolean }) => {
  const toast = useToast();
  if (channel.type === CHANNEL_TYPE.VOICE) {
    return (
      <li className="px-2">
        <button
          type="button"
          // The icon is decorative, so the name says what kind of channel this is.
          aria-label={`Голосовой канал ${channel.name}`}
          className={cn(rowClass, "cursor-pointer text-fg-faint hover:bg-hover hover:text-fg-muted")}
          onClick={() => {
            toast.show({ text: VOICE_LATER });
          }}
        >
          <Volume2 aria-hidden className="size-5 shrink-0 text-fg-faint" strokeWidth={1.75} />
          <span className="truncate text-base font-medium">{channel.name}</span>
        </button>
      </li>
    );
  }
  return (
    <li className="px-2">
      <Link
        to="/channels/$guildId/$channelId"
        params={{ guildId: channel.guild_id, channelId: channel.id }}
        aria-current={selected ? "page" : undefined}
        className={cn(rowClass, selected ? "bg-selected text-fg" : "text-fg-faint hover:bg-hover hover:text-fg-muted")}
      >
        <Hash aria-hidden className="size-5 shrink-0 text-fg-faint" strokeWidth={1.75} />
        <span className="truncate text-base font-medium">{channel.name}</span>
      </Link>
    </li>
  );
};

const Group = ({ group, openId }: { group: ChannelGroup; openId: string | null }) => {
  const category = group.category;
  const collapsed = useChannelUi((s) => (category ? s.collapsed[category.id] === true : false));
  const toggle = useChannelUi((s) => s.toggleCategory);
  // A collapsed category still shows the open channel, as Discord does.
  const shown = collapsed ? group.channels.filter((c) => c.id === openId) : group.channels;
  const list = shown.length > 0 && (
    <ul className="flex flex-col gap-px">
      {shown.map((c) => (
        <ChannelRow key={c.id} channel={c} selected={c.id === openId} />
      ))}
    </ul>
  );
  if (!category) return <div className="mt-1">{list}</div>;
  const Chevron = collapsed ? ChevronRight : ChevronDown;
  return (
    <section className="mt-4 first:mt-1">
      <h3>
        <button
          type="button"
          aria-expanded={!collapsed}
          onClick={() => {
            toggle(category.id);
          }}
          className="flex h-7 w-full cursor-pointer items-center gap-0.5 pr-2 pl-1.5 text-sm font-semibold text-fg-faint transition-colors hover:text-fg"
        >
          <Chevron aria-hidden className="size-3.5 shrink-0" strokeWidth={2.5} />
          <span className="truncate">{category.name}</span>
        </button>
      </h3>
      {list}
    </section>
  );
};

/**
 * The channel list of a server: its name with the server menu (invite,
 * leave), then the categories and channels the user may view. Categories
 * collapse and stay so across visits. Text channels are links; a voice
 * channel only says that voice comes in the next update.
 */
export const ChannelSidebar = ({ guildId, channelId }: { guildId: string; channelId: string | null }) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const userId = useGuilds((s) => s.me?.id);
  const { categories } = useVisibleChannels(guildId);
  const [dialog, setDialog] = useState<"invite" | "leave" | null>(null);
  if (!entry || !userId) return null;

  const { guild } = entry;
  const invite = inviteChannel(entry, userId, categories, channelId);
  const isOwner = guild.owner_id === userId;
  const close = (open: boolean) => {
    if (!open) setDialog(null);
  };

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          aria-label={`Меню сервера ${guild.name}`}
          className="flex h-(--header-height) shrink-0 cursor-pointer items-center justify-between gap-2 px-4 text-base font-semibold shadow-[0_1px_0_var(--border)] transition-colors hover:bg-hover data-[state=open]:bg-hover"
        >
          <span className="truncate">{guild.name}</span>
          <ChevronDown aria-hidden className="size-5 shrink-0 text-fg-muted" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-(--radix-dropdown-menu-trigger-width) min-w-52">
          {invite && (
            <DropdownMenuItem
              onSelect={() => {
                setDialog("invite");
              }}
            >
              Пригласить людей
              <UserPlus aria-hidden />
            </DropdownMenuItem>
          )}
          {invite && !isOwner && <DropdownMenuSeparator />}
          {!isOwner && (
            <DropdownMenuItem
              variant="danger"
              onSelect={() => {
                setDialog("leave");
              }}
            >
              Покинуть сервер
              <LogOut aria-hidden />
            </DropdownMenuItem>
          )}
          {!invite && isOwner && <DropdownMenuItem disabled>Нет каналов для приглашения</DropdownMenuItem>}
        </DropdownMenuContent>
      </DropdownMenu>
      <nav aria-label="Каналы" className="min-h-0 flex-1 overflow-y-auto pt-2 pb-3">
        {categories.map((g) => (
          <Group key={g.category?.id ?? "top"} group={g} openId={channelId} />
        ))}
      </nav>
      {invite && (
        <InviteDialog open={dialog === "invite"} onOpenChange={close} channelId={invite.id} channelName={invite.name} />
      )}
      {!isOwner && (
        <LeaveGuildDialog open={dialog === "leave"} onOpenChange={close} guildId={guildId} guildName={guild.name} />
      )}
    </>
  );
};
