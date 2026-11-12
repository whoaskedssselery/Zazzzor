import { Video, VideoOff } from "lucide-react";
import { canUseCamera, useCall } from "@/entities/call";
import { computeChannel, has, PERM, useGuilds } from "@/entities/guild";
import { cn, IconButton } from "@/shared/ui";
import { NO_VIDEO, startCamera, stopCamera } from "../model/session";

/**
 * «Включить камеру» in the call; locked without STREAM in the call's
 * channel (Discord's «Видео» covers both the camera and the screen),
 * absent where the browser has no camera API, but a camera that is on
 * can always be turned off.
 */
export const CameraButton = ({ className }: { className?: string }) => {
  const on = useCall((s) => s.camera);
  const guildId = useCall((s) => s.guildId);
  const channelId = useCall((s) => s.channelId);
  const allowed = useGuilds((s) => {
    // In a DM or a group everyone may share and film.
    if (guildId === null) return channelId !== null && channelId in s.privates;
    const entry = s.guilds[guildId];
    if (!entry || channelId === null || s.me === null) return false;
    return has(computeChannel(entry, channelId, s.me.id), PERM.STREAM);
  });
  if (!on && !canUseCamera()) return null;
  if (!allowed && !on) return <IconButton label={NO_VIDEO} icon={VideoOff} className={className} disabled />;
  return (
    <IconButton
      label={on ? "Выключить камеру" : "Включить камеру"}
      icon={on ? Video : VideoOff}
      aria-pressed={on}
      className={cn(className, on && "text-fg-success")}
      onClick={() => {
        void (on ? stopCamera() : startCamera());
      }}
    />
  );
};
