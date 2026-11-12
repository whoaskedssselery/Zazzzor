import { Users } from "lucide-react";
import {
  type DMChannel,
  dmPeer,
  type GuildEmoji,
  getGuildsState,
  privateName,
  RELATIONSHIP,
  type User,
  useGuilds,
} from "@/entities/guild";
import type { MentionSuggestion } from "@/features/send-message";
import type { MentionNames } from "@/shared/lib";
import { Avatar } from "@/shared/ui";
import type { ChatScope } from "../model/scope";
import { Chat } from "./Chat";

/** The top of a DM or a group, as Discord opens one: who it is with, and that this is the start. */
const Welcome = ({ channel }: { channel: DMChannel }) => {
  const peer = dmPeer(channel);
  const name = privateName(channel);
  return (
    <div className="mx-4 mt-6 mb-2">
      {peer ? (
        <Avatar name={peer.display_name} src={peer.avatar_url} size={80} />
      ) : (
        <span aria-hidden className="flex size-20 items-center justify-center rounded-full bg-accent text-on-accent">
          <Users className="size-10" strokeWidth={1.75} />
        </span>
      )}
      <h2 className="mt-3 text-xl font-bold break-words">{name}</h2>
      {peer && <p className="text-base text-fg-muted">{peer.username}</p>}
      <p className="mt-2 text-base text-fg-muted">
        {peer ? `Это начало вашей переписки с @${peer.display_name}.` : `Добро пожаловать в группу «${name}».`}
      </p>
    </div>
  );
};

/** A custom emoji in a message, from any server the user is in (a DM has none of its own). */
const anyEmoji = (id: string): GuildEmoji | null => {
  for (const e of Object.values(getGuildsState().guilds)) {
    const emoji = e.emojis[id];
    if (emoji) return emoji;
  }
  return null;
};

/**
 * The chat of a DM or a group. Its rules are fixed, as in Discord: every
 * member reads, writes, attaches and reacts with ordinary emoji (the server
 * refuses a server's own); each edits and deletes only their own messages;
 * there are no roles and no @everyone. People show by their own names. A
 * message from someone the user blocked is folded. The caller keys it by channel.
 */
export const PrivateChatView = ({ channel }: { channel: DMChannel }) => {
  const me = useGuilds((s) => s.me);
  const relationships = useGuilds((s) => s.relationships);
  const people = new Map<string, User>(channel.recipients.map((u) => [u.id, u]));
  if (me) people.set(me.id, me);
  const peer = dmPeer(channel);
  const name = privateName(channel);
  const mentions: MentionNames = {
    user: (id) => {
      const u = people.get(id);
      return u ? { name: u.display_name } : null;
    },
    role: () => null,
    emoji: anyEmoji,
  };
  const scope: ChatScope = {
    channelId: channel.id,
    listLabel: peer ? `Сообщения с @${peer.display_name}` : `Сообщения в группе ${name}`,
    composerLabel: peer ? `Сообщение для @${peer.display_name}` : `Сообщение в группу ${name}`,
    composerPlaceholder: peer ? `Написать @${peer.display_name}` : `Написать в «${name}»`,
    dropHint: peer ? `Файлы уйдут в переписку с @${peer.display_name}` : `Файлы уйдут в «${name}»`,
    welcome: <Welcome channel={channel} />,
    can: { read: true, send: true, manage: false, attach: true, react: true },
    author: (user) => ({ name: user.display_name, color: 0, avatarUrl: user.avatar_url }),
    typingName: (userId) => people.get(userId)?.display_name ?? "Кто-то",
    mentions,
    mentionables: () =>
      channel.recipients
        .map(
          (u): MentionSuggestion => ({
            kind: "user",
            id: u.id,
            name: u.display_name,
            detail: u.username,
            avatarUrl: u.avatar_url,
          }),
        )
        .sort((a, b) => a.name.localeCompare(b.name, "ru")),
    emojis: {},
    composerEmojis: () => [],
    mentioned: (m) => me !== null && m.mention_user_ids.includes(me.id),
    nameMenu: (_userId, nameNode) => nameNode,
    hidden: (m) => m.author !== null && relationships[m.author.id]?.type === RELATIONSHIP.BLOCKED,
  };
  return <Chat scope={scope} />;
};
