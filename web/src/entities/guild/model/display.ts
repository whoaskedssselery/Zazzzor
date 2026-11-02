import type { GuildEntry, Member, Role, User } from "./reducer";

/** Higher position first; equal positions (not expected) by ID, so the order is stable. */
export const byRank = (a: Role, b: Role): number => b.position - a.position || (a.id < b.id ? -1 : 1);

/** The member's roles (without @everyone), highest first. */
export const rolesOf = (entry: GuildEntry, member: Member): Role[] =>
  member.roles.flatMap((id) => entry.roles[id] ?? []).sort(byRank);

/** The colour of the member's highest role that has one; 0 for none. */
export const memberColor = (entry: GuildEntry, member: Member): number =>
  rolesOf(entry, member).find((r) => r.color > 0)?.color ?? 0;

/** How a user shows in a server: name, role colour and avatar. */
export interface UserDisplay {
  name: string;
  color: number;
  avatarUrl: string | null;
}

/**
 * How `user` shows in server `entry`: a member by their nickname (else
 * display name) in their role colour; someone no longer a member by their
 * display name, uncoloured.
 */
export const userDisplay = (entry: GuildEntry, user: User): UserDisplay => {
  const member = entry.members[user.id];
  if (!member) return { name: user.display_name, color: 0, avatarUrl: user.avatar_url };
  return {
    name: member.nick ?? member.user.display_name,
    color: memberColor(entry, member),
    avatarUrl: member.user.avatar_url,
  };
};
