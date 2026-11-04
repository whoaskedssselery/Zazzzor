import { HeadphoneOff, MicOff } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { getCall, useCall } from "@/entities/call";
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
import { joinVoice, NO_CONNECT, watchStream } from "@/features/voice-call";
import { Avatar, Badge, Button, cn } from "@/shared/ui";
import { CallControls } from "./CallControls";
import { OwnStreamTile, StreamTile } from "./StreamTile";

const nameOf = (entry: GuildEntry, userId: string): { name: string; avatarUrl: string | null } => {
  const user = entry.members[userId]?.user;
  return user ? userDisplay(entry, user) : { name: "Участник", avatarUrl: null };
};

/** One person in the call: the avatar ringed while they speak, their name and what they switched off. */
const PersonTile = ({
  entry,
  state,
  speaking,
  compact,
}: {
  entry: GuildEntry;
  state: VoiceState;
  speaking: boolean;
  compact: boolean;
}) => {
  const { name, avatarUrl } = nameOf(entry, state.user_id);
  return (
    <div
      className={cn(
        "flex size-full min-w-0 flex-col items-center justify-center rounded-lg bg-sidebar",
        compact ? "gap-1.5 p-2" : "gap-3 p-4",
      )}
    >
      <span
        data-speaking={speaking || undefined}
        className={cn(
          "flex rounded-full ring-transparent ring-offset-sidebar transition-shadow duration-(--duration-base) ease-(--ease-out)",
          compact ? "ring-2 ring-offset-1" : "ring-4 ring-offset-2",
          speaking && "ring-online",
        )}
      >
        <Avatar name={name} src={avatarUrl} size={compact ? 40 : 80} />
      </span>
      <span className={cn("flex max-w-full items-center gap-1.5 font-medium", compact ? "text-xs" : "text-sm")}>
        <span className="truncate">{name}</span>
        {state.self_stream && !compact && <Badge variant="live">В ЭФИРЕ</Badge>}
        {(state.mute || state.self_mute) && (
          <MicOff role="img" aria-label="Микрофон выключен" className="size-4 shrink-0 text-fg-danger" />
        )}
        {(state.deaf || state.self_deaf) && (
          <HeadphoneOff role="img" aria-label="Звук выключен" className="size-4 shrink-0 text-fg-danger" />
        )}
      </span>
    </div>
  );
};

interface Tile {
  key: string;
  /** The list item's name, and the focus button's. */
  name: string;
  /** `front` brings this tile to the front (a stream as its «Смотреть» starts it). */
  render: (compact: boolean, front: () => void) => ReactNode;
  /** What else bringing it to the front does (an unwatched stream starts playing). */
  onFocus?: () => void;
}

/**
 * A tile's place on the stage. A click on it (behind its own buttons)
 * brings it to the front; a click on the front one sends it back.
 */
const TileSlot = ({
  tile,
  focused,
  compact,
  onToggle,
  className,
}: {
  tile: Tile;
  focused: boolean;
  compact: boolean;
  onToggle: () => void;
  className: string;
}) => (
  <li aria-label={tile.name} className={cn("relative min-w-0 animate-pop-in list-none", className)}>
    {tile.render(compact, () => {
      if (!focused) onToggle();
    })}
    <button
      type="button"
      aria-label={focused ? `Вернуть в сетку: ${tile.name}` : `Развернуть: ${tile.name}`}
      aria-pressed={focused}
      className={cn(
        "absolute inset-0 cursor-zoom-in rounded-lg ring-inset transition-shadow duration-(--duration-fast) hover:ring-2 hover:ring-border-strong focus-visible:ring-2 focus-visible:ring-focus focus-visible:outline-none",
        focused && "cursor-zoom-out",
      )}
      onClick={onToggle}
    />
  </li>
);

