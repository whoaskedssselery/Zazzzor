import { useMutation } from "@tanstack/react-query";
import { useRef, useState } from "react";
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

/**
 * «Пригласить друзей»: choose how long the link lives and how many joins it
 * allows, create it, and it is copied. When the clipboard is not available
 * (plain HTTP, a denied permission) the link is selected in its read-only
 * field to copy by hand.
 */
export const InviteDialog = ({ open, onOpenChange, channelId, channelName }: InviteDialogProps) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    {open && <InviteForm channelId={channelId} channelName={channelName} />}
  </Dialog>
);

const InviteForm = ({ channelId, channelName }: { channelId: string; channelName: string }) => {
  const [maxAge, setMaxAge] = useState("604800");
  const [maxUses, setMaxUses] = useState("0");
  const [link, setLink] = useState<string | null>(null);
  const field = useRef<HTMLInputElement>(null);
  const action = useRef<HTMLButtonElement>(null);

  // No clipboard (HTTP) or no permission: the link is selected to copy by hand.
  const { state: copied, copy } = useCopy(() => {
    field.current?.focus();
    field.current?.select();
  });

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/channels/{channel_id}/invites", {
          params: { path: { channel_id: channelId } },
          body: { max_age_seconds: Number(maxAge), max_uses: Number(maxUses) },
        }),
      ),
    onSuccess: async (invite) => {
      const url = inviteLink(invite.code);
      setLink(url);
      // The field must show the link before it can be selected.
      await new Promise((resolve) => requestAnimationFrame(resolve));
      await copy(url);
    },
  });

  return (
    <DialogContent
      title={`Пригласить друзей в #${channelName}`}
      description="Отправьте ссылку другу: по ней он попадёт на сервер, сразу в этот канал."
      size="sm"
      // The action is the point of the dialog: it takes the first focus, not the close button.
      onOpenAutoFocus={(e) => {
        e.preventDefault();
        action.current?.focus();
      }}
    >
      {create.isError && <FormAlert>{errorText(create.error)}</FormAlert>}
      <div className="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
        <Field htmlFor="invite-max-age" label="Срок действия">
          <Select options={LIFETIMES} value={maxAge} onValueChange={setMaxAge} />
        </Field>
        <Field htmlFor="invite-max-uses" label="Число использований">
          <Select options={USES} value={maxUses} onValueChange={setMaxUses} />
        </Field>
      </div>
      {link !== null && (
        <Field
          htmlFor="invite-link"
          label="Ссылка-приглашение"
          className="mt-5"
          {...(copied === "failed" ? { hint: COPY_FAILED } : {})}
        >
          <div className="flex gap-2">
            <Input
              ref={field}
              value={link}
              readOnly
              spellCheck={false}
              className="font-mono text-sm"
              onFocus={(e) => {
                e.currentTarget.select();
              }}
            />
            <CopyButton
              state={copied}
              onClick={() => {
                void copy(link);
              }}
            />
          </div>
        </Field>
      )}
      <Button
        ref={action}
        className="mt-5 w-full"
        variant={link === null ? "primary" : "secondary"}
        loading={create.isPending}
        onClick={() => {
          if (!create.isPending) create.mutate();
        }}
      >
        {link === null ? "Создать ссылку" : "Создать новую ссылку"}
      </Button>
    </DialogContent>
  );
};
