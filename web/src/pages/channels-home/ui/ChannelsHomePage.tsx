import { Users } from "lucide-react";
import { incomingCount, useGuilds } from "@/entities/guild";
import { Badge, formatCount } from "@/shared/ui";
import { FriendsView } from "./Friends";

/**
 * Home's column in the shell: «Друзья», with the requests that wait on
 * the user, then the place of direct messages (stage 2.2).
 */
export const HomeSidebar = () => {
  const incoming = useGuilds((s) => incomingCount(s));
  return (
    <>
      <div className="flex h-(--header-height) shrink-0 items-center px-4 text-base font-semibold shadow-[0_1px_0_var(--border)]">
        Главная
      </div>
      <nav aria-label="Главная" className="flex flex-col gap-0.5 p-2">
        <span
          aria-current="page"
          className="flex h-10 items-center gap-3 rounded-md bg-selected px-3 text-base font-medium text-fg"
        >
          <Users aria-hidden className="size-5" />
          Друзья
          {incoming > 0 && (
            <Badge variant="count" className="ml-auto" aria-label={`Входящих заявок: ${String(incoming)}`}>
              {formatCount(incoming)}
            </Badge>
          )}
        </span>
      </nav>
      <p className="px-4 pt-3 text-xs font-semibold text-fg-faint uppercase">Личные сообщения</p>
      <p className="px-4 pt-2 text-sm text-fg-faint">Появятся в одном из следующих обновлений.</p>
    </>
  );
};

/** The main area of /channels/@me (no server open): the user's friends, as in Discord. */
export const ChannelsHomePage = () => <FriendsView />;
