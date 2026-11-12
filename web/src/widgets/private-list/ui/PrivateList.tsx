import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { Crown, Users, X } from "lucide-react";
import { useMemo, useState } from "react";
import {
  type DMChannel,
  dmPeer,
  isChannelMuted,
  isPrivateUnread,
  type PrivateEntry,
  privateList,
  privateBadgeCount,
  privateName,
  useClosePrivate,
  useGuilds,
  useRemoveRecipient,
} from "@/entities/guild";
import { NewPrivateButton } from "@/features/private-groups";
import { Avatar, Badge, ConfirmDialog, cn, formatCount, IconButton } from "@/shared/ui";

/** A group's picture: the same round tile for every group, as Discord's default. */
const GroupPicture = ({ size }: { size: 32 | 40 }) => (
  <span
    aria-hidden
    className={cn(
      "flex shrink-0 items-center justify-center rounded-full bg-accent text-on-accent",
      size === 32 ? "size-8" : "size-10",
    )}
  >
    <Users className={size === 32 ? "size-4" : "size-5"} strokeWidth={2} />
  </span>
);

/** What a leave or close of a group asks first; a DM closes without a question (it comes back with a message). */
type Leave = { channel: DMChannel } | null;

const Row = ({ entry, active, onLeave }: { entry: PrivateEntry; active: boolean; onLeave: (c: DMChannel) => void }) => {
  const { channel } = entry;
  const peer = dmPeer(channel);
  const online = useGuilds((s) => (peer ? s.presences[peer.id] === "online" : false));
  const close = useClosePrivate();
  const navigate = useNavigate();
  const name = privateName(channel);
  // A muted one stays quiet: no bold, no count, faded.
  const muted = useGuilds((s) => isChannelMuted(s, null, channel.id));
  const unread = !muted && isPrivateUnread(entry);
  const count = useGuilds((s) => privateBadgeCount(s, entry));
  let label = name;
  if (count > 0) label += `, непрочитанных: ${String(count)}`;
  if (muted) label += ", заглушено";
  return (
    <li className="group/row relative">
      <Link
        to="/channels/@me/$channelId"
        params={{ channelId: channel.id }}
        aria-label={label}
        aria-current={active ? "page" : undefined}
        className={cn(
          "flex h-11 items-center gap-3 rounded-md px-2 pr-9 text-fg-muted transition-colors duration-(--duration-fast) hover:bg-hover hover:text-fg",
          active && "bg-selected text-fg",
          unread && "text-fg",
          muted && !active && "opacity-60",
        )}
      >
        {peer ? (
          <Avatar name={peer.display_name} src={peer.avatar_url} size={32} status={online ? "online" : "offline"} />
        ) : (
          <GroupPicture size={32} />
        )}
        <span className="min-w-0 flex-1">
          <span className={cn("block truncate text-base", unread ? "font-semibold" : "font-medium")}>{name}</span>
          {!peer && (
            <span className="block truncate text-xs text-fg-faint">{`Участников: ${String(channel.recipients.length + 1)}`}</span>
          )}
        </span>
        {count > 0 && (
          <Badge variant="count" aria-hidden>
            {formatCount(count)}
          </Badge>
        )}
      </Link>
      <IconButton
        label={peer ? `Закрыть переписку с ${name}` : `Покинуть группу ${name}`}
        icon={X}
        className={cn(
          "absolute top-1/2 right-1 size-7 -translate-y-1/2 opacity-0 group-hover/row:opacity-100 focus-visible:opacity-100 [&>svg]:size-4",
          count > 0 && "group-hover/row:opacity-100",
        )}
        onClick={() => {
          if (!peer) {
            onLeave(channel);
            return;
          }
          close.mutate(channel.id);
          if (active) void navigate({ to: "/channels/@me" });
        }}
      />
    </li>
  );
};

/**
 * «Личные сообщения» on home, as in Discord: the user's DMs and groups,
 * the most recent first, each with its unread count; a click opens it,
 * the cross closes a DM (it comes back with the next message) or leaves a
 * group (asked first). The plus starts a new one.
 */
