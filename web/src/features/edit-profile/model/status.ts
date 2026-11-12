import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type User, updateSignedIn } from "@/entities/session";
import { api, unwrap } from "@/shared/api";
import { useToast } from "@/shared/ui";

export type ChosenStatus = User["status"];

/** The statuses to choose from, in menu order, with what each means. */
export const CHOSEN_STATUSES: readonly { value: ChosenStatus; label: string; hint?: string }[] = [
  { value: "online", label: "В сети" },
  { value: "idle", label: "Нет на месте" },
  { value: "dnd", label: "Не беспокоить", hint: "Без звука и уведомлений" },
  { value: "invisible", label: "Невидимка", hint: "Для других вы не в сети" },
];

/** Status lines are at most this many characters. */
export const STATUS_LINE_MAX = 128;

type StatusPatch = { status?: ChosenStatus; custom_status?: string | null };

/**
 * Changes the user's status or status line (PATCH /users/@me). The answer
 * goes into the session at once; the gateway's USER_UPDATE brings the same.
 */
export const useSetStatus = () => {
  const queryClient = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: async (body: StatusPatch) => unwrap(await api.PATCH("/users/@me", { body })),
    onSuccess: (user) => {
      updateSignedIn(queryClient, user);
    },
    onError: () => {
      toast.show({ text: "Не удалось сменить статус", tone: "error" });
    },
  });
};
