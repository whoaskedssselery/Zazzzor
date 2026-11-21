import { byRank, type GuildEntry, memberColor, type PresenceStatus, rolesOf } from "@/entities/guild";
import { t } from "@/shared/i18n";

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

const byName = (a: MemberRow, b: MemberRow): number =>
  a.name.localeCompare(b.name, "ru") || (a.userId < b.userId ? -1 : 1);

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
      color: memberColor(entry, member),
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
  if (online.length > 0)
    groups.push({ id: "online", title: t("shell:online", { count: online.length }), members: online });
  if (offline.length > 0)
    groups.push({ id: "offline", title: t("shell:offline", { count: offline.length }), members: offline });
  for (const g of groups) g.members.sort(byName);
  return groups;
}