export const PrivateList = () => {
  const privates = useGuilds((s) => s.privates);
  const list = useMemo(() => privateList({ privates }), [privates]);
  const { channelId } = useParams({ strict: false });
  const [leave, setLeave] = useState<Leave>(null);
  const remove = useRemoveRecipient();
  const meId = useGuilds((s) => s.me?.id ?? null);
  const navigate = useNavigate();
  return (
    <section aria-labelledby="private-list-title" className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center justify-between px-4 pt-3 pb-1">
        <h2 id="private-list-title" className="text-xs font-semibold text-fg-faint uppercase">
          Личные сообщения
        </h2>
        <NewPrivateButton />
      </div>
      {list.length === 0 ? (
        <p className="px-4 pt-1 text-sm text-fg-faint">
          Здесь появятся переписки. Напишите другу со страницы «Друзья» или нажмите плюс.
        </p>
      ) : (
        <ul className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto px-2 pb-2">
          {list.map((p) => (
            <Row
              key={p.channel.id}
              entry={p}
              active={p.channel.id === channelId}
              onLeave={(c) => {
                setLeave({ channel: c });
              }}
            />
          ))}
        </ul>
      )}
      <ConfirmDialog
        open={leave !== null}
        onOpenChange={(open) => {
          if (!open) setLeave(null);
        }}
        title={`Покинуть группу «${leave ? privateName(leave.channel) : ""}»?`}
        description="Вы перестанете получать её сообщения. Вернуться можно, только если вас снова позовут."
        confirmLabel="Покинуть группу"
        tone="danger"
        pending={remove.isPending}
        onConfirm={() => {
          if (!leave || meId === null) return;
          const id = leave.channel.id;
          remove.mutate(
            { id, userId: meId },
            {
              onSettled: () => {
                setLeave(null);
              },
              onSuccess: () => {
                if (id === channelId) void navigate({ to: "/channels/@me" });
              },
            },
          );
        }}
      />
    </section>
  );
};

/**
 * The people of a group in the member column: the user too, the owner
 * with a crown. The owner may remove anyone else, as in Discord.
 */
export const PrivateMembers = ({ channelId }: { channelId: string }) => {
  const channel = useGuilds((s) => s.privates[channelId]?.channel);
  const me = useGuilds((s) => s.me);
  const presences = useGuilds((s) => s.presences);
  const remove = useRemoveRecipient();
  const [kick, setKick] = useState<{ id: string; name: string } | null>(null);
  if (!channel || !me) return null;
  const people = [me, ...channel.recipients];
  const mineToManage = channel.owner_id === me.id;
  return (
    <section aria-label="Участники группы" className="flex min-h-0 flex-col overflow-y-auto px-2 py-4">
      <h2 className="px-2 pb-2 text-xs font-semibold text-fg-faint uppercase">{`Участники — ${String(people.length)}`}</h2>
      <ul className="flex flex-col gap-0.5">
        {people.map((u) => {
          const owner = channel.owner_id === u.id;
          const online = presences[u.id] === "online";
          return (
            <li key={u.id} className="group/member flex h-11 items-center gap-3 rounded-md px-2 hover:bg-hover">
              <Avatar name={u.display_name} src={u.avatar_url} size={32} status={online ? "online" : "offline"} />
              <span className={cn("min-w-0 flex-1 truncate text-base font-medium", !online && "text-fg-muted")}>
                {u.display_name}
              </span>
              {owner && <Crown role="img" aria-label="Создатель группы" className="size-4 shrink-0 text-fg-warning" />}
              {mineToManage && u.id !== me.id && (
                <IconButton
                  label={`Исключить ${u.display_name} из группы`}
                  icon={X}
                  className="size-7 opacity-0 group-hover/member:opacity-100 focus-visible:opacity-100 [&>svg]:size-4"
                  onClick={() => {
                    setKick({ id: u.id, name: u.display_name });
                  }}
                />
              )}
            </li>
          );
        })}
      </ul>
      <ConfirmDialog
        open={kick !== null}
        onOpenChange={(open) => {
          if (!open) setKick(null);
        }}
        title={`Исключить ${kick?.name ?? ""} из группы?`}
        description="Человек перестанет видеть группу и её сообщения."
        confirmLabel="Исключить"
        tone="danger"
        pending={remove.isPending}
        onConfirm={() => {
          if (!kick) return;
          remove.mutate(
            { id: channel.id, userId: kick.id },
            {
              onSettled: () => {
                setKick(null);
              },
            },
          );
        }}
      />
    </section>
  );
};
