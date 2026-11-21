import { ArrowLeftRight, Monitor, MonitorOff } from "lucide-react";
import { canShareScreen, useCall } from "@/entities/call";
import { computeChannel, has, PERM, useGuilds } from "@/entities/guild";
import { t } from "@/shared/i18n";
import { cn, DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger, IconButton } from "@/shared/ui";
import { NO_STREAM, startScreenShare, stopScreenShare, switchScreenShare } from "../model/session";

/**
 * «Демонстрация экрана» in the call panel; locked without STREAM in the
 * call's channel, absent where the browser cannot share (a phone), but a
 * share in progress can always be stopped. During a share it opens a menu,
 * as Discord's: share something else, or stop.
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
  if (!sharing) {
    return (
      <IconButton
        label={t("voice:share.title")}
        icon={Monitor}
        className={className}
        onClick={() => {
          void startScreenShare();
        }}
      />
    );
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <IconButton
          label={t("voice:share.title")}
          icon={Monitor}
          tooltip={false}
          aria-pressed
          className={cn(className, "text-fg-success")}
        />
      </DropdownMenuTrigger>
      <DropdownMenuContent side="top" align="start">
        {/* Only from a pick that can be granted: STREAM taken away mid-share still allows stopping. */}
        {allowed && (
          <DropdownMenuItem
            onSelect={() => {
              void switchScreenShare();
            }}
          >
            {t("voice:share.switch")}
            <ArrowLeftRight aria-hidden />
          </DropdownMenuItem>
        )}
        <DropdownMenuItem
          variant="danger"
          onSelect={() => {
            void stopScreenShare();
          }}
        >
          {t("voice:share.stop")}
          <MonitorOff aria-hidden />
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
};
