import { type GuildEntry, PERMISSION_GROUPS, parseMask } from "@/entities/guild";
import type { components } from "@/shared/api";
import { t, textTable } from "@/shared/i18n";

export type AuditLog = components["schemas"]["AuditLog"];
export type AuditEntry = AuditLog["audit_log_entries"][number];
type AuditChange = AuditEntry["changes"][number];
type AuditValue = AuditChange["old_value"];
type User = AuditLog["users"][number];

/** The actions the log names, in the order the filter lists them. */
export const AUDIT_ACTIONS = [
  20, 22, 23, 24, 25, 72, 30, 31, 32, 10, 11, 12, 13, 14, 15, 40, 42, 60, 61, 62, 1, 26, 27,
] as const;

const isNamed = (action: number): action is (typeof AUDIT_ACTIONS)[number] =>
  (AUDIT_ACTIONS as readonly number[]).includes(action);

/** What an action is called. */
export const actionTitle = (action: number): string =>
  isNamed(action) ? t(`audit:action.${action}`) : t("audit:action.other");

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
    if (id === null) return t("audit:deletedAccount");
    const member = entry.members[id];
    return member?.nick ?? users.get(id)?.display_name ?? member?.user.display_name ?? t("audit:deletedAccount");
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
    return name === null ? t("audit:deletedChannel") : `#${name}`;
  }
  if (ROLE_ACTIONS.has(action)) return (id === null ? null : names.role(id)) ?? nameIn(e) ?? t("audit:deletedRole");
  if (EMOJI_ACTIONS.has(action)) {
    const name = nameIn(e);
    return name === null ? t("audit:emoji") : `:${name}:`;
  }
  if (action === 40 || action === 42) return e.options.code ?? null;
  return null;
}

/** Extra facts worth a line: the channel of a deleted message or an invite, whom an overwrite is for. */
export function auditDetail(e: AuditEntry, names: AuditNames): string | null {
  const { channel_id: channelId, id, type } = e.options;
  if ((e.action_type === 72 || e.action_type === 40 || e.action_type === 42) && channelId !== undefined) {
    return t("audit:inChannel", { channel: names.channel(channelId) ?? t("audit:deletedChannel") });
  }
  if (CHANNEL_ACTIONS.has(e.action_type) && id !== undefined) {
    return type === "1"
      ? t("audit:forMember", { name: names.user(id) })
      : t("audit:forRole", { name: names.role(id) ?? t("audit:deletedRole") });
  }
  return null;
}

const KEY_TITLE: Readonly<Record<string, string>> = textTable({
  name: () => t("audit:key.name"),
  topic: () => t("audit:key.topic"),
  nick: () => t("audit:key.nick"),
  color: () => t("audit:key.color"),
  hoist: () => t("audit:key.hoist"),
  mentionable: () => t("audit:key.mentionable"),
  parent_id: () => t("audit:key.parent_id"),
  permission_synced: () => t("audit:key.permission_synced"),
  type: () => t("audit:key.type"),
  max_uses: () => t("audit:key.max_uses"),
  max_age: () => t("audit:key.max_age"),
  icon_hash: () => t("audit:key.icon_hash"),
  permissions: () => t("audit:key.permissions"),
  allow: () => t("audit:key.allow"),
  deny: () => t("audit:key.deny"),
  $add: () => t("audit:key.$add"),
  $remove: () => t("audit:key.$remove"),
});

/** An invite lifetime in words: «7 дней», «12 часов», «30 минут». */
export const durationText = (seconds: number): string => {
  if (seconds % 86400 === 0) {
    return t("audit:days", { count: seconds / 86400 });
  }
  if (seconds % 3600 === 0) {
    return t("audit:hours", { count: seconds / 3600 });
  }
  return t("audit:minutes", { count: Math.round(seconds / 60) });
};

const MASK_KEYS = new Set(["permissions", "allow", "deny"]);

const PERMISSION_ITEMS = PERMISSION_GROUPS.flatMap((g) => g.items);

const maskOf = (v: AuditValue): bigint => (typeof v === "string" ? (parseMask(v) ?? 0n) : 0n);

/** The permissions a mask change granted and took away, by name. */
const maskDiff = (before: AuditValue, after: AuditValue): string => {
  const was = maskOf(before);
  const now = maskOf(after);
  const parts: string[] = [];
  for (const { bit, name } of PERMISSION_ITEMS) {
    const had = (was & bit) !== 0n;
    const has = (now & bit) !== 0n;
    if (has && !had) parts.push(`+ ${name}`);
    if (had && !has) parts.push(`− ${name}`);
  }
  return parts.length === 0 ? t("audit:nothing") : parts.join(", ");
};

const formatValue = (key: string, v: AuditValue, names: AuditNames): string => {
  if (v === null || v === undefined || v === "") return "—";
  if (typeof v === "boolean") return v ? t("audit:yes") : t("audit:no");
  if (Array.isArray(v)) return v.map((id) => names.role(id) ?? t("audit:deletedRole")).join(", ");
  if (key === "parent_id" && typeof v === "string") return names.channel(v) ?? t("audit:deletedCategory");
  if (key === "color" && typeof v === "number")
    return v === 0 ? t("audit:noColor") : `#${v.toString(16).padStart(6, "0")}`;
  if (key === "max_uses" && v === 0) return t("audit:noLimit");
  if (key === "max_age" && typeof v === "number") return v === 0 ? t("audit:never") : durationText(v);
  if (key === "icon_hash") return t("audit:image");
  if (key === "type" && typeof v === "number")
    return v === 2 ? t("audit:voice") : v === 4 ? t("audit:category") : t("audit:text");
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
