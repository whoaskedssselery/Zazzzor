import { Hash } from "lucide-react";
import {
  type Channel,
  computeChannel,
  type GuildEntry,
  has,
  mentionsMe,
  PERM,
  RELATIONSHIP,
  useGuilds,
  userDisplay,
} from "@/entities/guild";
import { MemberMenu } from "@/features/manage-member";
import type { MentionSuggestion } from "@/features/send-message";
import type { MentionNames } from "@/shared/lib";
import type { ChatScope } from "../model/scope";
import { Chat } from "./Chat";

const Welcome = ({ name }: { name: string }) => (
  <div className="mx-4 mt-6 mb-2">
    <span aria-hidden className="flex size-17 items-center justify-center rounded-full bg-control">
      <Hash className="size-10 text-fg" strokeWidth={1.75} />
    </span>
    <h2 className="mt-3 text-xl font-bold break-words">{`Добро пожаловать в #${name}!`}</h2>
    <p className="mt-1 text-base text-fg-muted">{`Это начало канала #${name}.`}</p>
  </div>
);

/** Names for mentions as this server shows them: members by their name here, roles with their colour. */
const mentionNames = (entry: GuildEntry): MentionNames => ({
  user: (id) => {
    const member = entry.members[id];
    return member ? { name: userDisplay(entry, member.user).name } : null;
  },
  role: (id) => {
    const role = entry.roles[id];
    return role ? { name: role.name, color: role.color } : null;
  },
  emoji: (id) => entry.emojis[id] ?? null,
});

/**
 * Who `@` can reach in `channelId`: the members who can see it, by the name
 * they go by here; the roles that may be mentioned (all of them with
 * MENTION_EVERYONE, which also brings @everyone and @here).
 */
const mentionSuggestions = (
  entry: GuildEntry,
  channelId: string,
  meId: string | null,
  everyone: boolean,
): MentionSuggestion[] => {
  const users = Object.values(entry.members)
    .filter((m) => m.user.id !== meId && has(computeChannel(entry, channelId, m.user.id), PERM.VIEW_CHANNEL))
    .map((m): MentionSuggestion => {
      const view = userDisplay(entry, m.user);
      return { kind: "user", id: m.user.id, name: view.name, detail: m.user.username, avatarUrl: view.avatarUrl };
    })
    .sort((a, b) => a.name.localeCompare(b.name, "ru"));
  const roles = Object.values(entry.roles)
    .filter((r) => r.id !== entry.guild.id && (r.mentionable || everyone))
    .map((r): MentionSuggestion => ({ kind: "role", id: r.id, name: r.name, color: r.color }));
  const groups: MentionSuggestion[] = everyone
    ? [
        { kind: "everyone", id: null, name: "everyone", detail: "Все, кто видит канал" },
        { kind: "here", id: null, name: "here", detail: "Все, кто сейчас в сети" },
      ]
    : [];
  return [...users, ...roles, ...groups];
};

/**
 * The chat of a server's text channel. What the user may do follows their
 * permissions in the channel: without READ_MESSAGE_HISTORY no history is
 * asked for, without SEND_MESSAGES the composer gives way to a notice, and
 * only the author or MANAGE_MESSAGES may delete. Names show as the server
 * shows them; a member's name opens their card. The caller keys it by channel.
 */
export const ChatView = ({ guildId, channel }: { guildId: string; channel: Channel }) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const meId = useGuilds((s) => s.me?.id ?? null);
  const relationships = useGuilds((s) => s.relationships);
  if (!entry) return null;
  const perms = meId !== null ? computeChannel(entry, channel.id, meId) : 0n;
  const read = has(perms, PERM.READ_MESSAGE_HISTORY);
  const send = has(perms, PERM.SEND_MESSAGES);
  const where = `#${channel.name}`;
  const scope: ChatScope = {
    channelId: channel.id,
    listLabel: `Сообщения в ${where}`,
    composerLabel: `Сообщение в ${where}`,
    composerPlaceholder: `Написать в ${where}`,
    dropHint: `Файлы уйдут в ${where}`,
    welcome: <Welcome name={channel.name} />,
    can: {
      read,
      send,
      manage: has(perms, PERM.MANAGE_MESSAGES),
      attach: send && has(perms, PERM.ATTACH_FILES),
      react: read && has(perms, PERM.ADD_REACTIONS),
    },
    author: (user) => userDisplay(entry, user),
    typingName: (userId) => {
      const member = entry.members[userId];
      return member ? userDisplay(entry, member.user).name : "Кто-то";
    },
    mentions: mentionNames(entry),
    mentionables: () => mentionSuggestions(entry, channel.id, meId, has(perms, PERM.MENTION_EVERYONE)),
    emojis: entry.emojis,
    composerEmojis: () => Object.values(entry.emojis),
    mentioned: (m) => mentionsMe(entry, m),
    nameMenu: (userId, name) =>
      // A member's name opens their card; a former member's is only a name.
      userId in entry.members ? (
        <MemberMenu guildId={guildId} userId={userId} mode="dropdown">
          <button type="button" className="min-w-0 cursor-pointer truncate hover:underline">
            {name}
          </button>
        </MemberMenu>
      ) : (
        name
      ),
    hidden: (m) => m.author !== null && relationships[m.author.id]?.type === RELATIONSHIP.BLOCKED,
  };
  return <Chat scope={scope} />;
};
