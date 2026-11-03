import {
  ArrowRightLeft,
  Ellipsis,
  Gavel,
  HeadphoneOff,
  MicOff,
  Pencil,
  PhoneOff,
  Shield,
  UserMinus,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { type GuildEntry, useGuilds } from "@/entities/guild";
import {
  Avatar,
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuLabel,
  ContextMenuSeparator,
  ContextMenuTrigger,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  IconButton,
} from "@/shared/ui";
import { memberActions, useVoiceModeration, type VoiceActions, voiceActions } from "../model/actions";
import { type MemberDialog, MemberDialogs } from "./MemberDialogs";

interface Item {
  key: MemberDialog;
  label: string;
  icon: typeof Pencil;
  danger?: boolean;
}

const itemsFor = (entry: GuildEntry, meId: string, userId: string): Item[] => {
  const can = memberActions(entry, meId, userId);
  const items: Item[] = [];
  if (can.nick) items.push({ key: "nick", label: "Изменить ник", icon: Pencil });
  if (can.roles) items.push({ key: "roles", label: "Роли", icon: Shield });
  if (can.kick) items.push({ key: "kick", label: "Выгнать", icon: UserMinus, danger: true });
  if (can.ban) items.push({ key: "ban", label: "Забанить", icon: Gavel, danger: true });
  return items;
};

/**
 * The voice moderation items of a member in voice. A component of its own,
 * so its request hook mounts only while the menu is open (a list has a menu
 * on every row).
 */
const VoiceItems = ({
  guildId,
  userId,
  entry,
  voice,
  kind,
}: {
  guildId: string;
  userId: string;
  entry: GuildEntry;
  voice: VoiceActions;
  kind: "context" | "dropdown";
}) => {
  const moderate = useVoiceModeration(guildId);
  const Row = kind === "context" ? ContextMenuItem : DropdownMenuItem;
  const act = (body: { mute?: boolean; deaf?: boolean; channel_id?: string | null }) => {
    moderate.mutate({ userId, body });
  };
  return (
    <>
      {voice.mute && (
        <Row
          onSelect={() => {
            act({ mute: !voice.muted });
          }}
        >
          {voice.muted ? "Включить микрофон на сервере" : "Выключить микрофон на сервере"}
          <MicOff aria-hidden />
        </Row>
      )}
      {voice.deafen && (
        <Row
          onSelect={() => {
            act({ deaf: !voice.deafened });
          }}
        >
          {voice.deafened ? "Включить звук на сервере" : "Выключить звук на сервере"}
          <HeadphoneOff aria-hidden />
        </Row>
      )}
      {voice.moveTo.map((id) => (
        <Row
          key={id}
          onSelect={() => {
            act({ channel_id: id });
          }}
        >
          {`Переместить в «${entry.channels[id]?.name ?? "канал"}»`}
          <ArrowRightLeft aria-hidden />
        </Row>
      ))}
      {voice.disconnect && (
        <Row
          variant="danger"
          onSelect={() => {
            act({ channel_id: null });
          }}
        >
          Отключить от голоса
          <PhoneOff aria-hidden />
        </Row>
      )}
    </>
  );
};

/** The card at the top of the menu: who it is about. */
const Card = ({ entry, userId, kind }: { entry: GuildEntry; userId: string; kind: "context" | "dropdown" }) => {
  const member = entry.members[userId];
  if (!member) return null;
  const Label = kind === "context" ? ContextMenuLabel : DropdownMenuLabel;
  const name = member.nick ?? member.user.display_name;
  return (
    <Label className="flex items-center gap-2.5 py-2">
      <Avatar name={name} src={member.user.avatar_url} size={32} />
      <span className="min-w-0">
        <bdi className="block truncate text-base font-semibold text-fg">{name}</bdi>
        <span className="block truncate text-xs font-normal text-fg-muted">@{member.user.username}</span>
      </span>
    </Label>
  );
};

/**
 * The member actions of a server for one member, in a menu: a card with who
 * it is, then «Изменить ник», «Роли», «Выгнать» and «Забанить», each only
 * when the user's permissions and rank allow it. `children` is what opens
 * it: by right click in "context" mode (a row of the member list), by click
 * in "dropdown" mode (a message author's name, as a profile); without
 * children it is a «…» button, shown only when some action is allowed. A
 * member the store does not hold (a former member's message) gets no menu.
 */
export const MemberMenu = ({
  guildId,
  userId,
  mode = "context",
  children,
}: {
  guildId: string;
  userId: string;
  mode?: "context" | "dropdown";
  children?: ReactNode;
}) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const meId = useGuilds((s) => s.me?.id ?? null);
  const [dialog, setDialog] = useState<MemberDialog | null>(null);
  const member = entry?.members[userId];
  if (!entry || meId === null || !member) return <>{children}</>;

  const items = itemsFor(entry, meId, userId);
  const voice = voiceActions(entry, meId, userId);
  // Mounted only while one is open: a list has a menu on every row.
  const dialogs = dialog !== null && (
    <MemberDialogs
      entry={entry}
      meId={meId}
      member={member}
      open={dialog}
      onClose={() => {
        setDialog(null);
      }}
    />
  );
  const rows = (kind: "context" | "dropdown") => {
    const Row = kind === "context" ? ContextMenuItem : DropdownMenuItem;
    const Separator = kind === "context" ? ContextMenuSeparator : DropdownMenuSeparator;
    return (
      <>
        <Card entry={entry} userId={userId} kind={kind} />
        {items.length > 0 && <Separator />}
        {items.map((i) => (
          <Row
            key={i.key}
            variant={i.danger ? "danger" : "default"}
            onSelect={() => {
              setDialog(i.key);
            }}
          >
            {i.label}
            <i.icon aria-hidden />
          </Row>
        ))}
        {voice && (
          <>
            <Separator />
            <VoiceItems guildId={guildId} userId={userId} entry={entry} voice={voice} kind={kind} />
          </>
        )}
      </>
    );
  };

  if (children !== undefined && mode === "dropdown") {
    return (
      <>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
          <DropdownMenuContent align="start">{rows("dropdown")}</DropdownMenuContent>
        </DropdownMenu>
        {dialogs}
      </>
    );
  }
  if (children !== undefined) {
    return (
      <>
        <ContextMenu>
          <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
          <ContextMenuContent>{rows("context")}</ContextMenuContent>
        </ContextMenu>
        {dialogs}
      </>
    );
  }
  if (items.length === 0 && !voice) return null;
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <IconButton size="sm" label={`Действия с ${member.nick ?? member.user.display_name}`} icon={Ellipsis} />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">{rows("dropdown")}</DropdownMenuContent>
      </DropdownMenu>
      {dialogs}
    </>
  );
};
