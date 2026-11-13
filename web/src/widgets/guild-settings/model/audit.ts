import { type GuildEntry, PERMISSION_GROUPS, parseMask } from "@/entities/guild";
import type { components } from "@/shared/api";

export type AuditLog = components["schemas"]["AuditLog"];
export type AuditEntry = AuditLog["audit_log_entries"][number];
type AuditChange = AuditEntry["changes"][number];
type AuditValue = AuditChange["old_value"];
type User = AuditLog["users"][number];

/** What each action is called, in the order the filter lists them. */
export const AUDIT_ACTIONS: ReadonlyArray<readonly [number, string]> = [
  [20, "Участник выгнан"],
  [22, "Участник забанен"],
  [23, "Бан снят"],
  [24, "Ник изменён"],
  [25, "Роли участника изменены"],
  [72, "Сообщение удалено"],
  [30, "Роль создана"],
  [31, "Роль изменена"],
  [32, "Роль удалена"],
  [10, "Канал создан"],
  [11, "Канал изменён"],
  [12, "Канал удалён"],
  [13, "Права канала добавлены"],
  [14, "Права канала изменены"],
  [15, "Права канала убраны"],
  [40, "Приглашение создано"],
  [42, "Приглашение удалено"],
  [60, "Эмодзи добавлено"],
  [61, "Эмодзи переименовано"],
  [62, "Эмодзи удалено"],
  [1, "Сервер изменён"],
  [26, "Участник перемещён"],
  [27, "Участник отключён"],
];

const ACTION_TITLE = new Map(AUDIT_ACTIONS);

export const actionTitle = (action: number): string => ACTION_TITLE.get(action) ?? "Действие";

const USER_ACTIONS = new Set([20, 22, 23, 24, 25, 26, 27, 72]);
const CHANNEL_ACTIONS = new Set([10, 11, 12, 13, 14, 15]);
const ROLE_ACTIONS = new Set([30, 31, 32]);
const EMOJI_ACTIONS = new Set([60, 61, 62]);

/** The names a page of the log can refer to. */
export interface AuditNames {
  user: (id: string | null) => string;
  role: (id: string) => string | null;
  channel: (id: string) => string | null;
}

/** Names from the page's users, then the server's members, roles and channels. */
export const auditNames = (entry: GuildEntry, users: ReadonlyMap<string, User>): AuditNames => ({
  user: (id) => {
    if (id === null) return "Удалённый аккаунт";
    const member = entry.members[id];
    return member?.nick ?? users.get(id)?.display_name ?? member?.user.display_name ?? "Удалённый аккаунт";
  },
  role: (id) => entry.roles[id]?.name ?? null,
  channel: (id) => {
    const c = entry.channels[id];
    return c === undefined ? null : `#${c.name}`;
  },
});

/** The value a change had before or after, as text: a name a creation or deletion carries. */
const nameIn = (e: AuditEntry): string | null => {
  const name = e.changes.find((c) => c.key === "name");
  const v = name?.new_value ?? name?.old_value;
  return typeof v === "string" ? v : null;
};

/** What an entry acted on, in words: «Пётр», «#новости», «модераторы», «:кот:». */
export function auditTarget(e: AuditEntry, names: AuditNames): string | null {
  const id = e.target_id;
  const action = e.action_type;
  if (USER_ACTIONS.has(action)) return names.user(id);
  if (CHANNEL_ACTIONS.has(action)) {
    const known = id === null ? null : names.channel(id);
    if (known !== null) return known;
    const name = nameIn(e);
    return name === null ? "удалённый канал" : `#${name}`;
  }
  if (ROLE_ACTIONS.has(action)) return (id === null ? null : names.role(id)) ?? nameIn(e) ?? "удалённая роль";
  if (EMOJI_ACTIONS.has(action)) {
    const name = nameIn(e);
    return name === null ? "эмодзи" : `:${name}:`;
  }
  if (action === 40 || action === 42) return e.options.code ?? null;
  return null;
}

/** Extra facts worth a line: the channel of a deleted message or an invite, whom an overwrite is for. */
export function auditDetail(e: AuditEntry, names: AuditNames): string | null {
  const { channel_id: channelId, id, type } = e.options;
  if ((e.action_type === 72 || e.action_type === 40 || e.action_type === 42) && channelId !== undefined) {
    return `в канале ${names.channel(channelId) ?? "удалённый канал"}`;
  }
  if (CHANNEL_ACTIONS.has(e.action_type) && id !== undefined) {
    return type === "1" ? `для участника ${names.user(id)}` : `для роли ${names.role(id) ?? "удалённая роль"}`;
  }
  return null;
}

