import { Pencil, Users } from "lucide-react";
import { useState } from "react";
import { dmPeer, privateName, useConnectionStatus, useGuilds } from "@/entities/guild";
import { NotifyButton } from "@/features/notify";
import { AddPeopleButton, RenameGroupDialog } from "@/features/private-groups";
import { SearchButton } from "@/features/search-messages";
import { Avatar } from "@/shared/ui";
import { MembersButton, NavButton, ShellHeader } from "@/widgets/app-shell";
import { PrivateChatView } from "@/widgets/chat";
import { CallButton, PrivateCallStage } from "@/widgets/voice-stage";

/**
 * The main area of /channels/@me/$channelId: a DM or a group with its
 * header (who it is with; a group's name, which a click renames, and the
 * member toggle) and its chat. A channel the user no longer has (closed
 * elsewhere, left, removed) says so once the gateway is ready.
 */
export const DirectMessagePage = ({ channelId }: { channelId: string }) => {
  const channel = useGuilds((s) => s.privates[channelId]?.channel);
  const ready = useConnectionStatus() === "ready";
  const peerStatus = useGuilds((s) => {
    const peer = channel ? dmPeer(channel) : null;
    return (peer ? s.presences[peer.id] : undefined) ?? "offline";
  });
  const [renaming, setRenaming] = useState(false);
  if (!channel) {
    return (
      <>
        <ShellHeader>
          <NavButton />
        </ShellHeader>
        <div className="flex min-h-0 flex-1 items-center justify-center p-6 text-center">
          {ready && <p className="max-w-sm text-base text-fg-muted">Этой переписки у вас больше нет.</p>}
        </div>
      </>
    );
  }
  const peer = dmPeer(channel);
  const name = privateName(channel);
  return (
    <>
      <ShellHeader>
        <NavButton />
        {peer ? (
          <>
            <Avatar
              name={peer.display_name}
              src={peer.avatar_url}
              size={24}
              status={peerStatus}
              className="max-sm:hidden"
            />
            <h1 className="min-w-0 truncate text-base font-semibold">{peer.display_name}</h1>
          </>
        ) : (
          <>
            <Users aria-hidden className="size-6 shrink-0 text-fg-faint" strokeWidth={1.75} />
            <h1 className="min-w-0 truncate text-base font-semibold">
              <button
                type="button"
                aria-label={`${name}, изменить название группы`}
                className="group/name flex max-w-full cursor-pointer items-center gap-1.5 rounded-sm hover:underline"
                onClick={() => {
                  setRenaming(true);
                }}
              >
                <span className="truncate">{name}</span>
                <Pencil
                  aria-hidden
                  className="size-3.5 shrink-0 text-fg-faint opacity-0 group-hover/name:opacity-100"
                />
              </button>
            </h1>
          </>
        )}
        <div className="ml-auto flex shrink-0 items-center">
          <CallButton channel={channel} />
          <NotifyButton guildId={null} channelId={channel.id} />
          <SearchButton scope={{ kind: "channel", channelId: channel.id }} placeholder="Искать в переписке" />
          <AddPeopleButton channel={channel} />
          {!peer && <MembersButton />}
        </div>
      </ShellHeader>
      <PrivateCallStage channel={channel} />
      <PrivateChatView key={channel.id} channel={channel} />
      {!peer && <RenameGroupDialog channel={channel} open={renaming} onOpenChange={setRenaming} />}
    </>
  );
};
