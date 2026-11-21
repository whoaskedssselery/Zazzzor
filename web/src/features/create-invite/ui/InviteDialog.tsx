import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { api, errorText, unwrap } from "@/shared/api";
import { onLanguage, t } from "@/shared/i18n";
import { Button, CopyButton, Dialog, DialogContent, Field, FormAlert, Input, Select, useCopy } from "@/shared/ui";

const option = (value: string, label: () => string) => ({
  value,
  get label() {
    return label();
  },
});

/** Lifetimes of an invite, in seconds (0: never expires), as Discord offers them. */
const LIFETIMES = [
  option("1800", () => t("guild:invite.30m")),
  option("3600", () => t("guild:invite.1h")),
  option("21600", () => t("guild:invite.6h")),
  option("43200", () => t("guild:invite.12h")),
  option("86400", () => t("guild:invite.1d")),
  option("604800", () => t("guild:invite.7d")),
  option("0", () => t("guild:invite.never")),
];

/** How many joins an invite allows (0: no limit). */
const USES = [
  option("0", () => t("guild:invite.noLimit")),
  ...[1, 5, 10, 25, 50, 100].map((n) => ({ value: String(n), label: String(n) })),
];

export let COPY_FAILED = "";
onLanguage(() => {
  COPY_FAILED = t("guild:invite.copyFailed");
});

/** The link a friend opens: this site's own origin, so it works wherever Zazzzor is hosted. */
const inviteLink = (code: string): string => `${window.location.origin}/invite/${code}`;

interface InviteDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The channel the invite leads to (the user needs CREATE_INVITE in it). */
  channelId: string;
  channelName: string;
}

/** When the link stops working, said under it: «через 7 дней», or that it never does. */
export const lifetimeNote = (maxAge: string, maxUses: string): string => {
  const life = LIFETIMES.find((l) => l.value === maxAge)?.label ?? "";
  const when = maxAge === "0" ? t("guild:invite.noExpiry") : t("guild:invite.expiresIn", { life });
  return maxUses === "0"
    ? t("guild:invite.end", { when })
    : t("guild:invite.orAfter", { when, count: Number(maxUses) });
};

/**
 * «Пригласить друзей», as in Discord: the link is made as the dialog opens
 * (7 days, no limit) and waits beside «Копировать»; «Изменить
 * ссылку-приглашение» opens its lifetime and use limit and makes a new one.
 * When the clipboard is not available (plain HTTP, a denied permission) the
 * link is selected in its read-only field to copy by hand.
 */
export const InviteDialog = ({ open, onOpenChange, channelId, channelName }: InviteDialogProps) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    {open && <InviteForm channelId={channelId} channelName={channelName} />}
  </Dialog>
);

const DEFAULT_AGE = "604800";
const DEFAULT_USES = "0";

const InviteForm = ({ channelId, channelName }: { channelId: string; channelName: string }) => {
  const [maxAge, setMaxAge] = useState(DEFAULT_AGE);
  const [maxUses, setMaxUses] = useState(DEFAULT_USES);
  // What the shown link was made with: the note under it must not follow unsaved choices.
  const [made, setMade] = useState({ maxAge: DEFAULT_AGE, maxUses: DEFAULT_USES });
  const [editing, setEditing] = useState(false);
  const [link, setLink] = useState<string | null>(null);
  const field = useRef<HTMLInputElement>(null);
  const copyButton = useRef<HTMLDivElement>(null);

  // No clipboard (HTTP) or no permission: the link is selected to copy by hand.
  const { state: copied, copy } = useCopy(() => {
    field.current?.focus();
    field.current?.select();
  });

  const create = useMutation({
    mutationFn: async (with_: { maxAge: string; maxUses: string }) =>
      unwrap(
        await api.POST("/channels/{channel_id}/invites", {
          params: { path: { channel_id: channelId } },
          body: { max_age_seconds: Number(with_.maxAge), max_uses: Number(with_.maxUses) },
        }),
      ),
    onSuccess: async (invite, with_) => {
      setLink(inviteLink(invite.code));
      setMade(with_);
      setEditing(false);
      // «Копировать» is the point of the dialog: it takes the focus once it has a link,
      // unless the user moved it meanwhile (a quick copy that failed selects the field).
      const before = document.activeElement;
      await new Promise((resolve) => requestAnimationFrame(resolve));
      if (document.activeElement === before) copyButton.current?.querySelector("button")?.focus();
    },
  });
  const { mutate } = create;

  // The first link is made as the dialog opens; the ref keeps Strict Mode's second run from making another.
  const started = useRef(false);
  useEffect(() => {
    if (started.current) return;
    started.current = true;
    mutate({ maxAge: DEFAULT_AGE, maxUses: DEFAULT_USES });
  }, [mutate]);

  return (
    <DialogContent
      title={t("guild:invite.title", { channel: channelName })}
      description={t("guild:invite.text", { channel: channelName })}
      size="sm"
    >
      {create.isError && <FormAlert>{errorText(create.error)}</FormAlert>}
      <Field
        htmlFor="invite-link"
        label={t("guild:invite.sendLink")}
        {...(copied === "failed" ? { hint: COPY_FAILED } : {})}
      >
        <div className="flex gap-2 rounded-md">
          <Input
            ref={field}
            value={link ?? ""}
            placeholder={create.isPending ? t("guild:invite.creating") : ""}
            readOnly
            spellCheck={false}
            className="font-mono text-sm"
            onFocus={(e) => {
              e.currentTarget.select();
            }}
          />
          <div ref={copyButton} className="contents">
            <CopyButton
              primary
              disabled={link === null}
              state={copied}
              onClick={() => {
                if (link !== null) void copy(link);
              }}
            />
          </div>
        </div>
      </Field>
      {link !== null && (
        <p className="mt-2 text-xs text-fg-faint">
          {lifetimeNote(made.maxAge, made.maxUses)}{" "}
          {!editing && (
            <button
              type="button"
              className="cursor-pointer text-link hover:underline"
              onClick={() => {
                setEditing(true);
              }}
            >
              {t("guild:invite.edit")}
            </button>
          )}
        </p>
      )}
      {editing && (
        <div className="mt-4 flex animate-rise-in flex-col gap-3 rounded-md bg-sidebar p-3">
          <div className="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
            <Field htmlFor="invite-max-age" label={t("guild:invite.lifetime")}>
              <Select options={LIFETIMES} value={maxAge} onValueChange={setMaxAge} />
            </Field>
            <Field htmlFor="invite-max-uses" label={t("guild:invite.uses")}>
              <Select options={USES} value={maxUses} onValueChange={setMaxUses} />
            </Field>
          </div>
          <div className="flex justify-end gap-2">
            <Button
              variant="ghost"
              onClick={() => {
                setMaxAge(made.maxAge);
                setMaxUses(made.maxUses);
                setEditing(false);
              }}
            >
              {t("common:cancel")}
            </Button>
            <Button
              loading={create.isPending}
              onClick={() => {
                if (!create.isPending) mutate({ maxAge, maxUses });
              }}
            >
              {t("guild:invite.createNew")}
            </Button>
          </div>
        </div>
      )}
      {link === null && create.isError && (
        <Button
          className="mt-4 w-full"
          onClick={() => {
            mutate({ maxAge, maxUses });
          }}
        >
          {t("guild:invite.retry")}
        </Button>
      )}
    </DialogContent>
  );
};
