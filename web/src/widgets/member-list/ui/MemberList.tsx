import { useId, useMemo } from "react";
import { useGuilds } from "@/entities/guild";
import { MemberMenu } from "@/features/manage-member";
import { Avatar, cn, roleTextStyle } from "@/shared/ui";
import { groupMembers, type MemberGroup, type MemberRow } from "../model/groups";

const Row = ({ guildId, member }: { guildId: string; member: MemberRow }) => {
  const offline = member.status === "offline";
  const line = useGuilds((s) => (offline ? undefined : s.statusLines[member.userId]));
  return (
    <li>
      <MemberMenu guildId={guildId} userId={member.userId} mode="dropdown">
        <button
          type="button"
          className="flex h-11 w-full cursor-pointer items-center gap-3 rounded-md px-2 text-left transition-colors duration-(--duration-fast) hover:bg-hover data-[state=open]:bg-selected"
        >
          <Avatar
            name={member.name}
            src={member.avatarUrl}
            size={32}
            status={member.status}
            className={cn(offline && "opacity-45")}
          />
          <span className="min-w-0">
            <span
              data-member-name
              className={cn("block truncate text-base leading-5 font-medium", offline ? "text-fg-faint" : "role-text")}
              style={offline ? undefined : roleTextStyle(member.color)}
            >
              {member.name}
            </span>
            {line !== undefined && (
              <span data-member-line className="block truncate text-xs text-fg-muted">
                {line}
              </span>
            )}
          </span>
        </button>
      </MemberMenu>
    </li>
  );
};

const Group = ({ guildId, group }: { guildId: string; group: MemberGroup }) => {
  const headingId = useId();
  return (
    <section>
      <h3 id={headingId} className="truncate px-2 pt-6 pb-1 text-xs font-semibold text-fg-faint">
        {group.title}
      </h3>
      <ul aria-labelledby={headingId}>
        {group.members.map((m) => (
          <Row key={m.userId} guildId={guildId} member={m} />
        ))}
      </ul>
    </section>
  );
};

/**
 * The members of a server beside the chat (see groupMembers for the order):
 * avatar with presence, then the nickname or display name in the colour of
 * the highest coloured role. Offline members are dimmed and uncoloured.
 * A member opens their card with what the user may do to them.
 */
export const MemberList = ({ guildId }: { guildId: string }) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const presences = useGuilds((s) => s.presences);
  const groups = useMemo(() => (entry ? groupMembers(entry, presences) : []), [entry, presences]);
  return (
    <aside aria-label="Участники" className="min-h-0 flex-1 overflow-y-auto px-2 pb-4">
      {groups.map((g) => (
        <Group key={g.id} guildId={guildId} group={g} />
      ))}
    </aside>
  );
};
