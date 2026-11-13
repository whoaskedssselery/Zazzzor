import { useQuery } from "@tanstack/react-query";
import { api, type components, unwrap } from "@/shared/api";

export type UserProfile = components["schemas"]["UserProfile"];

export const profileKey = (userId: string) => ["profile", userId] as const;

/**
 * Someone's profile ("about me", the servers in common), fetched when a card
 * opens and kept a minute: it changes rarely and nothing announces it.
 */
export const useProfile = (userId: string) =>
  useQuery({
    queryKey: profileKey(userId),
    queryFn: async (): Promise<UserProfile> =>
      unwrap(await api.GET("/users/{user_id}/profile", { params: { path: { user_id: userId } } })),
    staleTime: 60_000,
    retry: false,
  });
