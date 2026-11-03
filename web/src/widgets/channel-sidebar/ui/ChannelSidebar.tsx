import { useMutation } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import {
  ChevronDown,
  ChevronRight,
  FolderPlus,
  Hash,
  LogOut,
  Plus,
  PlusCircle,
  Settings,
  UserPlus,
  Volume2,
} from "lucide-react";
import { createContext, type DragEvent, type KeyboardEvent, type ReactNode, use, useState } from "react";
import {
  CHANNEL_TYPE,
  type ChannelGroup,
  type ChannelMove,
  channelMove,
  stepMove,
  useChannelUi,
  useVisibleChannels,
} from "@/entities/channel";
import {
  type Channel,
  channelSettingsTabs,
  computeBase,
  computeChannel,
  type GuildEntry,
  guildSettingsTabs,
  has,
  PERM,
  useGuilds,
} from "@/entities/guild";
import { CreateChannelDialog, type NewChannelKind } from "@/features/create-channel";
import { InviteDialog } from "@/features/create-invite";
import { LeaveGuildDialog } from "@/features/leave-guild";
import { api, errorText, unwrap } from "@/shared/api";
import {
  cn,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  IconButton,
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

/** What the list lets the user do, shared by every row. */
interface ListActions {
  guildId: string;
  /** MANAGE_CHANNELS in the server: channels move and categories take new ones. */
  canArrange: boolean;
  /** The first settings section of a channel the user may open, if any. */
  settingsTabOf: (channelId: string) => string | undefined;
  /** The channel being dragged. */
  dragging: Channel | null;
  setDragging: (c: Channel | null) => void;
  move: (m: ChannelMove | null) => void;
  createIn: (category: Channel) => void;
}

const ListContext = createContext<ListActions | null>(null);

const useList = (): ListActions => {
  const ctx = use(ListContext);
  if (!ctx) throw new Error("ChannelSidebar rows render inside the sidebar");
  return ctx;
};

const rowClass =
  "flex h-8 w-full min-w-0 items-center gap-1.5 rounded-md px-2 text-left transition-colors duration-(--duration-fast)";

/** The gear beside a channel or category: its settings, shown on hover, focus or when open. */
const SettingsLink = ({ channel, tab, visible }: { channel: Channel; tab: string; visible: boolean }) => (
  <Link
    to="/guilds/$guildId/channels/$channelId/settings/$tab"
    params={{ guildId: channel.guild_id, channelId: channel.id, tab }}
    aria-label={`Настройки ${channel.type === CHANNEL_TYPE.CATEGORY ? "категории" : "канала"} ${channel.name}`}
    className={cn(
      "absolute top-1/2 right-3 flex size-6 -translate-y-1/2 items-center justify-center rounded-sm text-fg-muted hover:text-fg focus-visible:opacity-100 group-hover:opacity-100",
      visible ? "opacity-100" : "opacity-0",
    )}
  >
    <Settings aria-hidden className="size-4" />
  </Link>
);

/** Drag and Alt+↑/↓ for a row the user may arrange; nothing otherwise. */
const useArrange = (channel: Channel, siblings: Channel[]) => {
  const list = useList();
  if (!list.canArrange) return { row: {}, control: {} };
  return {
    row: {
      draggable: true,
      onDragStart: (e: DragEvent) => {
        e.dataTransfer.effectAllowed = "move";
        list.setDragging(channel);
      },
      onDragEnd: () => {
        list.setDragging(null);
      },
      onDragOver: (e: DragEvent) => {
        if (list.dragging && list.dragging.type !== CHANNEL_TYPE.CATEGORY) e.preventDefault();
      },
      onDrop: (e: DragEvent) => {
        e.preventDefault();
        const moved = list.dragging;
        list.setDragging(null);
        if (moved) list.move(channelMove(moved, channel.parent_id, channel));
      },
    },
    control: {
      "aria-keyshortcuts": "Alt+ArrowUp Alt+ArrowDown",
      onKeyDown: (e: KeyboardEvent) => {
        if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
        e.preventDefault();
        list.move(stepMove(channel, siblings, e.key === "ArrowUp"));
      },
    },
  };
};

const ChannelRow = ({ channel, siblings, selected }: { channel: Channel; siblings: Channel[]; selected: boolean }) => {
  const toast = useToast();
  const list = useList();
  const { row, control } = useArrange(channel, siblings);
  const tab = list.settingsTabOf(channel.id);
  const voice = channel.type === CHANNEL_TYPE.VOICE;
  const Icon = voice ? Volume2 : Hash;
  const look = cn(
    rowClass,
    tab && "pr-9",
    selected ? "bg-selected text-fg" : "text-fg-faint hover:bg-hover hover:text-fg-muted",
  );
  const label = (
    <>
      <Icon aria-hidden className="size-5 shrink-0 text-fg-faint" strokeWidth={1.75} />
      <span className="truncate text-base font-medium">{channel.name}</span>
    </>
  );
  return (
    <li className={cn("group relative px-2", list.dragging?.id === channel.id && "opacity-50")} {...row}>
      {voice ? (
        <button
          type="button"
          // The icon is decorative, so the name says what kind of channel this is.
          aria-label={`Голосовой канал ${channel.name}`}
          className={cn(look, "cursor-pointer")}
          onClick={() => {
            toast.show({ text: VOICE_LATER });
          }}
          {...control}
        >
          {label}
        </button>
      ) : (
        <Link
          to="/channels/$guildId/$channelId"
          params={{ guildId: channel.guild_id, channelId: channel.id }}
          aria-current={selected ? "page" : undefined}
          className={look}
          {...control}
        >
          {label}
        </Link>
      )}
      {tab && <SettingsLink channel={channel} tab={tab} visible={selected} />}
    </li>
  );
};

const Group = ({ group, openId }: { group: ChannelGroup; openId: string | null }) => {
  const list = useList();
  const category = group.category;
  const collapsed = useChannelUi((s) => (category ? s.collapsed[category.id] === true : false));
  const toggle = useChannelUi((s) => s.toggleCategory);
  // A collapsed category still shows the open channel, as Discord does.
  const shown = collapsed ? group.channels.filter((c) => c.id === openId) : group.channels;
  const rows = shown.length > 0 && (
    <ul className="flex flex-col gap-px">
      {shown.map((c) => (
        <ChannelRow key={c.id} channel={c} siblings={group.channels} selected={c.id === openId} />
      ))}
    </ul>
  );
  if (!category) return <div className="mt-1">{rows}</div>;
  const Chevron = collapsed ? ChevronRight : ChevronDown;
  const tab = list.settingsTabOf(category.id);
  // A channel dropped on the heading goes to the end of the category.
  const drop = list.canArrange
    ? {
        onDragOver: (e: DragEvent) => {
          if (list.dragging && list.dragging.type !== CHANNEL_TYPE.CATEGORY) e.preventDefault();
        },
        onDrop: (e: DragEvent) => {
          e.preventDefault();
          const moved = list.dragging;
          list.setDragging(null);
          if (moved) list.move(channelMove(moved, category.id, null));
        },
      }
    : {};
  return (
    <section className="mt-4 first:mt-1">
      <h3 className="group relative flex items-center" {...drop}>
        <button
          type="button"
          aria-expanded={!collapsed}
          onClick={() => {
            toggle(category.id);
          }}
          className="flex h-7 min-w-0 flex-1 cursor-pointer items-center gap-0.5 pr-2 pl-1.5 text-sm font-semibold text-fg-faint transition-colors hover:text-fg"
        >
          <Chevron aria-hidden className="size-3.5 shrink-0" strokeWidth={2.5} />
          <span className="truncate">{category.name}</span>
        </button>
        <span className="mr-2 flex shrink-0 items-center opacity-0 group-hover:opacity-100 has-[:focus-visible]:opacity-100">
          {list.canArrange && (
            <IconButton
              size="sm"
              label={`Создать канал в ${category.name}`}
              icon={Plus}
              onClick={() => {
                list.createIn(category);
              }}
            />
          )}
          {tab && (
            <Link
              to="/guilds/$guildId/channels/$channelId/settings/$tab"
              params={{ guildId: list.guildId, channelId: category.id, tab }}
              aria-label={`Настройки категории ${category.name}`}
              className="flex size-7 items-center justify-center rounded-md text-fg-muted hover:text-fg"
            >
              <Settings aria-hidden className="size-4" />
            </Link>
          )}
        </span>
      </h3>
      {rows}
    </section>
  );
};

type Dialog = "invite" | "leave" | NewChannelKind | null;

/**
 * The channel list of a server: its name with the server menu (settings,
 * new channel and category, invite, leave), then the categories and
 * channels the user may view. Categories collapse and stay so across
 * visits. Text channels are links; a voice channel only says that voice
 * comes in the next update.
 *
 * With MANAGE_CHANNELS the user arranges the list: a channel is dragged
 * before another or onto a category's heading (to its end), or moved with
 * Alt+↑/↓ within its group, and a category's «+» makes a channel in it.
 * A channel or category whose settings the user may open shows a gear.
 */
export const ChannelSidebar = ({ guildId, channelId }: { guildId: string; channelId: string | null }) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const userId = useGuilds((s) => s.me?.id);
  const { categories } = useVisibleChannels(guildId);
  const [dialog, setDialog] = useState<Dialog>(null);
  const [parent, setParent] = useState<Channel | null>(null);
  const [dragging, setDragging] = useState<Channel | null>(null);
  const navigate = useNavigate();
  const toast = useToast();
  const reorder = useMutation({
    mutationFn: async (m: ChannelMove) =>
      unwrap(await api.PATCH("/guilds/{guild_id}/channels", { params: { path: { guild_id: guildId } }, body: [m] })),
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
  if (!entry || !userId) return null;

  const { guild } = entry;
  const invite = inviteChannel(entry, userId, categories, channelId);
  const isOwner = guild.owner_id === userId;
  // The first settings section the user may open; none: no settings in the menu.
  const [settingsTab] = guildSettingsTabs(entry, userId);
  const canArrange = has(computeBase(entry, userId), PERM.MANAGE_CHANNELS);
  const close = (open: boolean) => {
    if (!open) setDialog(null);
  };
  const create = (kind: NewChannelKind, inCategory: Channel | null) => {
    setParent(inCategory);
    setDialog(kind);
  };
  const actions: ListActions = {
    guildId,
    canArrange,
    settingsTabOf: (id) => channelSettingsTabs(entry, id, userId)[0],
    dragging,
    setDragging,
    move: (m) => {
      if (m) reorder.mutate(m);
    },
    createIn: (category) => {
      create("channel", category);
    },
  };

  const menuItem = (label: string, icon: ReactNode, onSelect: () => void) => (
    <DropdownMenuItem onSelect={onSelect}>
      {label}
      {icon}
    </DropdownMenuItem>
  );
  return (
    <ListContext value={actions}>
      <DropdownMenu>
        <DropdownMenuTrigger
          aria-label={`Меню сервера ${guild.name}`}
          className="flex h-(--header-height) shrink-0 cursor-pointer items-center justify-between gap-2 px-4 text-base font-semibold shadow-[0_1px_0_var(--border)] transition-colors hover:bg-hover data-[state=open]:bg-hover"
        >
          <span className="truncate">{guild.name}</span>
          <ChevronDown aria-hidden className="size-5 shrink-0 text-fg-muted" />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-(--radix-dropdown-menu-trigger-width) min-w-52">
          {settingsTab &&
            menuItem("Настройки сервера", <Settings aria-hidden />, () => {
              void navigate({ to: "/guilds/$guildId/settings/$tab", params: { guildId, tab: settingsTab } });
            })}
          {canArrange && (
            <>
              {menuItem("Создать канал", <PlusCircle aria-hidden />, () => {
                create("channel", null);
              })}
              {menuItem("Создать категорию", <FolderPlus aria-hidden />, () => {
                create("category", null);
              })}
            </>
          )}
          {invite &&
            menuItem("Пригласить людей", <UserPlus aria-hidden />, () => {
              setDialog("invite");
            })}
          {!isOwner && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant="danger"
                onSelect={() => {
                  setDialog("leave");
                }}
              >
                Покинуть сервер
                <LogOut aria-hidden />
              </DropdownMenuItem>
            </>
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
      {canArrange && (
        <CreateChannelDialog
          open={dialog === "channel" || dialog === "category"}
          onOpenChange={close}
          guildId={guildId}
          kind={dialog === "category" ? "category" : "channel"}
          parent={parent ? { id: parent.id, name: parent.name } : null}
        />
      )}
    </ListContext>
  );
};
