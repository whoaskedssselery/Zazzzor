import { Bell, BellOff } from "lucide-react";
import type { ReactNode } from "react";
import {
  channelLevel,
  defaultNotify,
  isChannelMuted,
  isChannelOwnMuted,
  isScopeMuted,
  NOTIFY_LEVEL,
  type NotifyPatch,
  overrideOf,
  PRIVATE_SCOPE,
  useGuilds,
  useUpdateNotify,
} from "@/entities/guild";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
  IconButton,
} from "@/shared/ui";

const MINUTE = 60_000;

/** How long a mute lasts, as Discord offers; null is until the user turns it off. */
export const MUTE_SPANS: { label: string; ms: number | null }[] = [
  { label: "На 15 минут", ms: 15 * MINUTE },
  { label: "На 1 час", ms: 60 * MINUTE },
  { label: "На 3 часа", ms: 3 * 60 * MINUTE },
  { label: "На 8 часов", ms: 8 * 60 * MINUTE },
  { label: "На 24 часа", ms: 24 * 60 * MINUTE },
  { label: "Пока не включу", ms: null },
];

const LEVELS = [
  { value: NOTIFY_LEVEL.ALL, label: "Все сообщения" },
  { value: NOTIFY_LEVEL.MENTIONS, label: "Только @упоминания" },
  { value: NOTIFY_LEVEL.NOTHING, label: "Ничего" },
] as const;

const levelLabel = (level: number): string => LEVELS.find((l) => l.value === level)?.label ?? "";

/** «до 18:30» today, «до 13 ноября, 18:30» later; «пока не включите» for a mute without an end. */
export const muteEndText = (until: string | null, now = new Date()): string => {
  if (until === null) return "пока не включите";
  const end = new Date(until);
  const sameDay = end.toDateString() === now.toDateString();
  const time = end.toLocaleTimeString("ru-RU", { hour: "2-digit", minute: "2-digit" });
  return sameDay ? `до ${time}` : `до ${end.toLocaleDateString("ru-RU", { day: "numeric", month: "long" })}, ${time}`;
};

interface Where {
  /** The server, or null for the DMs and groups. */
  guildId: string | null;
  /** A channel of it; none for the whole server. */
  channelId?: string | undefined;
}

/**
 * The items of a notification menu, inside a DropdownMenuContent: mute for
 * a while or until turned back on (or turn the sound back on), and what
 * alerts. For a server also whether @everyone and @here alert; for a channel
 * «as on the server». A DM or a group only mutes.
 */
export const NotifyItems = ({ guildId, channelId }: Where) => {
  const stored = useGuilds((s) => s.notify[guildId ?? PRIVATE_SCOPE]);
  const settings = stored ?? defaultNotify(guildId);
  const update = useUpdateNotify();
  const send = (patch: NotifyPatch) => {
    update.mutate({ guildId, patch });
  };
  /** A mute or unmute of the menu's place: the channel, else the scope. */
  const forChannel = (patch: { muted: boolean; mute_until?: string | null }): NotifyPatch =>
    channelId === undefined ? patch : { channel_overrides: { [channelId]: patch } };
  const s = { notify: { [guildId ?? PRIVATE_SCOPE]: settings } };
  const scopeMuted = isScopeMuted(s, guildId);
  const own = channelId === undefined ? undefined : overrideOf(settings, channelId);
  const ownMuted = channelId !== undefined && isChannelOwnMuted(s, guildId, channelId);
  const what = channelId === undefined ? "сервер" : guildId === null ? "беседу" : "канал";
  const level = channelId === undefined ? settings.message_notifications : (own?.message_notifications ?? 3);

  let mute: ReactNode;
  if (scopeMuted && channelId !== undefined && guildId !== null) {
    mute = (
      <>
        <DropdownMenuLabel>Сервер заглушён {muteEndText(settings.mute_until)}</DropdownMenuLabel>
        <DropdownMenuItem
          onSelect={() => {
            send({ muted: false });
          }}
        >
          Включить звук сервера
        </DropdownMenuItem>
      </>
    );
  } else if (channelId === undefined ? scopeMuted : ownMuted) {
    const until = channelId === undefined ? settings.mute_until : (own?.mute_until ?? null);
    mute = (
      <>
        <DropdownMenuLabel>Заглушено {muteEndText(until)}</DropdownMenuLabel>
        <DropdownMenuItem
          onSelect={() => {
            send(forChannel({ muted: false }));
          }}
        >
          Включить звук
          <Bell aria-hidden />
        </DropdownMenuItem>
      </>
    );
  } else {
    mute = (
      <DropdownMenuSub>
        <DropdownMenuSubTrigger>Заглушить {what}</DropdownMenuSubTrigger>
        <DropdownMenuSubContent>
          {MUTE_SPANS.map((span) => (
            <DropdownMenuItem
              key={span.label}
              onSelect={() => {
                const until = span.ms === null ? null : new Date(Date.now() + span.ms).toISOString();
                send(forChannel({ muted: true, mute_until: until }));
              }}
            >
              {span.label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuSubContent>
      </DropdownMenuSub>
    );
  }
  if (guildId === null) return mute;
  return (
    <>
      {mute}
      <DropdownMenuSeparator />
      <DropdownMenuLabel>Уведомления</DropdownMenuLabel>
      <DropdownMenuRadioGroup
        value={String(level)}
        onValueChange={(v) => {
          const next = Number(v) as 0 | 1 | 2 | 3;
          if (channelId === undefined) send({ message_notifications: next === 3 ? 1 : next });
          else send({ channel_overrides: { [channelId]: { message_notifications: next } } });
        }}
      >
        {channelId !== undefined && (
          <DropdownMenuRadioItem value="3">
            Как на сервере ({levelLabel(settings.message_notifications).toLowerCase()})
          </DropdownMenuRadioItem>
        )}
        {LEVELS.map((l) => (
          <DropdownMenuRadioItem key={l.value} value={String(l.value)}>
            {l.label}
          </DropdownMenuRadioItem>
        ))}
      </DropdownMenuRadioGroup>
      {channelId === undefined && (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuCheckboxItem
            checked={settings.suppress_everyone}
            onCheckedChange={(on) => {
              send({ suppress_everyone: on });
            }}
          >
            Скрывать @everyone и @here
          </DropdownMenuCheckboxItem>
        </>
      )}
    </>
  );
};

/** The bell in a chat's header: its notification menu; crossed out while the chat is muted. */
export const NotifyButton = ({ guildId, channelId }: { guildId: string | null; channelId: string }) => {
  const muted = useGuilds((s) => isChannelMuted(s, guildId, channelId));
  const level = useGuilds((s) => channelLevel(s, guildId, channelId));
  const quiet = muted || (guildId !== null && level === NOTIFY_LEVEL.NOTHING);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <IconButton
          label={muted ? "Уведомления (заглушено)" : "Уведомления"}
          icon={quiet ? BellOff : Bell}
          tooltipSide="bottom"
        />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="min-w-60">
        <NotifyItems guildId={guildId} channelId={channelId} />
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
