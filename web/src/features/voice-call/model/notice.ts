import type { Browser } from "@/entities/call";
import { onLanguage, t } from "@/shared/i18n";
import type { ScreenShareResult } from "@/shared/lib";

export let FIREFOX_NO_AUDIO = "";
export let WINDOW_NO_AUDIO = "";
export let ECHO_RISK = "";
onLanguage(() => {
  FIREFOX_NO_AUDIO = t("voice:notice.firefoxNoAudio");
  WINDOW_NO_AUDIO = t("voice:notice.windowNoAudio");
  ECHO_RISK = t("voice:notice.echoRisk");
});

/**
 * What to tell the user about the sound of a share they just started, as
 * the browser decides it: Firefox captures none, Chromium none for a
 * window, and for the whole screen the voice chat too unless it can be left
 * out (restrictOwnAudio). Null: nothing to say.
 */
export const shareNotice = (result: ScreenShareResult, browser: Browser, restrictOwnAudio: boolean): string | null => {
  if (!result.ok) return null;
  if (browser === "firefox") return FIREFOX_NO_AUDIO;
  if (result.surface === "window" && !result.audio) return WINDOW_NO_AUDIO;
  if (result.surface === "monitor" && result.audio && !restrictOwnAudio) return ECHO_RISK;
  return null;
};
