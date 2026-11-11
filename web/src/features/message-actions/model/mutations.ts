import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  type Message,
  removeMessage,
  replaceMessage,
  updateHistory,
  updateMessage,
  withReaction,
} from "@/entities/message";
import { api, errorText, unwrap } from "@/shared/api";
import { useToast } from "@/shared/ui";

/**
 * Editing an own message (PATCH). The history takes the answer at once; its
 * MESSAGE_UPDATE changes nothing more. A failure is a toast, and the editor
 * stays closed: the old text is still there to edit again.
 */
export const useEditMessage = () => {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: async ({ message, content }: { message: Message; content: string }) =>
      unwrap(
        await api.PATCH("/channels/{channel_id}/messages/{message_id}", {
          params: { path: { channel_id: message.channel_id, message_id: message.id } },
          body: { content },
        }),
      ),
    onSuccess: (m) => {
      updateHistory(qc, m.channel_id, (h) => replaceMessage(h, m));
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};

/**
 * Deleting a message (own, or anyone's with MANAGE_MESSAGES). It leaves the
 * history at once; one already gone (404) counts as deleted.
 */
export const useDeleteMessage = () => {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: async (message: Message) => {
      const r = await api.DELETE("/channels/{channel_id}/messages/{message_id}", {
        params: { path: { channel_id: message.channel_id, message_id: message.id } },
      });
      if (r.response.status === 404) return;
      unwrap(r);
    },
    onSuccess: (_, message) => {
      updateHistory(qc, message.channel_id, (h) => removeMessage(h, message.id));
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};

/**
 * Adds or takes back the user's reaction `emoji` on a message. The chat
 * shows it at once; the server's event of it then changes nothing. A
 * refusal puts it back as it was and says why.
 */
export const useToggleReaction = () => {
  const qc = useQueryClient();
  const toast = useToast();
  return useMutation({
    mutationFn: async ({ message, emoji, add }: { message: Message; emoji: string; add: boolean }) => {
      const path = { channel_id: message.channel_id, message_id: message.id, emoji };
      const r = add
        ? await api.PUT("/channels/{channel_id}/messages/{message_id}/reactions/{emoji}/@me", { params: { path } })
        : await api.DELETE("/channels/{channel_id}/messages/{message_id}/reactions/{emoji}/@me", { params: { path } });
      unwrap(r);
    },
    onMutate: ({ message, emoji, add }) => {
      updateHistory(qc, message.channel_id, (h) =>
        updateMessage(h, message.id, (m) => withReaction(m, emoji, true, add)),
      );
    },
    onError: (e, { message, emoji, add }) => {
      updateHistory(qc, message.channel_id, (h) =>
        updateMessage(h, message.id, (m) => withReaction(m, emoji, true, !add)),
      );
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};
