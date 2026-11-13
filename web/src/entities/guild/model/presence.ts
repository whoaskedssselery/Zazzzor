import type { PresenceStatus } from "./reducer";
import { useGuilds } from "./store";

/** What a presence says, as a line under a name. */
export const PRESENCE_LABEL: Record<PresenceStatus, string> = {
  online: "В сети",
  idle: "Нет на месте",
  dnd: "Не беспокоить",
  offline: "Не в сети",
};

/** Whether someone shows as online (away and do not disturb count). */
export const isShownOnline = (status: PresenceStatus | undefined): boolean =>
  status !== undefined && status !== "offline";

/** A user's presence as the store holds it: offline when unknown, with their status line if any. */
export const usePresence = (userId: string | null | undefined): { status: PresenceStatus; line: string | null } => {
  const status = useGuilds((s) => (userId ? s.presences[userId] : undefined)) ?? "offline";
  const line = useGuilds((s) => (userId ? s.statusLines[userId] : undefined)) ?? null;
  return { status, line };
};
