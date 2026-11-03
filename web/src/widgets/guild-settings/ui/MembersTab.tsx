import { Search } from "lucide-react";
import { useState } from "react";
import { type GuildEntry, rolesOf, userDisplay } from "@/entities/guild";
import { MemberMenu } from "@/features/manage-member";
import { Avatar, Input, RoleColorDot, roleTextStyle } from "@/shared/ui";

const joinedFormat = new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "long", year: "numeric" });

/**
 * The members of a server with what the user may do to each (the «…»
 * menu: nickname, roles, kick, ban, as their permissions and rank allow).
 * The search matches the nickname, the display name and the login.
 */
export const MembersTab = ({ entry }: { entry: GuildEntry }) => {
  const [query, setQuery] = useState("");
  const q = query.trim().toLocaleLowerCase("ru");
  const members = Object.values(entry.members)
    .map((m) => ({ member: m, display: userDisplay(entry, m.user) }))
    .filter(
      ({ member, display }) =>
        q === "" ||
        [display.name, member.user.display_name, member.user.username].some((s) =>
          s.toLocaleLowerCase("ru").includes(q),
        ),
    )
    .sort((a, b) => a.display.name.localeCompare(b.display.name, "ru"));

  return (
    <section aria-labelledby="members-title">
      <h2 id="members-title" className="mb-5 text-xl font-semibold">
        Участники
      </h2>
      <div className="relative mb-4">
        <Search
          aria-hidden
          className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-fg-faint"
        />
        <Input
          type="search"
          aria-label="Поиск участников"
          placeholder="Поиск по имени"
          className="pl-9"
          value={query}
          onChange={(e) => {
            setQuery(e.currentTarget.value);
          }}
        />
      </div>
      {members.length === 0 ? (
        <p className="text-base text-fg-muted">Никого не нашлось.</p>
      ) : (
        <ul aria-label="Участники сервера" className="flex flex-col divide-y divide-border">
          {members.map(({ member, display }) => {
            const roles = rolesOf(entry, member);
            return (
              <li key={member.user.id} className="flex items-center gap-3 py-2.5">
                <Avatar name={display.name} src={display.avatarUrl} size={40} />
                <div className="min-w-0 flex-1">
                  <p className="flex min-w-0 items-baseline gap-2">
                    <bdi className="role-text truncate text-base font-semibold" style={roleTextStyle(display.color)}>
                      {display.name}
                    </bdi>
                    <span className="truncate text-sm text-fg-muted">@{member.user.username}</span>
                    {member.user.id === entry.guild.owner_id && (
                      <span className="shrink-0 text-xs text-fg-warning">владелец</span>
                    )}
                  </p>
                  <p className="text-xs text-fg-faint">{`На сервере с ${joinedFormat.format(new Date(member.joined_at))}`}</p>
                  {roles.length > 0 && (
                    <ul aria-label="Роли" className="mt-1 flex flex-wrap gap-1">
                      {roles.map((r) => (
                        <li
                          key={r.id}
                          className="flex items-center gap-1 rounded-sm bg-control px-1.5 py-0.5 text-xs font-medium"
                        >
                          <RoleColorDot color={r.color} size={8} />
                          {r.name}
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
                <MemberMenu guildId={entry.guild.id} userId={member.user.id} />
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
};