const KEY_TITLE: Record<string, string> = {
  name: "Название",
  topic: "Тема",
  nick: "Ник",
  color: "Цвет",
  hoist: "Отдельно в списке",
  mentionable: "Можно упоминать",
  parent_id: "Категория",
  permission_synced: "Как в категории",
  type: "Тип",
  max_uses: "Использований",
  max_age: "Срок",
  icon_hash: "Значок",
  permissions: "Права",
  allow: "Разрешено",
  deny: "Запрещено",
  $add: "Выданы роли",
  $remove: "Сняты роли",
};

const plural = new Intl.PluralRules("ru-RU");
const DAY_WORDS: Record<string, string> = { one: "день", few: "дня", many: "дней", other: "дня" };
const HOUR_WORDS: Record<string, string> = { one: "час", few: "часа", many: "часов", other: "часа" };

/** An invite lifetime in words: «7 дней», «12 часов», «30 минут». */
export const durationText = (seconds: number): string => {
  if (seconds % 86400 === 0) {
    const n = seconds / 86400;
    return `${String(n)} ${DAY_WORDS[plural.select(n)] ?? "дн."}`;
  }
  if (seconds % 3600 === 0) {
    const n = seconds / 3600;
    return `${String(n)} ${HOUR_WORDS[plural.select(n)] ?? "ч"}`;
  }
  return `${String(Math.round(seconds / 60))} мин`;
};

const MASK_KEYS = new Set(["permissions", "allow", "deny"]);

const PERMISSION_NAMES = new Map(PERMISSION_GROUPS.flatMap((g) => g.items.map((i) => [i.bit, i.name] as const)));

const maskOf = (v: AuditValue): bigint => (typeof v === "string" ? (parseMask(v) ?? 0n) : 0n);

/** The permissions a mask change granted and took away, by name. */
const maskDiff = (before: AuditValue, after: AuditValue): string => {
  const was = maskOf(before);
  const now = maskOf(after);
  const parts: string[] = [];
  for (const [bit, name] of PERMISSION_NAMES) {
    const had = (was & bit) !== 0n;
    const has = (now & bit) !== 0n;
    if (has && !had) parts.push(`+ ${name}`);
    if (had && !has) parts.push(`− ${name}`);
  }
  return parts.length === 0 ? "ничего" : parts.join(", ");
};

const formatValue = (key: string, v: AuditValue, names: AuditNames): string => {
  if (v === null || v === undefined || v === "") return "—";
  if (typeof v === "boolean") return v ? "да" : "нет";
  if (Array.isArray(v)) return v.map((id) => names.role(id) ?? "удалённая роль").join(", ");
  if (key === "parent_id" && typeof v === "string") return names.channel(v) ?? "удалённая категория";
  if (key === "color" && typeof v === "number") return v === 0 ? "без цвета" : `#${v.toString(16).padStart(6, "0")}`;
  if (key === "max_uses" && v === 0) return "без ограничений";
  if (key === "max_age" && typeof v === "number") return v === 0 ? "бессрочно" : durationText(v);
  if (key === "icon_hash") return "картинка";
  if (key === "type" && typeof v === "number") return v === 2 ? "голосовой" : v === 4 ? "категория" : "текстовый";
  return String(v);
};

/** One change as a line: «Название: helpers → moderators», «Права: + Банить участников». */
export function changeText(c: AuditChange, names: AuditNames): string {
  const title = KEY_TITLE[c.key] ?? c.key;
  if (MASK_KEYS.has(c.key)) return `${title}: ${maskDiff(c.old_value, c.new_value)}`;
  if (c.key === "$add" || c.key === "$remove") return `${title}: ${formatValue(c.key, c.new_value, names)}`;
  const hasOld = c.old_value !== undefined;
  const hasNew = c.new_value !== undefined;
  if (hasOld && hasNew) {
    return `${title}: ${formatValue(c.key, c.old_value, names)} → ${formatValue(c.key, c.new_value, names)}`;
  }
  return `${title}: ${formatValue(c.key, hasNew ? c.new_value : c.old_value, names)}`;
}
