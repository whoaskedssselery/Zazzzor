import { useEffect } from "react";
import { useToast } from "@/shared/ui";
import { onCallNotice } from "../model/session";

/** Shows what the voice call has to say (why it ended, a refused microphone) as toasts. */
export const CallNotices = () => {
  const toast = useToast();
  useEffect(
    () =>
      onCallNotice((text) => {
        toast.show({ text });
      }),
    [toast],
  );
  return null;
};
