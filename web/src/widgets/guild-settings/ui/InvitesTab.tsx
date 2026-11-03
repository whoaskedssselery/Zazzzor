import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import type { ReactNode } from "react";
import { api, type components, errorText, unwrap } from "@/shared/api";
import { Avatar, IconButton, Skeleton, useToast } from "@/shared/ui";

type Invite = components["schemas"]["Invite"];

const dateFormat = new Intl.DateTimeFormat("ru-RU", {
  day: "numeric",
  month: "long",
  hour: "2-digit",
  minute: "2-digit",
});
export const invitesKey = (guildId: string) => ["invites", guildId] as const;

/** «3 из 10», or «3» for an invite without a limit. */
export const usesText = (invite: Pick<Invite, "uses" | "max_uses">): string =>
  invite.max_uses === 0 ? String(invite.uses) : `${String(invite.uses)} из ${String(invite.max_uses)}`;

/** «до 24 октября в 21:04», or «бессрочно». */
export const expiresText = (expiresAt: string | null): string =>
  expiresAt === null ? "бессрочно" : `до ${dateFormat.format(new Date(expiresAt))}`;

/**
 * The invites of a server: code, channel, who made it, how many used it
 * and until when it works, with a way to delete each. Deleting one stops it
 * at once; nobody is asked, as a new one is a click away.
 */
export const InvitesTab = ({ guildId }: { guildId: string }) => {
  const qc = useQueryClient();
  const toast = useToast();
  const invites = useQuery({
    queryKey: invitesKey(guildId),
    queryFn: async () =>
      unwrap(await api.GET("/guilds/{guild_id}/invites", { params: { path: { guild_id: guildId } } })),
  });
  const remove = useMutation({
    mutationFn: async (code: string) => {
      unwrap(await api.DELETE("/invites/{code}", { params: { path: { code } } }));
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: invitesKey(guildId) });
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });

  let body: ReactNode;
  if (invites.isPending) {
    body = <Skeleton className="h-14 w-full rounded-md" />;
  } else if (invites.isError) {
    body = <p className="text-base text-fg-danger">{errorText(invites.error)}</p>;
  } else if (invites.data.length === 0) {
    body = <p className="text-base text-fg-muted">Приглашений нет.</p>;
  } else {
    body = (
      <table className="w-full text-left text-sm">
        <thead className="text-xs text-fg-faint uppercase">
          <tr>
            <th scope="col" className="py-2 font-semibold">
              Пригласил
            </th>
            <th scope="col" className="py-2 font-semibold">
              Код
            </th>
            <th scope="col" className="py-2 font-semibold">
              Использований
            </th>
            <th scope="col" className="py-2 font-semibold">
              Действует
            </th>
            <th scope="col" className="w-10 py-2">
              <span className="sr-only">Удалить</span>
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {invites.data.map((invite) => (
            <tr key={invite.code}>
              <td className="py-2.5">
                <span className="flex min-w-0 items-center gap-2">
                  <Avatar name={invite.inviter.display_name} src={invite.inviter.avatar_url} size={24} />
                  <bdi className="truncate font-medium">{invite.inviter.display_name}</bdi>
                </span>
              </td>
              <td className="py-2.5">
                <code>{invite.code}</code>
                <span className="block text-xs text-fg-muted">{`#${invite.channel.name}`}</span>
              </td>
              <td className="py-2.5">{usesText(invite)}</td>
              <td className="py-2.5">{expiresText(invite.expires_at)}</td>
              <td className="py-2.5">
                <IconButton
                  size="sm"
                  variant="danger"
                  label={`Удалить приглашение ${invite.code}`}
                  icon={Trash2}
                  disabled={remove.isPending}
                  onClick={() => {
                    remove.mutate(invite.code);
                  }}
                />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    );
  }

  return (
    <section aria-labelledby="invites-title">
      <h2 id="invites-title" className="mb-5 text-xl font-semibold">
        Приглашения
      </h2>
      {body}
    </section>
  );
};
