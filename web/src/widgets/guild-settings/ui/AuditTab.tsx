import { useInfiniteQuery } from "@tanstack/react-query";
import { type ReactNode, useMemo, useState } from "react";
import type { GuildEntry } from "@/entities/guild";
import { api, errorText, unwrap } from "@/shared/api";
import { Avatar, Button, Select, type SelectOption, Skeleton } from "@/shared/ui";
import {
  AUDIT_ACTIONS,
  type AuditEntry,
  type AuditLog,
  type AuditNames,
  actionTitle,
  auditDetail,
  auditNames,
  auditTarget,
  changeText,
} from "../model/audit";

/** Entries per page. */
export const AUDIT_PAGE = 50;

const ANY = "any";

const timeFormat = new Intl.DateTimeFormat("ru-RU", {
  day: "numeric",
  month: "long",
  hour: "2-digit",
  minute: "2-digit",
});

const ACTION_OPTIONS: SelectOption[] = [
  { value: ANY, label: "Все действия" },
  ...AUDIT_ACTIONS.map(([n, title]) => ({ value: String(n), label: title })),
];

const useAuditLog = (guildId: string, actorId: string, action: string) =>
  useInfiniteQuery({
    queryKey: ["audit-log", guildId, actorId, action],
    queryFn: async ({ pageParam }): Promise<AuditLog> =>
      unwrap(
        await api.GET("/guilds/{guild_id}/audit-logs", {
          params: {
            path: { guild_id: guildId },
            query: {
              limit: AUDIT_PAGE,
              ...(actorId === ANY ? {} : { user_id: actorId }),
              ...(action === ANY ? {} : { action_type: Number(action) as AuditEntry["action_type"] }),
              ...(pageParam === null ? {} : { before: pageParam }),
            },
          },
        }),
      ),
    initialPageParam: null as string | null,
    getNextPageParam: (last) =>
      last.audit_log_entries.length === AUDIT_PAGE ? (last.audit_log_entries.at(-1)?.id ?? null) : null,
    // The log only grows by what others do: a fresh look on every open is enough.
    staleTime: 0,
  });

const Entry = ({ e, names, avatar }: { e: AuditEntry; names: AuditNames; avatar: string | null }) => {
  const who = names.user(e.user_id);
  const target = auditTarget(e, names);
  const detail = auditDetail(e, names);
  return (
    <li className="flex gap-3 py-3">
      <Avatar name={who} src={avatar} size={32} />
      <div className="min-w-0 flex-1">
        <p className="text-base break-words">
          <span className="font-semibold">{actionTitle(e.action_type)}</span>
          {target !== null && (
            <>
              {": "}
              <bdi>{target}</bdi>
            </>
          )}
          {detail !== null && <span className="text-fg-muted">{` ${detail}`}</span>}
        </p>
        {e.changes.length > 0 && (
          <ul className="mt-0.5 text-sm break-words text-fg-muted">
            {e.changes.map((c) => (
              <li key={c.key}>{changeText(c, names)}</li>
            ))}
          </ul>
        )}
        {e.reason !== null && <p className="mt-0.5 text-sm break-words text-fg-muted">{`Причина: ${e.reason}`}</p>}
        <p className="mt-0.5 text-xs text-fg-faint">
          <bdi>{who}</bdi>
          {` · ${timeFormat.format(new Date(e.created_at))}`}
        </p>
      </div>
    </li>
  );
};

/**
 * The server's audit log (VIEW_AUDIT_LOG): who kicked, banned, renamed,
 * changed roles, channels, invites and emoji, and which messages moderators
 * deleted, newest first, filtered by who and what, a page at a time.
 */
export const AuditTab = ({ entry }: { entry: GuildEntry }) => {
  const guildId = entry.guild.id;
  const [actorId, setActorId] = useState(ANY);
  const [action, setAction] = useState(ANY);
  const log = useAuditLog(guildId, actorId, action);

  const users = useMemo(
    () => new Map((log.data?.pages ?? []).flatMap((p) => p.users).map((u) => [u.id, u] as const)),
    [log.data],
  );
  const names = useMemo(() => auditNames(entry, users), [entry, users]);
  const actors: SelectOption[] = useMemo(() => {
    const people = Object.values(entry.members)
      .map((m) => ({ value: m.user.id, label: m.nick ?? m.user.display_name }))
      .sort((a, b) => a.label.localeCompare(b.label, "ru"));
    return [{ value: ANY, label: "Все участники" }, ...people];
  }, [entry.members]);

  const entries = log.data?.pages.flatMap((p) => p.audit_log_entries) ?? [];
  let body: ReactNode;
  if (log.isPending) {
    body = <Skeleton className="h-14 w-full rounded-md" />;
  } else if (log.isError) {
    body = <p className="text-base text-fg-danger">{errorText(log.error)}</p>;
  } else if (entries.length === 0) {
    body = (
      <p className="text-base text-fg-muted">
        {actorId === ANY && action === ANY ? "Пока ничего не происходило." : "Ничего не нашлось."}
      </p>
    );
  } else {
    body = (
      <>
        <ul aria-label="Записи журнала" className="flex flex-col divide-y divide-border">
          {entries.map((e) => {
            const id = e.user_id;
            const avatar =
              id === null ? null : (entry.members[id]?.user.avatar_url ?? users.get(id)?.avatar_url ?? null);
            return <Entry key={e.id} e={e} names={names} avatar={avatar} />;
          })}
        </ul>
        {log.hasNextPage && (
          <Button
            variant="secondary"
            className="mt-4"
            loading={log.isFetchingNextPage}
            onClick={() => {
              void log.fetchNextPage();
            }}
          >
            Показать ещё
          </Button>
        )}
      </>
    );
  }

  return (
    <section aria-labelledby="audit-title">
      <h2 id="audit-title" className="mb-2 text-xl font-semibold">
        Журнал действий
      </h2>
      <p className="mb-5 text-sm text-fg-muted">Записи хранятся 90 дней.</p>
      <div className="mb-4 flex flex-wrap gap-2">
        <Select
          aria-label="Кто"
          className="min-w-48 flex-1"
          options={actors}
          value={actorId}
          onValueChange={setActorId}
        />
        <Select
          aria-label="Действие"
          className="min-w-48 flex-1"
          options={ACTION_OPTIONS}
          value={action}
          onValueChange={setAction}
        />
      </div>
      {body}
    </section>
  );
};
