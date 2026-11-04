import { useMutation } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { api, errorText, unwrap } from "@/shared/api";
import { Button, CopyButton, Dialog, DialogContent, Field, FormAlert, Input, Select, useCopy } from "@/shared/ui";

/** Lifetimes of an invite, in seconds (0: never expires), as Discord offers them. */
const LIFETIMES = [
  { value: "1800", label: "30 минут" },
  { value: "3600", label: "1 час" },
  { value: "21600", label: "6 часов" },
  { value: "43200", label: "12 часов" },
  { value: "86400", label: "1 день" },
  { value: "604800", label: "7 дней" },
  { value: "0", label: "Никогда" },
];

/** How many joins an invite allows (0: no limit). */
const USES = [
  { value: "0", label: "Без ограничений" },
  ...[1, 5, 10, 25, 50, 100].map((n) => ({ value: String(n), label: String(n) })),
];

export const COPY_FAILED = "Не получилось скопировать: ссылка выделена, скопируйте её вручную.";

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
  const when = maxAge === "0" ? "Ссылка не истечёт" : `Ссылка перестанет действовать через ${life}`;
  return maxUses === "0" ? `${when}.` : `${when} или после ${maxUses} ${maxUses === "1" ? "входа" : "входов"}.`;
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
      // «Копировать» is the point of the dialog: it takes the focus once it has a link.
      await new Promise((resolve) => requestAnimationFrame(resolve));
      copyButton.current?.querySelector("button")?.focus();
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
      title={`Пригласить друзей в #${channelName}`}
      description={`Друзья попадут на сервер, сразу в #${channelName}.`}
      size="sm"
    >
      {create.isError && <FormAlert>{errorText(create.error)}</FormAlert>}
      <Field
        htmlFor="invite-link"
        label="Отправьте другу ссылку-приглашение"
        {...(copied === "failed" ? { hint: COPY_FAILED } : {})}
      >
        <div className="flex gap-2 rounded-md">
          <Input
            ref={field}
            value={link ?? ""}
            placeholder={create.isPending ? "Создаём ссылку…" : ""}
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
              Изменить ссылку-приглашение
            </button>
          )}
        </p>
      )}
      {editing && (
        <div className="mt-4 flex animate-rise-in flex-col gap-3 rounded-md bg-sidebar p-3">
          <div className="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
            <Field htmlFor="invite-max-age" label="Срок действия">
              <Select options={LIFETIMES} value={maxAge} onValueChange={setMaxAge} />
            </Field>
            <Field htmlFor="invite-max-uses" label="Число использований">
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
              Отмена
            </Button>
            <Button
              loading={create.isPending}
              onClick={() => {
                if (!create.isPending) mutate({ maxAge, maxUses });
              }}
            >
              Создать новую ссылку
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
          Попробовать ещё раз
        </Button>
      )}
    </DialogContent>
  );
};
