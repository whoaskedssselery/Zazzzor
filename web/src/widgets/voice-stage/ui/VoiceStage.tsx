import { HeadphoneOff, MicOff } from "lucide-react";
import { type CSSProperties, type ReactNode, type RefObject, useEffect, useRef, useState } from "react";
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
import { CameraView } from "./CameraView";
import { OwnStreamTile, StreamTile } from "./StreamTile";

/** How a person shows on the stage. */
export interface StagePerson {
  name: string;
  avatarUrl: string | null;
}

const nameOf = (entry: GuildEntry, userId: string): StagePerson => {
  const user = entry.members[userId]?.user;
  return user ? userDisplay(entry, user) : { name: "Участник", avatarUrl: null };
};

/** What a person switched off, as icons after their name. */
const Switches = ({ state }: { state: VoiceState }) => (
  <>
    {(state.mute || state.self_mute) && (
      <MicOff role="img" aria-label="Микрофон выключен" className="size-4 shrink-0 text-fg-danger" />
    )}
    {(state.deaf || state.self_deaf) && (
      <HeadphoneOff role="img" aria-label="Звук выключен" className="size-4 shrink-0 text-fg-danger" />
    )}
  </>
);

/**
 * One person in the call: their camera filling the tile when it is on,
 * otherwise the avatar; ringed while they speak, with their name and what
 * they switched off.
 */
