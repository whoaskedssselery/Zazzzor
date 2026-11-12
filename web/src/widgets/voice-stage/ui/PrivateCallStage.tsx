import { Phone } from "lucide-react";
import { useMemo } from "react";
import { useCall } from "@/entities/call";
import { callMembers, type DMChannel, useGuilds } from "@/entities/guild";
import { joinVoice } from "@/features/voice-call";
import { Avatar, Button, IconButton } from "@/shared/ui";
import { CallTiles, type StagePerson } from "./VoiceStage";

/** Whether the user is in the call of DM or group `channelId`. */
const useInCall = (channelId: string) =>
  useCall((s) => s.status !== "idle" && s.guildId === null && s.channelId === channelId);

/** «Начать звонок» in a DM's header; gone while the user is in its call. */
export const CallButton = ({ channel }: { channel: DMChannel }) => {
  const inCall = useInCall(channel.id);
  const going = useGuilds((s) => Object.keys(s.privates[channel.id]?.voice ?? {}).length > 0);
  if (inCall) return null;
  return (
    <IconButton
      label={going ? "Присоединиться к звонку" : "Начать звонок"}
      icon={Phone}
      tooltipSide="bottom"
      onClick={() => {
        void joinVoice(null, channel.id);
      }}
    />
  );
};

/**
 * The call of a DM or a group above its chat, as in Discord. In the call:
 * the stage with a tile per person and its controls. Outside it, while
 * someone is in it: who, and a way in. Otherwise nothing.
 */
export const PrivateCallStage = ({ channel }: { channel: DMChannel }) => {
  const inCall = useInCall(channel.id);
  const voice = useGuilds((s) => s.privates[channel.id]?.voice);
  const me = useGuilds((s) => s.me);
  const members = useMemo(
    () => (voice ? callMembers({ privates: { [channel.id]: { channel, read: null, voice } } }, channel.id) : []),
    [voice, channel],
  );
  const person = (userId: string): StagePerson => {
    if (me?.id === userId) return { name: me.display_name, avatarUrl: me.avatar_url };
    const user = channel.recipients.find((r) => r.id === userId);
    return user ? { name: user.display_name, avatarUrl: user.avatar_url } : { name: "Участник", avatarUrl: null };
  };
  if (inCall) {
    return (
      <section aria-label="Звонок" className="flex h-[min(28rem,50dvh)] shrink-0 flex-col border-b border-border">
        <CallTiles members={members} person={person} />
      </section>
    );
  }
  if (members.length === 0) return null;
  return (
    <section
      aria-label="Идёт звонок"
      className="flex shrink-0 items-center gap-3 border-b border-border bg-sidebar px-4 py-2.5"
    >
      <ul className="flex -space-x-2">
        {members.slice(0, 5).map((v) => {
          const { name, avatarUrl } = person(v.user_id);
          return (
            <li key={v.user_id} className="rounded-full ring-2 ring-sidebar">
              <Avatar name={name} src={avatarUrl} size={32} />
            </li>
          );
        })}
      </ul>
      <p className="min-w-0 flex-1 truncate text-sm font-medium">
        Идёт звонок
        <span className="text-fg-muted"> · {members.map((v) => person(v.user_id).name).join(", ")}</span>
      </p>
      <Button
        size="sm"
        onClick={() => {
          void joinVoice(null, channel.id);
        }}
      >
        <Phone aria-hidden className="size-4" />
        Присоединиться
      </Button>
    </section>
  );
};
