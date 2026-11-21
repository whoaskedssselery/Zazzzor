import { t, textTable } from "@/shared/i18n";
import type { PresenceStatus } from "./reducer";
import { useGuilds } from "./store";

/** What a presence says, as a line under a name. */
export const PRESENCE_LABEL = textTable<PresenceStatus>({
  online: () => t("guild:presence.online"),
  idle: () => t("guild:presence.idle"),
  dnd: () => t("guild:presence.dnd"),
  offline: () => t("guild:presence.offline"),
});

/** Whether someone shows as online (away and do not disturb count). */
export const isShownOnline = (status: PresenceStatus | undefined): boolean =>
  status !== undefined && status !== "offline";

/** A user's presence as the store holds it: offline when unknown, with their status line if any. */
export const usePresence = (userId: string | null | undefined): { status: PresenceStatus; line: string | null } => {
  const status = useGuilds((s) => (userId ? s.presences[userId] : undefined)) ?? "offline";
  const line = useGuilds((s) => (userId ? s.statusLines[userId] : undefined)) ?? null;
  return { status, line };
};
