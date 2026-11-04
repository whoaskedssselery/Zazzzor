import { useCall } from "@/entities/call";
import { myVoice, useGuilds } from "@/entities/guild";
import { setSelfDeaf, setSelfMute } from "./session";

export const MODERATOR_MUTED = "Модератор выключил вам микрофон";
export const MODERATOR_DEAFENED = "Модератор выключил вам звук";

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
      label: modMute ? MODERATOR_MUTED : micOff ? "Включить микрофон" : "Выключить микрофон",
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
      label: modDeaf ? MODERATOR_DEAFENED : soundOff ? "Включить звук" : "Выключить звук",
      toggle: () => {
        setSelfDeaf(!selfDeaf);
      },
    },
  };
};