/** The stage in the call: every tile in a grid, or one in front with the others in a strip under it, as in Discord. */
const CallStage = ({ tiles }: { tiles: Tile[] }) => {
  const [focus, setFocus] = useState<string | null>(null);
  const [fullscreen, setFullscreen] = useState(false);
  const stage = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const sync = () => {
      setFullscreen(document.fullscreenElement !== null && document.fullscreenElement === stage.current);
    };
    document.addEventListener("fullscreenchange", sync);
    return () => {
      document.removeEventListener("fullscreenchange", sync);
    };
  }, []);
  // A focused tile that left (the stream ended, the person hung up) gives the grid back.
  const front = tiles.find((t) => t.key === focus);
  const toggle = (t: Tile) => {
    if (front?.key === t.key) setFocus(null);
    else {
      t.onFocus?.();
      setFocus(t.key);
    }
  };
  const rest = tiles.filter((t) => t !== front);

  return (
    <div ref={stage} className="flex min-h-0 flex-1 flex-col bg-chat">
      {front ? (
        <div className="flex min-h-0 flex-1 flex-col gap-3 p-4 pb-2">
          <ul className="flex min-h-0 flex-1">
            <TileSlot
              tile={front}
              focused
              compact={false}
              className="size-full"
              onToggle={() => {
                toggle(front);
              }}
            />
          </ul>
          {rest.length > 0 && (
            <ul aria-label="Остальные в звонке" className="flex shrink-0 justify-center gap-2 overflow-x-auto">
              {rest.map((t) => (
                <TileSlot
                  key={t.key}
                  tile={t}
                  focused={false}
                  compact
                  className="aspect-video h-28 shrink-0"
                  onToggle={() => {
                    toggle(t);
                  }}
                />
              ))}
            </ul>
          )}
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto p-4">
          <ul className="grid grid-cols-[repeat(auto-fill,minmax(16rem,1fr))] gap-3">
            {tiles.map((t) => (
              <TileSlot
                key={t.key}
                tile={t}
                focused={false}
                compact={false}
                className="aspect-video"
                onToggle={() => {
                  toggle(t);
                }}
              />
            ))}
          </ul>
        </div>
      )}
      <CallControls
        fullscreen={fullscreen}
        onFullscreen={() => {
          if (fullscreen) void document.exitFullscreen();
          else void stage.current?.requestFullscreen();
        }}
      />
    </div>
  );
};

/**
 * A voice channel's own screen. Outside the call: who is there and a way
 * in. In the call: a tile per person, the user's own screen share, then a
 * tile per other screen share (watched on demand); any tile can be brought
 * to the front, and the call's controls sit under them.
 */
export const VoiceStage = ({ guildId, channel }: { guildId: string; channel: Channel }) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const meId = useGuilds((s) => s.me?.id ?? null);
  const inCall = useCall((s) => s.status !== "idle" && s.channelId === channel.id);
  const speaking = useCall((s) => s.speaking);
  const streams = useCall((s) => s.streams);
  const watching = useCall((s) => s.watching);
  const sharing = useCall((s) => s.sharing);
  if (!entry || meId === null) return null;
  const members = voiceMembers(entry, channel.id);
  const canConnect = has(computeChannel(entry, channel.id, meId), PERM.VIEW_CHANNEL | PERM.CONNECT);

  const tiles: Tile[] = [
    ...members.map(
      (v): Tile => ({
        key: `user:${v.user_id}`,
        name: nameOf(entry, v.user_id).name,
        render: (compact) => (
          <PersonTile entry={entry} state={v} speaking={speaking.includes(v.user_id)} compact={compact} />
        ),
      }),
    ),
    ...(sharing
      ? [
          {
            key: "own",
            name: "Ваша демонстрация экрана",
            render: (compact: boolean) => <OwnStreamTile compact={compact} />,
          },
        ]
      : []),
    ...streams.map((id): Tile => {
      const { name } = nameOf(entry, id);
      return {
        key: `stream:${id}`,
        name: `${name} — демонстрация экрана`,
        render: (compact, front) => (
          <StreamTile userId={id} name={name} watching={watching.includes(id)} compact={compact} onWatch={front} />
        ),
        onFocus: () => {
          // Read now, not at render: «Смотреть» may have started it a moment ago.
          if (!getCall().watching.includes(id)) watchStream(id, true);
        },
      };
    }),
  ];

  return (
    <section
      aria-label={`Голосовой канал ${channel.name}`}
      className={cn("flex min-h-0 flex-1 flex-col", !inCall && "overflow-y-auto p-4")}
    >
      {inCall ? (
        <CallStage tiles={tiles} />
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
