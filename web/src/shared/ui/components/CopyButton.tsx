import { Check, Copy } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Button } from "./Button";

export type CopyState = "idle" | "copied" | "failed";

export const COPIED = "Скопировано";
/** How long the button says it copied before it reads «Копировать» again. */
const SHOWN_MS = 2000;

/**
 * Copies text to the clipboard and remembers how it went for a moment, for
 * a CopyButton. Without a clipboard (plain HTTP, a denied permission) the
 * state is "failed" until the next try and `onFailed` runs: select the text
 * there, so it can be copied by hand.
 */
export const useCopy = (onFailed?: () => void) => {
  const [state, setState] = useState<CopyState>("idle");
  const timer = useRef<number | undefined>(undefined);
  useEffect(
    () => () => {
      window.clearTimeout(timer.current);
    },
    [],
  );
  const copy = useCallback(
    async (text: string) => {
      window.clearTimeout(timer.current);
      try {
        await navigator.clipboard.writeText(text);
      } catch {
        // Stays until the next try: the text has to be copied by hand meanwhile.
        setState("failed");
        onFailed?.();
        return;
      }
      setState("copied");
      timer.current = window.setTimeout(() => {
        setState("idle");
      }, SHOWN_MS);
    },
    [onFailed],
  );
  return { state, copy };
};

/**
 * «Копировать» beside a read-only field, as in Discord: it turns into
 * «Скопировано» for a moment rather than raising a toast, so a dialog it
 * sits in still closes on the first Escape. The result is announced too.
 */
export const CopyButton = ({
  state,
  onClick,
  primary = false,
  disabled = false,
}: {
  state: CopyState;
  onClick: () => void;
  /** The main action of its dialog: in the accent colour before it copies too. */
  primary?: boolean;
  disabled?: boolean;
}) => {
  const copied = state === "copied";
  const Icon = copied ? Check : Copy;
  return (
    <>
      <Button
        variant={copied || primary ? "primary" : "secondary"}
        className="min-w-36"
        disabled={disabled}
        onClick={onClick}
      >
        <Icon aria-hidden className="size-4" />
        {copied ? COPIED : "Копировать"}
      </Button>
      <span role="status" className="sr-only">
        {copied ? COPIED : ""}
      </span>
    </>
  );
};
