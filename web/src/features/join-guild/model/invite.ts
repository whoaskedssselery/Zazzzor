import { useMutation } from "@tanstack/react-query";
import { useOpenGuild } from "@/entities/channel";
import { ApiError, api, type components, errorText, unwrap } from "@/shared/api";

export type Invite = components["schemas"]["Invite"];

/** An invite code of the contract: 8 base62 characters. */
const CODE = /^[0-9A-Za-z]{8}$/;

/**
 * The invite code in what the user pasted: the bare code, or a link
 * `http(s)://<any host>/invite/<code>` (a trailing slash, a query or a hash
 * are fine). The link is read with the URL parser, so only its path decides;
 * null for anything else.
 */
export function parseInviteCode(input: string): string | null {
  const text = input.trim();
  if (CODE.test(text)) return text;
  let url: URL;
  try {
    url = new URL(text);
  } catch {
    return null;
  }
  if (url.protocol !== "https:" && url.protocol !== "http:") return null;
  const parts = url.pathname.split("/").filter(Boolean);
  const [first, code] = parts;
  return parts.length === 2 && first === "invite" && code !== undefined && CODE.test(code) ? code : null;
}

export const INVALID_INVITE = "Приглашение недействительно";
export const BANNED = "Вы заблокированы на этом сервере";

/** Why an invite cannot be used, in the invite's own words; anything else as the common texts say it. */
export function inviteErrorText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.code === "banned") return BANNED;
    if (e.status === 404 || e.status === 410) return INVALID_INVITE;
  }
  return errorText(e);
}

/** GET /invites/{code}: where the invite leads. */
export async function previewInvite(code: string): Promise<Invite> {
  return unwrap(await api.GET("/invites/{code}", { params: { path: { code } } }));
}

/** Shown when the joined server has not arrived over the gateway in time. */
export const JOINED_LATE =
  "Вы вступили на сервер, но связь прервалась. Он появится в списке серверов, когда подключение восстановится.";

/**
 * Joins through an invite and opens its channel: POST /invites/{code}, then
 * the server opens on the invite's channel (or the nearest visible one) once
 * its GUILD_CREATE is in the store (see useOpenGuild). `late` says the server
 * is overdue; `onOpen` runs right before it opens (a dialog closes itself).
 */
export function useJoinInvite(onOpen?: () => void) {
  const arrival = useOpenGuild(onOpen);
  const join = useMutation({
    mutationFn: async ({ code }: { code: string; channelId: string }) =>
      unwrap(await api.POST("/invites/{code}", { params: { path: { code } } })),
    onSuccess: (guild, { channelId }) => {
      arrival.open(guild.id, channelId);
    },
  });
  return {
    join: join.mutate,
    pending: join.isPending || (arrival.waiting && !arrival.late),
    error: join.error,
    late: arrival.late,
  };
}
