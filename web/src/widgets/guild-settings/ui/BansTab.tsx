import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { api, type components, errorText, unwrap, useGatewayEvent } from "@/shared/api";
import { Avatar, Button, ConfirmDialog, Skeleton, useToast } from "@/shared/ui";

type Ban = components["schemas"]["Ban"];

const dateFormat = new Intl.DateTimeFormat("ru-RU", { day: "numeric", month: "long", year: "numeric" });
export const bansKey = (guildId: string) => ["bans", guildId] as const;

/** The guild a GUILD_BAN_ADD or _REMOVE is about, without trusting the rest of it. */
const guildOf = (d: unknown): string | null =>
  typeof d === "object" && d !== null && "guild_id" in d && typeof d.guild_id === "string" ? d.guild_id : null;

/**
 * The bans of a server, oldest first, with their reason and date, and a way
 * to lift each (after asking). The list follows GUILD_BAN_ADD and _REMOVE,
 * which reach only the members who may ban.
 */
export const BansTab = ({ guildId }: { guildId: string }) => {
  const qc = useQueryClient();
  const toast = useToast();
  const [lifting, setLifting] = useState<Ban | null>(null);
  const bans = useQuery({
    queryKey: bansKey(guildId),
    queryFn: async () => unwrap(await api.GET("/guilds/{guild_id}/bans", { params: { path: { guild_id: guildId } } })),
  });
  const refresh = (d: unknown) => {
    if (guildOf(d) === guildId) void qc.invalidateQueries({ queryKey: bansKey(guildId) });
  };
  useGatewayEvent("GUILD_BAN_ADD", refresh);
  useGatewayEvent("GUILD_BAN_REMOVE", refresh);
  const unban = useMutation({
    mutationFn: async (userId: string) => {
      unwrap(
        await api.DELETE("/guilds/{guild_id}/bans/{user_id}", {
          params: { path: { guild_id: guildId, user_id: userId } },
        }),
      );
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: bansKey(guildId) });
      toast.show({ text: "Бан снят" });
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });

  let body: ReactNode;
  if (bans.isPending) {
    body = <Skeleton className="h-14 w-full rounded-md" />;
  } else if (bans.isError) {
    body = <p className="text-base text-fg-danger">{errorText(bans.error)}</p>;
  } else if (bans.data.length === 0) {
    body = <p className="text-base text-fg-muted">Никто не забанен.</p>;
  } else {
    body = (
      <ul aria-label="Забаненные" className="flex flex-col divide-y divide-border">
        {bans.data.map((ban) => {
          const user = ban.user;
          if (user === null) return null;
          return (
            <li key={user.id} className="flex items-center gap-3 py-2.5">
              <Avatar name={user.display_name} src={user.avatar_url} size={40} />
              <div className="min-w-0 flex-1">
                <p className="flex min-w-0 items-baseline gap-2">
                  <bdi className="truncate text-base font-semibold">{user.display_name}</bdi>
                  <span className="truncate text-sm text-fg-muted">@{user.username}</span>
                </p>
                <p className="text-sm break-words text-fg-muted">{ban.reason ?? "Без причины"}</p>
                <p className="text-xs text-fg-faint">{`Забанен ${dateFormat.format(new Date(ban.created_at))}`}</p>
              </div>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => {
                  setLifting(ban);
                }}
              >
                Разбанить
              </Button>
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <section aria-labelledby="bans-title">
      <h2 id="bans-title" className="mb-5 text-xl font-semibold">
        Баны
      </h2>
      {body}
      <ConfirmDialog
        open={lifting !== null}
        onOpenChange={(open) => {
          if (!open) setLifting(null);
        }}
        title={`Разбанить ${lifting?.user?.display_name ?? ""}?`}
        description="Он сможет вернуться на сервер по приглашению."
        confirmLabel="Разбанить"
        onConfirm={() => {
          const id = lifting?.user?.id;
          if (id !== undefined) unban.mutate(id);
          setLifting(null);
        }}
      />
    </section>
  );
};
