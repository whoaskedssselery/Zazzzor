import { useEffect } from "react";
import { applySwitchKeys, dropSwitchKeys } from "./session";

/** The microphone and sound combinations work while the user is signed in (`on`). */
export const useSwitchKeys = (on: boolean): void => {
  useEffect(() => {
    if (!on) return;
    applySwitchKeys();
    return dropSwitchKeys;
  }, [on]);
};
