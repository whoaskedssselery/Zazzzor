import { useMutation } from "@tanstack/react-query";
import { useNavigateToFallback } from "@/entities/channel";
import { waitForGuild } from "@/entities/guild";
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

/**
 * Joins through an invite and opens its channel: POST /invites/{code}, then
 * the server's GUILD_CREATE is awaited (the store must hold the server
 * before it can be opened), then the channel of the invite, or the nearest
 * visible one, opens. A member already in the server just goes there.
 */
export function useJoinInvite() {
  const open = useNavigateToFallback();
  return useMutation({
    mutationFn: async ({ code, channelId }: { code: string; channelId: string }) => {
      const guild = unwrap(await api.POST("/invites/{code}", { params: { path: { code } } }));
      await waitForGuild(guild.id);
      return { guildId: guild.id, channelId };
    },
    onSuccess: ({ guildId, channelId }) => {
      open(guildId, { prefer: channelId });
    },
  });
}
