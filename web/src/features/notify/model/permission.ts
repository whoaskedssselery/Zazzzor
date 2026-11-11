import { useCallback, useEffect, useState } from "react";

/** The browser's answer about notifications; "unsupported" where there are none. */
export type Permission = NotificationPermission | "unsupported";

const current = (): Permission => ("Notification" in window ? Notification.permission : "unsupported");

/**
 * The notification permission, kept current (the user may change it in the
 * browser's site settings), and the request, which must follow a click.
 */
export const useNotificationPermission = (): { permission: Permission; request: () => Promise<Permission> } => {
  const [permission, setPermission] = useState(current);
  useEffect(() => {
    const update = () => {
      setPermission(current());
    };
    window.addEventListener("focus", update);
    let status: PermissionStatus | null = null;
    // Not every browser tells about changes (or has the Permissions API at all).
    void (navigator.permissions as Permissions | undefined)
      ?.query({ name: "notifications" })
      .then((s) => {
        status = s;
        s.addEventListener("change", update);
      })
      .catch(() => undefined);
    return () => {
      window.removeEventListener("focus", update);
      status?.removeEventListener("change", update);
    };
  }, []);
  const request = useCallback(async () => {
    if (!("Notification" in window)) return "unsupported" as const;
    const answer = await Notification.requestPermission();
    setPermission(answer);
    return answer;
  }, []);
  return { permission, request };
};
