import { Monitor, MonitorOff } from "lucide-react";
import { canShareScreen, useCall } from "@/entities/call";
import { computeChannel, has, PERM, useGuilds } from "@/entities/guild";
import { t } from "@/shared/i18n";
import { cn, IconButton } from "@/shared/ui";
import { NO_STREAM, startScreenShare, stopScreenShare } from "../model/session";

/**
 * «Демонстрация экрана» in the call panel; locked without STREAM in the
 * call's channel, absent where the browser cannot share (a phone), but a
 * share in progress can always be stopped.
 */
export const ScreenShareButton = ({ className }: { className?: string }) => {
  const sharing = useCall((s) => s.sharing);
  const guildId = useCall((s) => s.guildId);
  const channelId = useCall((s) => s.channelId);
  const allowed = useGuilds((s) => {
    // In a DM or a group everyone may share and film.
    if (guildId === null) return channelId !== null && channelId in s.privates;
    const entry = s.guilds[guildId];
    if (!entry || channelId === null || s.me === null) return false;
    return has(computeChannel(entry, channelId, s.me.id), PERM.STREAM);
  });
  if (!sharing && !canShareScreen()) return null;
  if (!allowed && !sharing) return <IconButton label={NO_STREAM} icon={Monitor} className={className} disabled />;
  return (
    <IconButton
      label={sharing ? t("voice:share.stop") : t("voice:share.title")}
      icon={sharing ? MonitorOff : Monitor}
      aria-pressed={sharing}
      className={cn(className, sharing && "text-fg-success")}
      onClick={() => {
        void (sharing ? stopScreenShare() : startScreenShare());
      }}
    />
  );
};
