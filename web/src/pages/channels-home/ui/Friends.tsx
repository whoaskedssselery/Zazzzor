import { Check, Ellipsis, UserMinus, Users, X } from "lucide-react";
import { type ReactNode, type SyntheticEvent, useMemo, useState } from "react";
import {
  friendLists,
  incomingCount,
  RELATIONSHIP,
  type Relationship,
  requestErrorText,
  useGuilds,
  useRemoveRelationship,
  useRequestFriend,
  useSetRelationship,
} from "@/entities/guild";
import {
  Avatar,
  Badge,
  Button,
  ConfirmDialog,
  cn,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  formatCount,
  IconButton,
  Input,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/shared/ui";
import { NavButton, ShellHeader } from "@/widgets/app-shell";

export type FriendsTab = "online" | "all" | "pending" | "blocked" | "add";

/** What a row says under the name. */
const statusText = (r: Relationship, online: boolean): string => {
  switch (r.type) {
    case RELATIONSHIP.INCOMING:
      return "Входящая заявка";
    case RELATIONSHIP.OUTGOING:
      return "Исходящая заявка";
    case RELATIONSHIP.BLOCKED:
      return "Заблокирован";
    default:
      return online ? "В сети" : "Не в сети";
  }
};

/** A question before an action that cannot be taken back quietly. */
type Ask = { kind: "remove" | "block"; r: Relationship };

/** One person on the friends screen with what can be done about them. */
const Row = ({ r, online, onAsk }: { r: Relationship; online: boolean; onAsk: (a: Ask) => void }) => {
  const set = useSetRelationship();
  const remove = useRemoveRelationship();
  const name = r.user.display_name;
  let actions: ReactNode;
  switch (r.type) {
    case RELATIONSHIP.INCOMING:
      actions = (
        <>
          <IconButton
            label={`Принять заявку от ${name}`}
            icon={Check}
            className="rounded-full bg-control hover:text-fg-success"
            onClick={() => {
              set.mutate({ id: r.id, type: 1 });
            }}
          />
          <IconButton
            label={`Отклонить заявку от ${name}`}
            icon={X}
            className="rounded-full bg-control hover:text-fg-danger"
            onClick={() => {
              remove.mutate(r.id);
            }}
          />
        </>
      );
      break;
    case RELATIONSHIP.OUTGOING:
      actions = (
        <IconButton
          label={`Отменить заявку для ${name}`}
          icon={X}
          className="rounded-full bg-control hover:text-fg-danger"
          onClick={() => {
            remove.mutate(r.id);
          }}
        />
      );
      break;
    case RELATIONSHIP.BLOCKED:
      actions = (
        <Button
          size="sm"
          variant="secondary"
          onClick={() => {
            remove.mutate(r.id);
          }}
        >
          Разблокировать
        </Button>
      );
      break;
    default:
      actions = (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <IconButton label={`Действия с ${name}`} icon={Ellipsis} className="rounded-full bg-control" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              variant="danger"
              onSelect={() => {
                onAsk({ kind: "remove", r });
              }}
            >
              Удалить из друзей
              <UserMinus aria-hidden />
            </DropdownMenuItem>
            <DropdownMenuItem
              variant="danger"
              onSelect={() => {
                onAsk({ kind: "block", r });
              }}
            >
              Заблокировать
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      );
  }
  return (
    <li className="flex items-center gap-3 border-t border-border py-2.5 first:border-t-0">
      <Avatar
        name={name}
        src={r.user.avatar_url}
        size={32}
        {...(r.type === RELATIONSHIP.FRIEND ? { status: online ? "online" : "offline" } : {})}
      />
      <div className="min-w-0 flex-1">
        <p className="flex min-w-0 items-baseline gap-1.5">
          <bdi className="truncate text-base font-semibold">{name}</bdi>
          <span className="truncate text-sm text-fg-faint">{r.user.username}</span>
        </p>
        <p className="truncate text-sm text-fg-muted">{statusText(r, online)}</p>
      </div>
      <div className="flex shrink-0 items-center gap-2">{actions}</div>
    </li>
  );
};

/** One list of the screen under its heading with a count, or what to say when it is empty. */
const List = ({
  title,
  people,
  presences,
  empty,
  onAsk,
}: {
  title: string;
  people: Relationship[];
  presences: Record<string, "online" | "offline">;
  empty: ReactNode;
  onAsk: (a: Ask) => void;
}) =>
  people.length === 0 ? (
    <div className="flex flex-1 flex-col items-center justify-center gap-4 px-6 py-16 text-center text-base text-fg-muted">
      {empty}
    </div>
  ) : (
    <section aria-label={title} className="px-6 pt-4">
      <h2 className="pb-2 text-xs font-semibold text-fg-faint uppercase">{`${title} — ${String(people.length)}`}</h2>
      <ul>
        {people.map((r) => (
          <Row key={r.id} r={r} online={presences[r.id] === "online"} onAsk={onAsk} />
        ))}
      </ul>
    </section>
  );

/**
 * A friend request by username. The server answers alike whether or not
 * the request really went out (a block stays secret), so a success says
 * only that it was sent.
 */
const AddFriend = () => {
  const request = useRequestFriend();
  const [username, setUsername] = useState("");
  const [sent, setSent] = useState<string | null>(null);
  const name = username.trim().replace(/^@/, "");
  const submit = (e: SyntheticEvent) => {
    e.preventDefault();
    if (name === "") return;
    setSent(null);
    request.mutate(name, {
      onSuccess: () => {
        setSent(name);
        setUsername("");
      },
    });
  };
  return (
    <section aria-labelledby="add-friend-title" className="border-b border-border px-6 py-5">
      <h2 id="add-friend-title" className="text-base font-semibold">
        Добавить в друзья
      </h2>
      <p className="mt-1 text-sm text-fg-muted">Введите имя пользователя: то, под которым друг входит в Zazzzor.</p>
      <form
        noValidate
        onSubmit={submit}
        className={cn(
          "mt-4 flex items-center gap-2 rounded-lg bg-input p-1.5 pl-3 ring-1 ring-transparent focus-within:ring-focus max-sm:flex-col max-sm:items-stretch max-sm:p-3",
          request.isError && "ring-danger",
        )}
      >
        <Input
          value={username}
          aria-label="Имя пользователя"
          placeholder="Имя пользователя"
          autoComplete="off"
          spellCheck={false}
          maxLength={64}
          className="h-9 flex-1 bg-transparent px-0 focus-visible:ring-0"
          onChange={(e) => {
            setUsername(e.currentTarget.value);
            if (request.isError) request.reset();
            setSent(null);
          }}
        />
        <Button type="submit" size="sm" disabled={name === "" || request.isPending}>
          Отправить заявку
        </Button>
      </form>
      <p aria-live="polite" className="mt-2 min-h-5 text-sm">
        {request.isError && <span className="text-fg-danger">{requestErrorText(request.error)}</span>}
        {sent !== null && <span className="text-fg-success">{`Заявка для ${sent} отправлена.`}</span>}
      </p>
    </section>
  );
};

/**
 * Home's main area, as Discord's: the user's friends (online first by
 * default), the requests that wait, the people they blocked, and a way
 * to add a friend by username. Everything follows the gateway's
 * relationship events.
 */
export const FriendsView = () => {
  const relationships = useGuilds((s) => s.relationships);
  const presences = useGuilds((s) => s.presences);
  const lists = useMemo(() => friendLists({ relationships, presences }), [relationships, presences]);
  const incoming = incomingCount({ relationships });
  const [tab, setTab] = useState<FriendsTab>("online");
  const [ask, setAsk] = useState<Ask | null>(null);
  const set = useSetRelationship();
  const remove = useRemoveRelationship();

  const goAdd = (
    <Button
      size="sm"
      onClick={() => {
        setTab("add");
      }}
    >
      Добавить в друзья
    </Button>
  );

  return (
    <Tabs
      value={tab}
      onValueChange={(v) => {
        setTab(v as FriendsTab);
      }}
      className="flex min-h-0 flex-1 flex-col"
    >
      <ShellHeader>
        <NavButton />
        <span className="flex shrink-0 items-center gap-2 font-semibold">
          <Users aria-hidden className="size-5 text-fg-muted" />
          <h1 className="text-base max-sm:sr-only">Друзья</h1>
        </span>
        <span aria-hidden className="mx-2 h-6 w-px shrink-0 bg-border max-sm:hidden" />
        <TabsList aria-label="Друзья" className="min-w-0 overflow-x-auto p-1 [scrollbar-width:none]">
          <TabsTrigger value="online">В сети</TabsTrigger>
          <TabsTrigger value="all">Все</TabsTrigger>
          <TabsTrigger
            value="pending"
            aria-label={incoming > 0 ? `Ожидание, входящих заявок: ${String(incoming)}` : "Ожидание"}
          >
            Ожидание
            {incoming > 0 && (
              <Badge variant="count" className="ml-1.5">
                {formatCount(incoming)}
              </Badge>
            )}
          </TabsTrigger>
          <TabsTrigger value="blocked">Заблокированные</TabsTrigger>
          <TabsTrigger
            value="add"
            className="bg-accent text-on-accent hover:bg-accent-hover hover:text-on-accent data-[state=active]:bg-transparent data-[state=active]:text-fg-success"
          >
            Добавить в друзья
          </TabsTrigger>
        </TabsList>
      </ShellHeader>
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        <TabsContent value="online" className="flex flex-1 flex-col">
          <List
            title="В сети"
            people={lists.online}
            presences={presences}
            onAsk={setAsk}
            empty={
              <>
                <p>Никого из друзей сейчас нет в сети.</p>
                {lists.all.length === 0 && goAdd}
              </>
            }
          />
        </TabsContent>
        <TabsContent value="all" className="flex flex-1 flex-col">
          <List
            title="Все друзья"
            people={lists.all}
            presences={presences}
            onAsk={setAsk}
            empty={
              <>
                <p>Друзей пока нет. Добавьте кого-нибудь по имени пользователя.</p>
                {goAdd}
              </>
            }
          />
        </TabsContent>
        <TabsContent value="pending" className="flex flex-1 flex-col">
          <List
            title="Ожидание"
            people={lists.pending}
            presences={presences}
            onAsk={setAsk}
            empty={<p>Заявок нет.</p>}
          />
        </TabsContent>
        <TabsContent value="blocked" className="flex flex-1 flex-col">
          <List
            title="Заблокированные"
            people={lists.blocked}
            presences={presences}
            onAsk={setAsk}
            empty={<p>Вы никого не заблокировали.</p>}
          />
        </TabsContent>
        <TabsContent value="add">
          <AddFriend />
        </TabsContent>
      </div>
      <ConfirmDialog
        open={ask !== null}
        onOpenChange={(open) => {
          if (!open) setAsk(null);
        }}
        title={
          ask?.kind === "block"
            ? `Заблокировать ${ask.r.user.display_name}?`
            : `Удалить ${ask?.r.user.display_name ?? ""} из друзей?`
        }
        description={
          ask?.kind === "block"
            ? "Дружба закончится, а новые заявки от этого человека перестанут приходить. Он об этом не узнает."
            : "Вы перестанете видеть друг друга в списках друзей. Можно будет снова отправить заявку."
        }
        confirmLabel={ask?.kind === "block" ? "Заблокировать" : "Удалить из друзей"}
        tone="danger"
        pending={set.isPending || remove.isPending}
        onConfirm={() => {
          if (!ask) return;
          const done = {
            onSettled: () => {
              setAsk(null);
            },
          };
          if (ask.kind === "block") set.mutate({ id: ask.r.id, type: 2 }, done);
          else remove.mutate(ask.r.id, done);
        }}
      />
    </Tabs>
  );
};
