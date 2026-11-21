import { useCall } from "@/entities/call";
import { myVoice, useGuilds } from "@/entities/guild";
import { onLanguage, t } from "@/shared/i18n";
import { setSelfDeaf, setSelfMute } from "./session";

export let MODERATOR_MUTED = "";
export let MODERATOR_DEAFENED = "";
onLanguage(() => {
  MODERATOR_MUTED = t("voice:switches.modMuted");
  MODERATOR_DEAFENED = t("voice:switches.modDeafened");
});

/**
 * The user's microphone and sound switches as Discord has them, for the user
 * panel and the call's own controls: what is off, whether a moderator locked
 * it, and the labels and toggles. The microphone while deafened brings the
 * sound back too.
 */
export const useSelfSwitches = () => {
  const selfMute = useCall((s) => s.selfMute);
  const selfDeaf = useCall((s) => s.selfDeaf);
  const voice = useGuilds(myVoice);
  const modMute = voice?.mute ?? false;
  const modDeaf = voice?.deaf ?? false;
  const micOff = selfMute || selfDeaf || modMute;
  const soundOff = selfDeaf || modDeaf;
  return {
    mic: {
      off: micOff,
      locked: modMute,
      label: modMute ? MODERATOR_MUTED : micOff ? t("voice:switches.micOn") : t("voice:switches.micOff"),
      toggle: () => {
        if (selfDeaf) {
          setSelfDeaf(false);
          setSelfMute(false);
        } else setSelfMute(!selfMute);
      },
    },
    sound: {
      off: soundOff,
      locked: modDeaf,
      label: modDeaf ? MODERATOR_DEAFENED : soundOff ? t("voice:switches.soundOn") : t("voice:switches.soundOff"),
      toggle: () => {
        setSelfDeaf(!selfDeaf);
      },
    },
  };
};
