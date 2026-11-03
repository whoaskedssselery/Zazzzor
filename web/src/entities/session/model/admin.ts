import { queryOptions, useQuery } from "@tanstack/react-query";
import { ApiError, api, type components, unwrap } from "@/shared/api";

export type InstanceSettings = components["schemas"]["InstanceSettings"];

/** Query key of GET /admin/settings; the session's end clears it with the rest of the cache. */
export const ADMIN_SETTINGS_KEY = ["admin", "settings"] as const;

/**
 * The instance settings, which only administrators may read: null for
 * anyone else (403 admin_only, or 404 should the server hide the route).
 * The answer is both the settings and the admin check, and stays until the
 * session changes.
 */
export const adminSettingsQuery = queryOptions({
  queryKey: ADMIN_SETTINGS_KEY,
  queryFn: async (): Promise<InstanceSettings | null> => {
    try {
      return unwrap(await api.GET("/admin/settings"));
    } catch (e) {
      if (e instanceof ApiError && (e.status === 403 || e.status === 404)) return null;
      throw e;
    }
  },
  staleTime: Infinity,
  retry: false,
});

/** Whether the user administers the instance: undefined until known (or when the check failed). */
export const useIsAdmin = (): boolean | undefined => {
  const { data } = useQuery(adminSettingsQuery);
  return data === undefined ? undefined : data !== null;
};
