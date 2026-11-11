import { Link } from "@tanstack/react-router";
import { Compass, House, type LucideIcon, Plus } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useChannelToOpen } from "@/entities/channel";
import { guildMentionCount, isGuildUnread, useGuilds } from "@/entities/guild";
import { CreateGuildDialog } from "@/features/create-guild";
import { JoinGuildDialog } from "@/features/join-guild";
import { Badge, cn, formatCount, GuildIcon, Tooltip } from "@/shared/ui";

/** The round tile of the rail's own buttons (home, create, join): it squares off on hover and focus. */
const tileClass =
  "flex size-12 items-center justify-center rounded-[50%] bg-guild transition-[border-radius,background-color,color] duration-(--duration-base) ease-(--ease-out) " +
  "group-hover:rounded-xl group-hover:bg-accent group-hover:text-on-accent group-focus-visible:rounded-xl group-focus-visible:bg-accent group-focus-visible:text-on-accent";

/**
 * One place in the rail: the pill on its left edge (tall for the open
 * server, short for one with unread messages) and a tooltip with the name on the right.
 */
const RailItem = ({
  label,
  active = false,
  unread = false,
  children,
}: {
  label: string;
  active?: boolean;
  unread?: boolean;
  children: ReactNode;
}) => (
  <li className="relative flex w-full shrink-0 justify-center">
    <span
      aria-hidden
      className={cn(
        "absolute top-1/2 left-0 w-1 -translate-y-1/2 rounded-r-full bg-fg transition-[height] duration-(--duration-base)",
        active ? "h-10" : unread ? "h-2" : "h-0",
      )}
    />
    <Tooltip content={label} side="right" delayDuration={0}>
      {children}
    </Tooltip>
  </li>
);

const RailButton = ({ label, icon: Icon, onClick }: { label: string; icon: LucideIcon; onClick: () => void }) => (
  <RailItem label={label}>
    <button type="button" aria-label={label} onClick={onClick} className="group cursor-pointer rounded-xl">
      <span className={cn(tileClass, "text-fg-success")}>
        <Icon aria-hidden className="size-6" strokeWidth={2} />
      </span>
    </button>
  </RailItem>
);

/**
 * A server of the rail. It links straight to the channel the server opens on
 * (the last one opened there, else the first visible one), so a click is one
 * navigation; a server with no text channel to open links to its own view.
 */
const GuildItem = ({ guildId, active }: { guildId: string; active: boolean }) => {
  const guild = useGuilds((s) => s.guilds[guildId]?.guild);
  const channelId = useChannelToOpen(guildId);
  const unread = useGuilds((s) => {
    const e = s.guilds[guildId];
    return e !== undefined && isGuildUnread(e);
  });
  const mentions = useGuilds((s) => {
    const e = s.guilds[guildId];
    return e === undefined ? 0 : guildMentionCount(e);
  });
  if (!guild) return null;
  let name = guild.name;
  if (mentions > 0) name += `, упоминаний: ${String(mentions)}`;
  else if (unread) name += ", есть непрочитанные";
  const target =
    channelId === null
      ? ({ to: "/channels/$guildId", params: { guildId } } as const)
      : ({ to: "/channels/$guildId/$channelId", params: { guildId, channelId } } as const);
  return (
    <RailItem label={guild.name} active={active} unread={unread}>
      <Link
        {...target}
        aria-label={name}
        aria-current={active ? "page" : undefined}
        className="group relative rounded-xl"
      >
        <GuildIcon name={guild.name} src={guild.icon_url} active={active} />
        {mentions > 0 && (
          <Badge
            variant="count"
            aria-hidden
            className="absolute -right-1 -bottom-1 h-5 min-w-5 px-1.5 ring-4 ring-(--bg-rail)"
          >
            {formatCount(mentions)}
          </Badge>
        )}
      </Link>
    </RailItem>
  );
};

/**
 * The server rail: home, the servers in the order of READY (then of
 * joining), and the buttons to create a server or join one. The open server
 * (`activeGuildId`; null for home) is marked by the pill and a squared icon.
 */
export const GuildRail = ({ activeGuildId }: { activeGuildId: string | null }) => {
  const order = useGuilds((s) => s.order);
  const [dialog, setDialog] = useState<"create" | "join" | null>(null);
  const home = activeGuildId === null;
  const close = (open: boolean) => {
    if (!open) setDialog(null);
  };
  return (
    <nav aria-label="Серверы" className="min-h-0 flex-1 overflow-y-auto py-3 [scrollbar-width:none]">
      <ul className="flex flex-col items-center gap-2">
        <RailItem label="Главная" active={home}>
          <Link
            to="/channels/@me"
            aria-label="Главная"
            aria-current={home ? "page" : undefined}
            className="group rounded-xl"
          >
            <span className={cn(tileClass, home ? "rounded-xl bg-accent text-on-accent" : "text-fg")}>
              <House aria-hidden className="size-6" strokeWidth={1.75} />
            </span>
          </Link>
        </RailItem>
        <li aria-hidden className="h-0.5 w-8 shrink-0 rounded-full bg-control" />
        {order.map((id) => (
          <GuildItem key={id} guildId={id} active={id === activeGuildId} />
        ))}
        <RailButton
          label="Создать сервер"
          icon={Plus}
          onClick={() => {
            setDialog("create");
          }}
        />
        <RailButton
          label="Присоединиться к серверу"
          icon={Compass}
          onClick={() => {
            setDialog("join");
          }}
        />
      </ul>
      <CreateGuildDialog open={dialog === "create"} onOpenChange={close} />
      <JoinGuildDialog open={dialog === "join"} onOpenChange={close} />
    </nav>
  );
};
