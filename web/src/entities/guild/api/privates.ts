import { useMutation } from "@tanstack/react-query";
import { api, errorText, unwrap } from "@/shared/api";
import { useToast } from "@/shared/ui";
import type { DMChannel } from "../model/reducer";
import { applyGatewayDispatch } from "../model/store";

// Private channels change by the server's DM_CHANNEL_CREATE, _UPDATE and
// _DELETE. Opening one also takes the answer into the store at once, so the
// page it opens has it before the event arrives (the event changes nothing).

/** Opens the 1:1 DM with user `userId` (made if new, reopened if closed) and gives it. */
export const openDirect = async (userId: string): Promise<DMChannel> => {
  const channel = unwrap(await api.POST("/users/@me/channels", { body: { recipient_id: userId } }));
  applyGatewayDispatch("DM_CHANNEL_CREATE", channel);
  return channel;
};

/** Makes a group with the user and `userIds` (2–9 of their friends) and gives it. */
export const createGroup = async (userIds: string[]): Promise<DMChannel> => {
  const channel = unwrap(await api.POST("/users/@me/channels", { body: { recipient_ids: userIds } }));
  applyGatewayDispatch("DM_CHANNEL_CREATE", channel);
  return channel;
};

/** Opening a DM with someone; a refusal (no shared server, not friends, a block) is a toast. */
export const useOpenDirect = () => {
  const toast = useToast();
  return useMutation({
    mutationFn: openDirect,
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};

/** Making a group; the caller shows a refusal where it was asked. */
export const useCreateGroup = () => useMutation({ mutationFn: createGroup });

/** Renaming a group (null: back to its members' names); a refusal is a toast. */
export const useRenameGroup = () => {
  const toast = useToast();
  return useMutation({
    mutationFn: async ({ id, name }: { id: string; name: string | null }) => {
      unwrap(await api.PATCH("/channels/{channel_id}", { params: { path: { channel_id: id } }, body: { name } }));
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};

/** Adding friends to a group, one request each; the caller shows a refusal. */
export const useAddRecipients = () =>
  useMutation({
    mutationFn: async ({ id, userIds }: { id: string; userIds: string[] }) => {
      for (const userId of userIds) {
        unwrap(
          await api.PUT("/channels/{channel_id}/recipients/{user_id}", {
            params: { path: { channel_id: id, user_id: userId } },
          }),
        );
      }
    },
  });

/** Removing someone from a group (the owner) or leaving it (anyone); a refusal is a toast. */
export const useRemoveRecipient = () => {
  const toast = useToast();
  return useMutation({
    mutationFn: async ({ id, userId }: { id: string; userId: string }) => {
      unwrap(
        await api.DELETE("/channels/{channel_id}/recipients/{user_id}", {
          params: { path: { channel_id: id, user_id: userId } },
        }),
      );
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};

/** Closing a DM for the user, or leaving a group; a refusal is a toast. */
export const useClosePrivate = () => {
  const toast = useToast();
  return useMutation({
    mutationFn: async (id: string) => {
      unwrap(await api.DELETE("/channels/{channel_id}", { params: { path: { channel_id: id } } }));
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};
