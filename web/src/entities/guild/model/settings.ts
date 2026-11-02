import { computeBase, has, PERM } from "./permissions";
import type { GuildEntry } from "./reducer";

/** The sections of a server's settings, in the order the settings list them. */
export const GUILD_SETTINGS_TABS = ["overview", "roles", "members", "bans", "invites"] as const;
export type GuildSettingsTab = (typeof GUILD_SETTINGS_TABS)[number];

export const GUILD_SETTINGS_TITLES: Record<GuildSettingsTab, string> = {
  overview: "Обзор",
  roles: "Роли",
  members: "Участники",
  bans: "Баны",
  invites: "Приглашения",
};

/** What each section needs: any one of the bits. */
const NEEDS: Record<GuildSettingsTab, bigint[]> = {
  overview: [PERM.MANAGE_GUILD],
  roles: [PERM.MANAGE_ROLES],
  members: [PERM.KICK_MEMBERS, PERM.BAN_MEMBERS, PERM.MANAGE_ROLES, PERM.MANAGE_NICKNAMES],
  bans: [PERM.BAN_MEMBERS],
  invites: [PERM.MANAGE_GUILD],
};

export const isGuildSettingsTab = (s: string): s is GuildSettingsTab =>
  (GUILD_SETTINGS_TABS as readonly string[]).includes(s);

/**
 * The settings sections `userId` may open in server `entry`, by their
 * server-wide permissions (the owner and ADMINISTRATOR have them all).
 * Empty: the server menu offers no settings at all.
 */
export function guildSettingsTabs(entry: GuildEntry, userId: string): GuildSettingsTab[] {
  const base = computeBase(entry, userId);
  return GUILD_SETTINGS_TABS.filter((tab) => NEEDS[tab].some((bit) => has(base, bit)));
}