const PersonTile = ({
  person,
  state,
  speaking,
  compact,
  camera,
}: {
  person: StagePerson;
  state: VoiceState;
  speaking: boolean;
  compact: boolean;
  /** Their camera is on: "own" for the user's preview, "remote" for someone else's. */
  camera: "own" | "remote" | null;
}) => {
  const { name, avatarUrl } = person;
  if (camera !== null) {
    return (
      <div className="relative size-full min-w-0 overflow-hidden rounded-lg bg-sidebar">
        <CameraView userId={state.user_id} own={camera === "own"} />
        <span
          data-speaking={speaking || undefined}
          aria-hidden
          className={cn(
            "pointer-events-none absolute inset-0 rounded-lg ring-inset ring-transparent transition-shadow duration-(--duration-base) ease-(--ease-out)",
            compact ? "ring-2" : "ring-3",
            speaking && "ring-online",
          )}
        />
        <span
          className={cn(
            "absolute bottom-2 left-2 flex max-w-[calc(100%-1rem)] items-center gap-1.5 rounded-md bg-scrim font-medium text-[#fff]",
            compact ? "px-1.5 py-0.5 text-xs" : "px-2 py-1 text-sm",
          )}
        >
          <span className="truncate">{name}</span>
          {state.self_stream && !compact && <Badge variant="live">В ЭФИРЕ</Badge>}
          <Switches state={state} />
        </span>
      </div>
    );
  }
  return (
    <div
      // The avatar follows the tile's height (a low window, the strip), so nothing spills out of it.
      className={cn(
        "flex size-full min-w-0 flex-col items-center justify-center overflow-hidden rounded-lg bg-sidebar [container-type:size]",
        compact ? "gap-1 p-2" : "gap-[min(0.75rem,4cqh)] p-[min(1rem,5cqh)]",
      )}
    >
      <span
        data-speaking={speaking || undefined}
        className={cn(
          "flex aspect-square shrink-0 rounded-full ring-transparent ring-offset-sidebar transition-shadow duration-(--duration-base) ease-(--ease-out)",
          compact ? "h-[min(2.5rem,45cqh)] ring-2 ring-offset-1" : "h-[min(5rem,45cqh)] ring-4 ring-offset-2",
          speaking && "ring-online",
        )}
      >
        <Avatar name={name} src={avatarUrl} size={compact ? 40 : 80} className="size-full!" />
      </span>
      <span className={cn("flex max-w-full items-center gap-1.5 font-medium", compact ? "text-xs" : "text-sm")}>
        <span className="truncate">{name}</span>
        {state.self_stream && !compact && <Badge variant="live">В ЭФИРЕ</Badge>}
        <Switches state={state} />
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
  style,
}: {
  tile: Tile;
  focused: boolean;
  compact: boolean;
  onToggle: () => void;
  className: string;
  style?: CSSProperties;
}) => (
  <li aria-label={tile.name} className={cn("relative min-w-0 animate-pop-in list-none", className)} style={style}>
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

const GAP = 12;
const PAD = 16;
/** Below this a tile is unreadable: the grid scrolls instead of shrinking further. */
const MIN_TILE = 176;

/**
 * The width of 16:9 tiles that fit `count` of them in `width` × `height`
 * as large as possible, as Discord lays out a call: every column count is
 * tried and the one giving the widest tile wins.
 */
export const fitTileWidth = (count: number, width: number, height: number): number => {
  if (count === 0 || width <= 0 || height <= 0) return MIN_TILE;
  let best = 0;
  for (let cols = 1; cols <= count; cols++) {
    const rows = Math.ceil(count / cols);
    const byWidth = (width - GAP * (cols - 1)) / cols;
    const byHeight = ((height - GAP * (rows - 1)) / rows) * (16 / 9);
    best = Math.max(best, Math.min(byWidth, byHeight));
  }
  return Math.max(MIN_TILE, Math.min(Math.floor(best), width));
};

/** Follows the grid area's size and gives the tile width that fits them all. */
const useFitWidth = (area: RefObject<HTMLElement | null>, count: number, on: boolean): number | undefined => {
  const [size, setSize] = useState<{ w: number; h: number } | null>(null);
  useEffect(() => {
    const el = area.current;
    if (!el || !on || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => {
      setSize({ w: el.clientWidth - PAD * 2, h: el.clientHeight - PAD * 2 });
    });
    observer.observe(el);
    return () => {
      observer.disconnect();
    };
  }, [area, on]);
  return size ? fitTileWidth(count, size.w, size.h) : undefined;
};

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
  const grid = useRef<HTMLDivElement>(null);
  const tileWidth = useFitWidth(grid, tiles.length, front === undefined);

  return (
    <div ref={stage} className="flex min-h-0 flex-1 flex-col bg-chat">
      {front ? (
        <div className="flex min-h-0 flex-1 flex-col gap-[min(0.75rem,2cqh)] p-4 pb-2 [container-type:size]">
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
                  className="aspect-video h-[clamp(3rem,18cqh,7rem)] shrink-0"
                  onToggle={() => {
                    toggle(t);
                  }}
                />
              ))}
            </ul>
          )}
        </div>
      ) : (
        <div ref={grid} className="min-h-0 flex-1 overflow-y-auto p-4">
          <ul className="flex min-h-full flex-wrap content-center justify-center gap-3">
            {tiles.map((t) => (
              <TileSlot
                key={t.key}
                tile={t}
                focused={false}
                compact={false}
                className="aspect-video shrink-0"
                style={{ width: tileWidth ?? 320 }}
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
 * The tiles of the call the user is in: a tile per person in `members`,
 * the user's own screen share, then a tile per other screen share (watched
 * on demand). `person` says how each user shows.
 */
const useCallTiles = (members: VoiceState[], person: (userId: string) => StagePerson): Tile[] => {
  const meId = useGuilds((s) => s.me?.id ?? null);
  const speaking = useCall((s) => s.speaking);
  const streams = useCall((s) => s.streams);
  const watching = useCall((s) => s.watching);
  const sharing = useCall((s) => s.sharing);
  const cameras = useCall((s) => s.cameras);
  const myCamera = useCall((s) => s.camera);
  const cameraOf = (userId: string): "own" | "remote" | null => {
    if (userId === meId) return myCamera ? "own" : null;
    return cameras.includes(userId) ? "remote" : null;
  };
  return [
    ...members.map(
      (v): Tile => ({
        key: `user:${v.user_id}`,
        name: person(v.user_id).name,
        render: (compact) => (
          <PersonTile
            person={person(v.user_id)}
            state={v}
            speaking={speaking.includes(v.user_id)}
            compact={compact}
            camera={cameraOf(v.user_id)}
          />
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
      const { name } = person(id);
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
};

/** The stage of the call the user is in, with its controls: the tiles of `members`. */
export const CallTiles = ({ members, person }: { members: VoiceState[]; person: (userId: string) => StagePerson }) => (
  <CallStage tiles={useCallTiles(members, person)} />
);

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
  if (!entry || meId === null) return null;
  const members = voiceMembers(entry, channel.id);
  const canConnect = has(computeChannel(entry, channel.id, meId), PERM.VIEW_CHANNEL | PERM.CONNECT);

  return (
    <section
      aria-label={`Голосовой канал ${channel.name}`}
      className={cn("flex min-h-0 flex-1 flex-col", !inCall && "overflow-y-auto p-4")}
    >
      {inCall ? (
        <CallTiles members={members} person={(id) => nameOf(entry, id)} />
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
