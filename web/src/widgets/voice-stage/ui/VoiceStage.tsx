import { HeadphoneOff, MicOff } from "lucide-react";
import { useCall } from "@/entities/call";
import {
  type Channel,
  computeChannel,
  type GuildEntry,
  has,
  PERM,
  useGuilds,
  userDisplay,
  type VoiceState,
  voiceMembers,
} from "@/entities/guild";
import { joinVoice, NO_CONNECT } from "@/features/voice-call";
import { Avatar, Badge, Button, cn } from "@/shared/ui";
import { StreamTile } from "./StreamTile";

const nameOf = (entry: GuildEntry, userId: string): { name: string; avatarUrl: string | null } => {
  const user = entry.members[userId]?.user;
  return user ? userDisplay(entry, user) : { name: "Участник", avatarUrl: null };
};

/** One person in the call: the avatar ringed while they speak, their name and what they switched off. */
const PersonTile = ({ entry, state, speaking }: { entry: GuildEntry; state: VoiceState; speaking: boolean }) => {
  const { name, avatarUrl } = nameOf(entry, state.user_id);
  return (
    <li
      aria-label={name}
      className="flex aspect-video min-w-0 flex-col items-center justify-center gap-3 rounded-lg bg-sidebar p-4"
    >
      <span
        data-speaking={speaking || undefined}
        className={cn("rounded-full ring-4 ring-transparent transition-shadow", speaking && "ring-online")}
      >
        <Avatar name={name} src={avatarUrl} size={80} />
      </span>
      <span className="flex max-w-full items-center gap-1.5 text-sm font-medium">
        <span className="truncate">{name}</span>
        {state.self_stream && <Badge variant="live">В ЭФИРЕ</Badge>}
        {(state.mute || state.self_mute) && (
          <MicOff role="img" aria-label="Микрофон выключен" className="size-4 shrink-0 text-fg-danger" />
        )}
        {(state.deaf || state.self_deaf) && (
          <HeadphoneOff role="img" aria-label="Звук выключен" className="size-4 shrink-0 text-fg-danger" />
        )}
      </span>
    </li>
  );
};

/**
 * A voice channel's own screen. Outside the call: who is there and a way
 * in. In the call: a tile per person, then a tile per screen share, watched
 * on demand.
 */
export const VoiceStage = ({ guildId, channel }: { guildId: string; channel: Channel }) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const meId = useGuilds((s) => s.me?.id ?? null);
  const inCall = useCall((s) => s.status !== "idle" && s.channelId === channel.id);
  const speaking = useCall((s) => s.speaking);
  const streams = useCall((s) => s.streams);
  const watching = useCall((s) => s.watching);
  if (!entry || meId === null) return null;
  const members = voiceMembers(entry, channel.id);
  const canConnect = has(computeChannel(entry, channel.id, meId), PERM.VIEW_CHANNEL | PERM.CONNECT);

  return (
    <section
      aria-label={`Голосовой канал ${channel.name}`}
      className="flex min-h-0 flex-1 flex-col overflow-y-auto p-4"
    >
      {inCall ? (
        <ul className="grid grid-cols-[repeat(auto-fill,minmax(16rem,1fr))] gap-3">
          {members.map((v) => (
            <PersonTile key={v.user_id} entry={entry} state={v} speaking={speaking.includes(v.user_id)} />
          ))}
          {streams.map((id) => (
            <li key={`stream-${id}`} className="list-none">
              <StreamTile userId={id} name={nameOf(entry, id).name} watching={watching.includes(id)} />
            </li>
          ))}
        </ul>
      ) : (
        <div className="m-auto flex max-w-md flex-col items-center gap-4 text-center">
          <p className="text-base text-fg-muted">{members.length === 0 ? "Здесь пока никого нет." : "Сейчас здесь:"}</p>
          {members.length > 0 && (
            <ul className="flex flex-wrap justify-center gap-3">
              {members.map((v) => {
                const { name, avatarUrl } = nameOf(entry, v.user_id);
                return (
                  <li key={v.user_id} className="flex flex-col items-center gap-1 text-sm">
                    <Avatar name={name} src={avatarUrl} size={40} />
                    <span className="max-w-24 truncate">{name}</span>
                  </li>
                );
              })}
            </ul>
          )}
          <Button
            size="lg"
            disabled={!canConnect}
            onClick={() => {
              void joinVoice(guildId, channel.id);
            }}
          >
            Присоединиться
          </Button>
          {!canConnect && <p className="text-sm text-fg-muted">{NO_CONNECT}</p>}
        </div>
      )}
    </section>
  );
};
