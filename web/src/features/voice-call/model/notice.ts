import type { Browser } from "@/entities/call";
import type { ScreenShareResult } from "@/shared/lib";

export const FIREFOX_NO_AUDIO = "Firefox не передаёт звук демки — зрители увидят только картинку";
export const WINDOW_NO_AUDIO = "Звук окна — в приложении для Windows или делитесь всем экраном";
export const ECHO_RISK = "Зрители услышат и голосовой чат: наденьте наушники или делитесь вкладкой";

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
