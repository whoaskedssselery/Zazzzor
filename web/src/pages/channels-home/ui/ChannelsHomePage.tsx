import { Link, useParams } from "@tanstack/react-router";
import { Users } from "lucide-react";
import { incomingCount, useGuilds } from "@/entities/guild";
import { Badge, cn, formatCount } from "@/shared/ui";
import { PrivateList } from "@/widgets/private-list";
import { FriendsView } from "./Friends";

/**
 * Home's column in the shell: «Друзья», with the requests that wait on
 * the user, then their direct messages and groups.
 */
export const HomeSidebar = () => {
  const incoming = useGuilds((s) => incomingCount(s));
  const { channelId } = useParams({ strict: false });
  const friends = channelId === undefined;
  return (
    <>
      <div className="flex h-(--header-height) shrink-0 items-center px-4 text-base font-semibold shadow-[0_1px_0_var(--border)]">
        Главная
      </div>
      <nav aria-label="Главная" className="flex flex-col gap-0.5 p-2">
        <Link
          to="/channels/@me"
          aria-current={friends ? "page" : undefined}
          className={cn(
            "flex h-10 items-center gap-3 rounded-md px-3 text-base font-medium text-fg-muted transition-colors duration-(--duration-fast) hover:bg-hover hover:text-fg",
            friends && "bg-selected text-fg",
          )}
        >
          <Users aria-hidden className="size-5" />
          Друзья
          {incoming > 0 && (
            <Badge variant="count" className="ml-auto" aria-label={`Входящих заявок: ${String(incoming)}`}>
              {formatCount(incoming)}
            </Badge>
          )}
        </Link>
      </nav>
      <PrivateList />
    </>
  );
};

/** The main area of /channels/@me (no server open): the user's friends, as in Discord. */
export const ChannelsHomePage = () => <FriendsView />;
