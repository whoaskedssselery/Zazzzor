import { PERM } from "../model/permissions";

/**
 * Where a permission is set: "guild" only on roles (it means nothing for a
 * single channel), "both" on roles and in channel overwrites. None is
 * channel-only: every channel permission has a server-wide default.
 */
export type PermissionScope = "guild" | "both";

export interface PermissionItem {
  bit: bigint;
  name: string;
  description: string;
  scope: PermissionScope;
}

export interface PermissionGroup {
  title: string;
  items: PermissionItem[];
}

const item = (bit: bigint, name: string, description: string, scope: PermissionScope = "both"): PermissionItem => ({
  bit,
  name,
  description,
  scope,
});

/**
 * Every permission, in the groups and order the role and channel editors
 * show them, with its Russian name and what it allows. The server refuses
 * ADMINISTRATOR in channel overwrites, so it is "guild".
 */
export const PERMISSION_GROUPS: PermissionGroup[] = [
  {
    title: "Общие",
    items: [
      item(PERM.VIEW_CHANNEL, "Просматривать каналы", "Видеть каналы и читать в них сообщения по мере появления."),
      item(PERM.MANAGE_CHANNELS, "Управлять каналами", "Создавать, переименовывать, перемещать и удалять каналы."),
      item(PERM.MANAGE_ROLES, "Управлять ролями", "Создавать роли ниже своей высшей и менять права каналов."),
    ],
  },
  {
    title: "Участники",
    items: [
      item(PERM.CREATE_INVITE, "Создавать приглашения", "Приглашать новых участников на сервер."),
      item(PERM.CHANGE_NICKNAME, "Менять свой ник", "Задавать себе ник на этом сервере.", "guild"),
      item(PERM.MANAGE_NICKNAMES, "Управлять никами", "Менять ники других участников.", "guild"),
      item(PERM.KICK_MEMBERS, "Выгонять участников", "Удалять с сервера участников ниже своей роли.", "guild"),
      item(PERM.BAN_MEMBERS, "Банить участников", "Удалять участников ниже своей роли без права вернуться.", "guild"),
    ],
  },
  {
    title: "Текстовые каналы",
    items: [
      item(PERM.SEND_MESSAGES, "Отправлять сообщения", "Писать в текстовых каналах."),
      item(PERM.ATTACH_FILES, "Прикреплять файлы", "Отправлять картинки и файлы."),
      item(PERM.MENTION_EVERYONE, "Упоминать @everyone", "Упоминать всех участников сразу."),
      item(PERM.MANAGE_MESSAGES, "Управлять сообщениями", "Удалять сообщения других участников."),
      item(PERM.READ_MESSAGE_HISTORY, "Читать историю", "Видеть сообщения, отправленные до входа в канал."),
    ],
  },
  {
    title: "Голосовые каналы",
    items: [
      item(PERM.CONNECT, "Подключаться", "Заходить в голосовые каналы."),
      item(PERM.SPEAK, "Говорить", "Включать микрофон в голосовых каналах."),
      item(PERM.STREAM, "Показывать экран", "Демонстрировать экран или окно в голосовом канале."),
      item(PERM.USE_VOICE_ACTIVITY, "Активация по голосу", "Говорить без нажатия клавиши."),
      item(PERM.MUTE_MEMBERS, "Отключать микрофон участникам", "Заглушать других в голосовых каналах."),
      item(PERM.DEAFEN_MEMBERS, "Отключать звук участникам", "Лишать других звука в голосовых каналах."),
      item(PERM.MOVE_MEMBERS, "Перемещать участников", "Переводить других между голосовыми каналами и отключать их."),
    ],
  },
  {
    title: "Сервер",
    items: [
      item(PERM.MANAGE_GUILD, "Управлять сервером", "Менять название и иконку, видеть и удалять приглашения.", "guild"),
      item(
        PERM.ADMINISTRATOR,
        "Администратор",
        "Все права и доступ ко всем каналам. Опасно: давайте только тем, кому доверяете.",
        "guild",
      ),
    ],
  },
];

/** The groups as a channel's permission editor shows them: without the server-only permissions. */
export const CHANNEL_PERMISSION_GROUPS: PermissionGroup[] = PERMISSION_GROUPS.map((g) => ({
  ...g,
  items: g.items.filter((i) => i.scope === "both"),
})).filter((g) => g.items.length > 0);
