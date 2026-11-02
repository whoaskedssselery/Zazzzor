import type { GuildEntry, Member, PresenceStatus, Role } from "@/entities/guild";

/** One row of the member list. */
export interface MemberRow {
  userId: string;
  /** The nickname in this server, else the display name. */
  name: string;
  avatarUrl: string | null;
  status: PresenceStatus;
  /** The colour of the highest role that has one; 0 for none. */
  color: number;
}

export interface MemberGroup {
  /** The role ID of a hoisted group, or "online" / "offline". */
  id: string;
  /** «Админ — 2», «В сети — 5», «Не в сети — 3». */
  title: string;
  members: MemberRow[];
}

/** Higher position first; equal positions (not expected) by ID, so the order is stable. */
const byRank = (a: Role, b: Role): number => b.position - a.position || (a.id < b.id ? -1 : 1);

const byName = (a: MemberRow, b: MemberRow): number =>
  a.name.localeCompare(b.name, "ru") || (a.userId < b.userId ? -1 : 1);

/** The member's roles (without @everyone), highest first. */
const rolesOf = (entry: GuildEntry, member: Member): Role[] =>
  member.roles.flatMap((id) => entry.roles[id] ?? []).sort(byRank);

/**
 * The member list of a server, as Discord groups it: the online members
 * under their highest hoisted role (roles by position, highest first), then
 * the other online members as «В сети», then everyone offline as «Не в
 * сети». Empty groups are left out; names sort alphabetically within a group.
 */
export function groupMembers(entry: GuildEntry, presences: Record<string, PresenceStatus>): MemberGroup[] {
  const hoisted = Object.values(entry.roles)
    .filter((r) => r.hoist && r.id !== entry.guild.id)
    .sort(byRank);
  const byRole = new Map<string, MemberRow[]>(hoisted.map((r) => [r.id, []]));
  const online: MemberRow[] = [];
  const offline: MemberRow[] = [];

  for (const member of Object.values(entry.members)) {
    const roles = rolesOf(entry, member);
    const row: MemberRow = {
      userId: member.user.id,
      name: member.nick ?? member.user.display_name,
      avatarUrl: member.user.avatar_url,
      status: presences[member.user.id] ?? "offline",
      color: roles.find((r) => r.color > 0)?.color ?? 0,
    };
    if (row.status === "offline") {
      offline.push(row);
      continue;
    }
    const top = roles.find((r) => r.hoist);
    const bucket = top ? byRole.get(top.id) : undefined;
    if (bucket) bucket.push(row);
    else online.push(row);
  }

  const groups: MemberGroup[] = hoisted.flatMap((r) => {
    const members = byRole.get(r.id) ?? [];
    return members.length > 0 ? [{ id: r.id, title: `${r.name} — ${members.length}`, members }] : [];
  });
  if (online.length > 0) groups.push({ id: "online", title: `В сети — ${online.length}`, members: online });
  if (offline.length > 0) groups.push({ id: "offline", title: `Не в сети — ${offline.length}`, members: offline });
  for (const g of groups) g.members.sort(byName);
  return groups;
}
