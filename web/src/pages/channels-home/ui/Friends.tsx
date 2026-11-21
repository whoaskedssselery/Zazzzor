import { useNavigate } from "@tanstack/react-router";
import { Check, Ellipsis, MessageCircle, UserMinus, Users, X } from "lucide-react";
import { type ReactNode, type SyntheticEvent, useMemo, useState } from "react";
import {
  friendLists,
  incomingCount,
  PRESENCE_LABEL,
  type PresenceStatus,
  RELATIONSHIP,
  type Relationship,
  requestErrorText,
  useGuilds,
  useOpenDirect,
  usePresence,
  useRemoveRelationship,
  useRequestFriend,
  useSetRelationship,
} from "@/entities/guild";
import { t } from "@/shared/i18n";
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
const statusText = (r: Relationship, status: PresenceStatus, line: string | null): string => {
  switch (r.type) {
    case RELATIONSHIP.INCOMING:
      return t("friends:incoming");
    case RELATIONSHIP.OUTGOING:
      return t("friends:outgoing");
    case RELATIONSHIP.BLOCKED:
      return t("friends:blocked");
    default:
      return status !== "offline" && line !== null ? line : PRESENCE_LABEL[status];
  }
};

/** A question before an action that cannot be taken back quietly. */
type Ask = { kind: "remove" | "block"; r: Relationship };

/** One person on the friends screen with what can be done about them. */
const Row = ({ r, onAsk }: { r: Relationship; onAsk: (a: Ask) => void }) => {
  const { status, line } = usePresence(r.id);
  const set = useSetRelationship();
  const remove = useRemoveRelationship();
  const open = useOpenDirect();
  const navigate = useNavigate();
  const name = r.user.display_name;
  let actions: ReactNode;
  switch (r.type) {
    case RELATIONSHIP.INCOMING:
      actions = (
        <>
          <IconButton
            label={t("friends:acceptFrom", { name })}
            icon={Check}
            className="rounded-full bg-control hover:text-fg-success"
            onClick={() => {
              set.mutate({ id: r.id, type: 1 });
            }}
          />
          <IconButton
            label={t("friends:declineFrom", { name })}
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
          label={t("friends:cancelFor", { name })}
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
          {t("friends:unblock")}
        </Button>
      );
      break;
    default:
      actions = (
        <>
          <IconButton
            label={t("friends:messageTo", { name })}
            icon={MessageCircle}
            className="rounded-full bg-control"
            disabled={open.isPending}
            onClick={() => {
              open.mutate(r.id, {
                onSuccess: (channel) => {
                  void navigate({
                    to: "/channels/@me/$channelId",
                    params: { channelId: channel.id },
                  });
                },
              });
            }}
          />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <IconButton
                label={t("friends:actionsWith", { name })}
                icon={Ellipsis}
                className="rounded-full bg-control"
              />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem
                variant="danger"
                onSelect={() => {
                  onAsk({ kind: "remove", r });
                }}
              >
                {t("friends:removeFriend")}
                <UserMinus aria-hidden />
              </DropdownMenuItem>
              <DropdownMenuItem
                variant="danger"
                onSelect={() => {
                  onAsk({ kind: "block", r });
                }}
              >
                {t("friends:block")}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </>
      );
  }
  return (
    <li className="flex items-center gap-3 border-t border-border py-2.5 first:border-t-0">
      <Avatar name={name} src={r.user.avatar_url} size={32} {...(r.type === RELATIONSHIP.FRIEND ? { status } : {})} />
      <div className="min-w-0 flex-1">
        <p className="flex min-w-0 items-baseline gap-1.5">
          <bdi className="truncate text-base font-semibold">{name}</bdi>
          <span className="truncate text-sm text-fg-faint">{r.user.username}</span>
        </p>
        <p className="truncate text-sm text-fg-muted">{statusText(r, status, line)}</p>
      </div>
      <div className="flex shrink-0 items-center gap-2">{actions}</div>
    </li>
  );
};

/** One list of the screen under its heading with a count, or what to say when it is empty. */
const List = ({
  title,
  people,
  empty,
  onAsk,
}: {
  title: string;
  people: Relationship[];
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
          <Row key={r.id} r={r} onAsk={onAsk} />
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
        {t("friends:addAsFriend")}
      </h2>
      <p className="mt-1 text-sm text-fg-muted">{t("friends:addHint")}</p>
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
          aria-label={t("friends:username")}
          placeholder={t("friends:username")}
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
          {t("friends:sendRequest")}
        </Button>
      </form>
      <p aria-live="polite" className="mt-2 min-h-5 text-sm">
        {request.isError && <span className="text-fg-danger">{requestErrorText(request.error)}</span>}
        {sent !== null && <span className="text-fg-success">{t("friends:requestSentTo", { name: sent })}</span>}
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
      {t("friends:addAsFriend")}
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
          <h1 className="text-base max-sm:sr-only">{t("friends:friends")}</h1>
        </span>
        <span aria-hidden className="mx-2 h-6 w-px shrink-0 bg-border max-sm:hidden" />
        <TabsList aria-label={t("friends:friends")} className="min-w-0 overflow-x-auto p-1 [scrollbar-width:none]">
          <TabsTrigger value="online">{t("friends:online")}</TabsTrigger>
          <TabsTrigger value="all">{t("friends:all")}</TabsTrigger>
          <TabsTrigger
            value="pending"
            aria-label={incoming > 0 ? t("friends:pendingCount", { count: incoming }) : t("friends:pending")}
          >
            {t("friends:pending")}
            {incoming > 0 && (
              <Badge variant="count" className="ml-1.5">
                {formatCount(incoming)}
              </Badge>
            )}
          </TabsTrigger>
          <TabsTrigger value="blocked">{t("friends:blockedTab")}</TabsTrigger>
          <TabsTrigger
            value="add"
            className="bg-accent text-on-accent hover:bg-accent-hover hover:text-on-accent data-[state=active]:bg-transparent data-[state=active]:text-fg-success"
          >
            {t("friends:addAsFriend")}
          </TabsTrigger>
        </TabsList>
      </ShellHeader>
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        <TabsContent value="online" className="flex flex-1 flex-col">
          <List
            title={t("friends:online")}
            people={lists.online}
            onAsk={setAsk}
            empty={
              <>
                <p>{t("friends:noneOnline")}</p>
                {lists.all.length === 0 && goAdd}
              </>
            }
          />
        </TabsContent>
        <TabsContent value="all" className="flex flex-1 flex-col">
          <List
            title={t("friends:allFriends")}
            people={lists.all}
            onAsk={setAsk}
            empty={
              <>
                <p>{t("friends:noFriendsYet")}</p>
                {goAdd}
              </>
            }
          />
        </TabsContent>
        <TabsContent value="pending" className="flex flex-1 flex-col">
          <List
            title={t("friends:pending")}
            people={lists.pending}
            onAsk={setAsk}
            empty={<p>{t("friends:noRequests")}</p>}
          />
        </TabsContent>
        <TabsContent value="blocked" className="flex flex-1 flex-col">
          <List
            title={t("friends:blockedTab")}
            people={lists.blocked}
            onAsk={setAsk}
            empty={<p>{t("friends:noneBlocked")}</p>}
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
            ? t("friends:blockTitle", { name: ask.r.user.display_name })
            : t("friends:removeTitle", { name: ask?.r.user.display_name ?? "" })
        }
        description={ask?.kind === "block" ? t("friends:blockText") : t("friends:removeText")}
        confirmLabel={ask?.kind === "block" ? t("friends:block") : t("friends:removeFriend")}
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
