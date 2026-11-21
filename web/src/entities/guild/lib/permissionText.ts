import { t } from "@/shared/i18n";
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

type PermKey = keyof typeof PERM;
type GroupKey = "general" | "members" | "text" | "voice" | "server";

/** A permission whose name and description are read in the language of the moment. */
const item = (key: PermKey, scope: PermissionScope = "both"): PermissionItem => ({
  bit: PERM[key],
  scope,
  get name() {
    return t(`guild:permissions.${key}.name`);
  },
  get description() {
    return t(`guild:permissions.${key}.description`);
  },
});

const group = (key: GroupKey, items: PermissionItem[]): PermissionGroup => ({
  get title() {
    return t(`guild:permissionGroups.${key}`);
  },
  items,
});

/**
 * Every permission, in the groups and order the role and channel editors
 * show them, with its name and what it allows. The server refuses
 * ADMINISTRATOR in channel overwrites, so it is "guild".
 */
export const PERMISSION_GROUPS: PermissionGroup[] = [
  group("general", [
    item("VIEW_CHANNEL"),
    item("MANAGE_CHANNELS"),
    item("MANAGE_ROLES"),
    item("MANAGE_EXPRESSIONS", "guild"),
    item("VIEW_AUDIT_LOG", "guild"),
  ]),
  group("members", [
    item("CREATE_INVITE"),
    item("CHANGE_NICKNAME", "guild"),
    item("MANAGE_NICKNAMES", "guild"),
    item("KICK_MEMBERS", "guild"),
    item("BAN_MEMBERS", "guild"),
  ]),
  group("text", [
    item("SEND_MESSAGES"),
    item("ATTACH_FILES"),
    item("ADD_REACTIONS"),
    item("MENTION_EVERYONE"),
    item("MANAGE_MESSAGES"),
    item("READ_MESSAGE_HISTORY"),
  ]),
  group("voice", [
    item("CONNECT"),
    item("SPEAK"),
    item("STREAM"),
    item("USE_VOICE_ACTIVITY"),
    item("MUTE_MEMBERS"),
    item("DEAFEN_MEMBERS"),
    item("MOVE_MEMBERS"),
  ]),
  group("server", [item("MANAGE_GUILD", "guild"), item("ADMINISTRATOR", "guild")]),
];

/** The groups as a channel's permission editor shows them: without the server-only permissions. */
export const CHANNEL_PERMISSION_GROUPS: PermissionGroup[] = PERMISSION_GROUPS.map((g) => ({
  get title() {
    return g.title;
  },
  items: g.items.filter((i) => i.scope === "both"),
})).filter((g) => g.items.length > 0);
