import { useNavigate } from "@tanstack/react-router";
import { Phone, PhoneOff, Users } from "lucide-react";
import { useEffect, useMemo } from "react";
import { useCall } from "@/entities/call";
import {
  type Call,
  dmPeer,
  type GuildsState,
  isChannelMuted,
  type PrivateEntry,
  privateName,
  useGuilds,
} from "@/entities/guild";
import { useNotifyPrefs } from "@/features/notify";
import { declineCall, joinVoice, startRingtone } from "@/features/voice-call";
import { Avatar, Button } from "@/shared/ui";

/**
 * The calls that ring the user now: they are on the list of a call they are
 * not in, in a DM or group they have not muted.
 */
export const ringingCalls = (
  s: Pick<GuildsState, "me" | "calls" | "privates" | "notify">,
  inCall: string | null,
): PrivateEntry[] => {
  const me = s.me?.id;
  if (me === undefined) return [];
  return Object.entries(s.calls)
    .filter(([id, c]: [string, Call]) => c.ringing.includes(me) && id !== inCall && !isChannelMuted(s, null, id))
    .map(([id]) => s.privates[id])
    .filter((p): p is PrivateEntry => p !== undefined);
};

const windowHidden = () => document.visibilityState !== "visible" || !document.hasFocus();

/** One incoming call: who calls, and the two answers. */
const IncomingCall = ({ entry }: { entry: PrivateEntry }) => {
  const navigate = useNavigate();
  const { channel } = entry;
  const peer = dmPeer(channel);
  const name = privateName(channel);
  return (
    <section
      role="alertdialog"
      aria-label={`Входящий звонок: ${name}`}
      className="pointer-events-auto flex w-80 max-w-[calc(100vw-2rem)] animate-float flex-col items-center gap-3 rounded-lg bg-overlay p-5 text-center shadow-float ring-1 ring-border"
    >
      {peer ? (
        <Avatar name={peer.display_name} src={peer.avatar_url} size={80} />
      ) : (
        <span aria-hidden className="flex size-20 items-center justify-center rounded-full bg-accent text-on-accent">
          <Users className="size-9" strokeWidth={1.75} />
        </span>
      )}
      <div className="min-w-0">
        <p className="truncate text-lg font-semibold">{name}</p>
        <p className="text-sm text-fg-muted">{peer ? "Входящий звонок…" : "Звонок в группе…"}</p>
      </div>
      <div className="flex gap-3">
        <Button
          variant="danger"
          onClick={() => {
            void declineCall(channel.id);
          }}
        >
          <PhoneOff aria-hidden className="size-4" />
          Отклонить
        </Button>
        <Button
          variant="primary"
          autoFocus
          onClick={() => {
            void navigate({ to: "/channels/@me/$channelId", params: { channelId: channel.id } });
            void joinVoice(null, channel.id);
          }}
        >
          <Phone aria-hidden className="size-4" />
          Принять
        </Button>
      </div>
    </section>
  );
};

/**
 * Incoming calls, over whatever screen is open, as in Discord: a card per
 * call with «Принять» and «Отклонить», a ring while any rings (if sounds are
 * on), and a system notification when the window is in the background.
 */
export const IncomingCalls = () => {
  const calls = useGuilds((s) => s.calls);
  const privates = useGuilds((s) => s.privates);
  const me = useGuilds((s) => s.me);
  const notify = useGuilds((s) => s.notify);
  const inCall = useCall((s) => (s.status !== "idle" && s.guildId === null ? s.channelId : null));
  const prefs = useNotifyPrefs();
  const ringing = useMemo(
    () => ringingCalls({ me, calls, privates, notify }, inCall),
    [me, calls, privates, notify, inCall],
  );
  const key = ringing.map((p) => p.channel.id).join(",");

  useEffect(() => {
    if (key === "" || !prefs.sound) return;
    return startRingtone();
  }, [key, prefs.sound]);

  useEffect(() => {
    if (key === "" || !prefs.desktop || !windowHidden()) return;
    if (!("Notification" in window) || Notification.permission !== "granted") return;
    const shown = ringing.map((p) => {
      const n = new Notification(privateName(p.channel), {
        body: "Входящий звонок",
        tag: `call-${p.channel.id}`,
        icon: new URL(dmPeer(p.channel)?.avatar_url ?? "/favicon.svg", window.location.origin).href,
      });
      n.onclick = () => {
        window.focus();
        n.close();
      };
      return n;
    });
    return () => {
      for (const n of shown) n.close();
    };
    // `key` names the calls: a new list of calls, a new notification.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, prefs.desktop]);

  if (ringing.length === 0) return null;
  return (
    <div className="pointer-events-none fixed inset-x-0 top-6 z-(--z-dialog) flex flex-col items-center gap-3 px-4">
      {ringing.map((p) => (
        <IncomingCall key={p.channel.id} entry={p} />
      ))}
    </div>
  );
};
