import { useMutation } from "@tanstack/react-query";
import { ApiError, api, errorText, unwrap } from "@/shared/api";
import { useToast } from "@/shared/ui";

/** The answer to a request for a username nobody has (or the user's own). */
export const NO_SUCH_USER = "Пользователя с таким именем нет. Проверьте написание.";

// The user's friends, blocks and requests change only by the server's
// RELATIONSHIP_ADD and _REMOVE: these calls ask for a change and say when
// one is refused, and the gateway brings the result.

/**
 * A friend request by username. The server answers the same whether or not
 * the request really went out (a block stays unknown to the blocked user),
 * so success is "sent" and nothing more.
 */
export const useRequestFriend = () =>
  useMutation({
    mutationFn: async (username: string) => {
      unwrap(await api.POST("/users/@me/relationships", { body: { username } }));
    },
  });

/** The text of a failed friend request. */
export const requestErrorText = (e: unknown): string =>
  e instanceof ApiError && e.status === 404 ? NO_SUCH_USER : errorText(e);

/** Accepting a request (1) or blocking (2) user `id`; a refusal is a toast. */
export const useSetRelationship = () => {
  const toast = useToast();
  return useMutation({
    mutationFn: async ({ id, type }: { id: string; type: 1 | 2 }) => {
      unwrap(
        await api.PUT("/users/@me/relationships/{user_id}", { params: { path: { user_id: id } }, body: { type } }),
      );
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};

/** Removing a friend, declining or cancelling a request, or unblocking; a refusal is a toast. */
export const useRemoveRelationship = () => {
  const toast = useToast();
  return useMutation({
    mutationFn: async (id: string) => {
      unwrap(await api.DELETE("/users/@me/relationships/{user_id}", { params: { path: { user_id: id } } }));
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};
