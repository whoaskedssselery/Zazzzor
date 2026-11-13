import { useNavigate } from "@tanstack/react-router";
import { Search } from "lucide-react";
import { type SubmitEvent, useMemo, useState } from "react";
import { useGuilds, userDisplay } from "@/entities/guild";
import { requestFocus } from "@/entities/message";
import { errorText } from "@/shared/api";
import { formatMessageTime, markdownText, snowflakeTime } from "@/shared/lib";
import {
  Avatar,
  Button,
  IconButton,
  Input,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Select,
  type SelectOption,
  Spinner,
} from "@/shared/ui";
import { canSearch, type FoundMessage, foundText, type SearchAsk, type SearchScope, useSearch } from "../model/search";

const ANY = "any";

/** Who wrote a result and where, as the scope knows them. */
interface Names {
  author: (m: FoundMessage) => { name: string; avatarUrl: string | null };
  channel: (channelId: string) => string | null;
}

const useNames = (scope: SearchScope): Names => {
  const guildId = scope.kind === "guild" ? scope.guildId : null;
  const entry = useGuilds((s) => (guildId === null ? undefined : s.guilds[guildId]));
  return useMemo(
    () => ({
      author: (m) => {
        if (m.author === null) return { name: "Удалённый пользователь", avatarUrl: null };
        if (!entry) return { name: m.author.display_name, avatarUrl: m.author.avatar_url };
        const d = userDisplay(entry, m.author);
        return { name: d.name, avatarUrl: d.avatarUrl };
      },
      channel: (id) => (entry ? (entry.channels[id]?.name ?? null) : null),
    }),
    [entry],
  );
};

/** The authors and channels to filter by: the server's members and text channels, or a group's people. */
const useFilters = (scope: SearchScope) => {
  const guildId = scope.kind === "guild" ? scope.guildId : null;
  const channelId = scope.kind === "channel" ? scope.channelId : null;
  const entry = useGuilds((s) => (guildId === null ? undefined : s.guilds[guildId]));
  const priv = useGuilds((s) => (channelId === null ? undefined : s.privates[channelId]?.channel));
  const me = useGuilds((s) => s.me);
  return useMemo(() => {
    const people = entry
      ? Object.values(entry.members).map((m) => ({ value: m.user.id, label: m.nick ?? m.user.display_name }))
      : [...(me ? [me] : []), ...(priv?.recipients ?? [])].map((u) => ({ value: u.id, label: u.display_name }));
    people.sort((a, b) => a.label.localeCompare(b.label, "ru"));
    const channels = entry
      ? Object.values(entry.channels)
          .filter((c) => c.type === 0)
          .sort((a, b) => a.position - b.position)
          .map((c) => ({ value: c.id, label: `#${c.name}` }))
      : [];
    const authors: SelectOption[] = [{ value: ANY, label: "От кого угодно" }, ...people];
    const where: SelectOption[] = [{ value: ANY, label: "Во всех каналах" }, ...channels];
    return { authors, where: entry ? where : null };
  }, [entry, priv, me]);
};

