import { useNavigate } from "@tanstack/react-router";
import { Settings } from "lucide-react";
import type { CSSProperties } from "react";
import { useConnectionStatus } from "@/entities/guild";
import { useSession } from "@/entities/session";
import { LogoutButton } from "@/features/auth";
import { Avatar, IconButton } from "@/shared/ui";

/**
 * The signed-in user at the foot of the channel list: avatar with presence,
 * name, status, then sign-out and the user settings. The user's own presence
 * is the gateway's: online while connected (the server tells others, not the
 * user). The microphone and headphones buttons come with voice (stage 1.3).
 */
export const UserPanel = () => {
  const { user } = useSession();
  const online = useConnectionStatus() === "ready";
  const navigate = useNavigate();
  if (!user) return null;
  return (
    <section
      aria-label="Ваш профиль"
      className="flex h-(--user-panel-height) shrink-0 items-center gap-1 bg-panel px-2"
      style={{ "--avatar-cutout": "var(--bg-panel)" } as CSSProperties}
    >
      <div className="flex min-w-0 flex-1 items-center gap-2 py-1 pr-1 pl-0.5">
        <Avatar name={user.display_name} src={user.avatar_url} size={32} status={online ? "online" : "offline"} />
        <span className="min-w-0">
          <span data-user-name className="block truncate text-sm leading-4 font-semibold">
            {user.display_name}
          </span>
          <span data-user-status className="block truncate text-xs text-fg-faint">
            {online ? "в сети" : "не в сети"}
          </span>
        </span>
      </div>
      <LogoutButton iconOnly />
      <IconButton
        label="Настройки пользователя"
        icon={Settings}
        onClick={() => {
          void navigate({ href: "/settings/profile" });
        }}
      />
    </section>
  );
};
