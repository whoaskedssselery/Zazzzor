import { playSound } from "@/shared/lib";

/** A short two-note chime for a mention (shared/lib/sounds; a burst of mentions chimes once). */
export const playMentionSound = (): void => {
  playSound("mention");
};