const Result = ({ m, names, onOpen }: { m: FoundMessage; names: Names; onOpen: (m: FoundMessage) => void }) => {
  const who = names.author(m);
  const where = names.channel(m.channel_id);
  const text = markdownText(m.content) || (m.attachments.length > 0 ? "📎 Вложение" : "");
  return (
    <li>
      <button
        type="button"
        className="flex w-full cursor-pointer gap-3 rounded-md px-2 py-2 text-left transition-colors duration-(--duration-fast) hover:bg-hover"
        onClick={() => {
          onOpen(m);
        }}
      >
        <Avatar name={who.name} src={who.avatarUrl} size={32} />
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-baseline gap-2">
            <bdi className="truncate text-sm font-semibold">{who.name}</bdi>
            {where !== null && <span className="truncate text-xs text-fg-muted">#{where}</span>}
            <time className="ml-auto shrink-0 text-xs text-fg-faint" dateTime={snowflakeTime(m.id).toISOString()}>
              {formatMessageTime(snowflakeTime(m.id), new Date())}
            </time>
          </span>
          <span className="mt-0.5 line-clamp-3 block text-sm break-words whitespace-pre-line text-fg">{text}</span>
        </span>
      </button>
    </li>
  );
};

/**
 * «Поиск» in a channel header: words, who wrote it and (in a server) which
 * channel; results newest first, a page at a time. A result opens its
 * channel at that message.
 */
export const SearchButton = ({ scope, placeholder }: { scope: SearchScope; placeholder: string }) => {
  const [open, setOpen] = useState(false);
  const [content, setContent] = useState("");
  const [authorId, setAuthorId] = useState("");
  const [channelId, setChannelId] = useState("");
  const [ask, setAsk] = useState<SearchAsk | null>(null);
  const results = useSearch(scope, ask);
  const names = useNames(scope);
  const filters = useFilters(scope);
  const navigate = useNavigate();

  const draft: SearchAsk = { content, authorId, channelId };
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (canSearch(draft)) setAsk(draft);
  };
  const openResult = (m: FoundMessage) => {
    requestFocus(m.channel_id, m.id);
    setOpen(false);
    void (scope.kind === "guild"
      ? navigate({ to: "/channels/$guildId/$channelId", params: { guildId: scope.guildId, channelId: m.channel_id } })
      : navigate({ to: "/channels/@me/$channelId", params: { channelId: m.channel_id } }));
  };

  const found = results.data?.pages.flatMap((p) => p.messages) ?? [];
  const total = results.data?.pages[0]?.total_results;
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <IconButton label="Поиск по сообщениям" icon={Search} pressed={open} tooltipSide="bottom" />
      </PopoverTrigger>
      <PopoverContent
        align="end"
        aria-label="Поиск по сообщениям"
        className="flex max-h-[min(36rem,80vh)] w-[min(28rem,calc(100vw-1rem))] flex-col"
      >
        <search className="border-b border-border">
          <form className="flex flex-col gap-2 p-3" onSubmit={submit}>
            <div className="flex gap-2">
              <Input
                type="search"
                aria-label="Что искать"
                placeholder={placeholder}
                value={content}
                maxLength={200}
                autoFocus
                onChange={(e) => {
                  setContent(e.currentTarget.value);
                }}
              />
              <Button
                type="submit"
                disabled={!canSearch(draft)}
                loading={results.isFetching && !results.isFetchingNextPage}
              >
                Найти
              </Button>
            </div>
            <div className="flex gap-2">
              <Select
                aria-label="Автор"
                className="min-w-0 flex-1"
                options={filters.authors}
                value={authorId === "" ? ANY : authorId}
                onValueChange={(v) => {
                  setAuthorId(v === ANY ? "" : v);
                }}
              />
              {filters.where && (
                <Select
                  aria-label="Канал"
                  className="min-w-0 flex-1"
                  options={filters.where}
                  value={channelId === "" ? ANY : channelId}
                  onValueChange={(v) => {
                    setChannelId(v === ANY ? "" : v);
                  }}
                />
              )}
            </div>
          </form>
        </search>
        <div className="min-h-0 flex-1 overflow-y-auto p-2" aria-live="polite">
          {ask === null ? (
            <p className="px-2 py-6 text-center text-sm text-fg-muted">
              Слова находятся в любой форме и по началу: «кин» найдёт «кино».
            </p>
          ) : results.isError ? (
            <p role="alert" className="px-2 py-6 text-center text-sm text-fg-danger">
              {errorText(results.error)}
            </p>
          ) : results.isPending ? (
            <div className="flex justify-center py-6">
              <Spinner label="Ищем…" />
            </div>
          ) : (
            <>
              <p data-search-total className="px-2 pb-1 text-xs font-semibold text-fg-faint uppercase">
                {foundText(total ?? 0)}
              </p>
              <ul aria-label="Результаты поиска">
                {found.map((m) => (
                  <Result key={m.id} m={m} names={names} onOpen={openResult} />
                ))}
              </ul>
              {results.hasNextPage && (
                <Button
                  variant="secondary"
                  className="mx-2 mt-2"
                  loading={results.isFetchingNextPage}
                  onClick={() => {
                    void results.fetchNextPage();
                  }}
                >
                  Показать ещё
                </Button>
              )}
            </>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
};
