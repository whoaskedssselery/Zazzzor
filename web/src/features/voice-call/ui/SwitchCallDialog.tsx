import { useCall } from "@/entities/call";
import { type GuildsState, privateName, useGuilds } from "@/entities/guild";
import { t } from "@/shared/i18n";
import { ConfirmDialog } from "@/shared/ui";
import { cancelSwitch, confirmSwitch, useSwitchCall } from "../model/switch-call";

/** A call's place as the user knows it: the voice channel's name, or the DM's. */
const placeName = (s: GuildsState, guildId: string | null, channelId: string | null): string => {
  if (channelId === null) return t("shell:call");
  if (guildId === null) {
    const dm = s.privates[channelId]?.channel;
    return dm ? privateName(dm) : t("shell:call");
  }
  return s.guilds[guildId]?.channels[channelId]?.name ?? t("shell:voiceChannelFallback");
};

/** Asks before leaving the call for another, as Discord does. */
export const SwitchCallDialog = () => {
  const pending = useSwitchCall((s) => s.pending);
  const guildId = useCall((s) => s.guildId);
  const channelId = useCall((s) => s.channelId);
  const from = useGuilds((s) => placeName(s, guildId, channelId));
  const to = useGuilds((s) => (pending ? placeName(s, pending.guildId, pending.channelId) : ""));
  return (
    <ConfirmDialog
      open={pending !== null}
      onOpenChange={(open) => {
        if (!open) cancelSwitch();
      }}
      title={t("voice:switch.title")}
      description={t("voice:switch.text", { from, to })}
      confirmLabel={t("voice:switch.confirm")}
      onConfirm={confirmSwitch}
    />
  );
};
