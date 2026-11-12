import type { ReactNode } from "react";
import type { GuildEmoji, User } from "@/entities/guild";
import type { AuthorView, Message } from "@/entities/message";
import type { MentionSuggestion } from "@/features/send-message";
import type { MentionNames } from "@/shared/lib";

/**
 * Where a chat is and what the user may do there. A server channel builds
 * it from the member's permissions and the server's names (ChatView); a DM
 * or a group from its fixed rules and its people (PrivateChatView). The
 * chat itself (Chat) knows nothing of servers.
 */
export interface ChatScope {
  channelId: string;
  /** The message list's name: «Сообщения в #общий». */
  listLabel: string;
  /** The composer field's name and its empty text. */
  composerLabel: string;
  composerPlaceholder: string;
  /** What the hint over dropped files says: «Файлы уйдут в #общий». */
  dropHint: string;
  /** The top of the history, once it is all loaded. */
  welcome: ReactNode;
  can: {
    /** READ_MESSAGE_HISTORY: without it no history is asked for. */
    read: boolean;
    /** SEND_MESSAGES: without it the composer gives way to a notice. */
    send: boolean;
    /** MANAGE_MESSAGES: delete anyone's message. */
    manage: boolean;
    attach: boolean;
    react: boolean;
  };
  /** How a user shows here: their name, colour and avatar. */
  author: (user: User) => AuthorView;
  /** How a typing user is named. */
  typingName: (userId: string) => string;
  mentions: MentionNames;
  /** Who and what `@` can reach here. */
  mentionables: () => MentionSuggestion[];
  /** The custom emoji the reaction picker and the bar know, by ID. */
  emojis: Record<string, GuildEmoji>;
  /** The custom emoji `:name` offers in the composer. */
  composerEmojis: () => GuildEmoji[];
  /** Whether message `m` mentions the user (it is highlighted). */
  mentioned: (m: Message) => boolean;
  /** An author's name, wrapped in what a click on it opens (a member's card); the name alone where there is none. */
  nameMenu: (userId: string, name: ReactNode) => ReactNode;
  /** Whether message `m` is hidden behind «Показать» (its author is blocked). */
  hidden: (m: Message) => boolean;
}
