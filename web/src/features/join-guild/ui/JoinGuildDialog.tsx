import { type SubmitEvent, useState } from "react";
import { t } from "@/shared/i18n";
import { Button, Dialog, DialogClose, DialogContent, Field, FormAlert, Input } from "@/shared/ui";
import { inviteErrorText, JOINED_LATE, parseInviteCode, previewInvite, useJoinInvite } from "../model/invite";

interface JoinGuildDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** «Присоединиться к серверу»: a link or a code, then the server opens on the invite's channel. */
export const JoinGuildDialog = ({ open, onOpenChange }: JoinGuildDialogProps) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    {open && (
      <JoinForm
        onDone={() => {
          onOpenChange(false);
        }}
      />
    )}
  </Dialog>
);

const FORM_ID = "join-guild-form";

const JoinForm = ({ onDone }: { onDone: () => void }) => {
  const [input, setInput] = useState("");
  const [fieldError, setFieldError] = useState<string | undefined>(undefined);
  const [alert, setAlert] = useState<string | null>(null);
  const [previewing, setPreviewing] = useState(false);
  const join = useJoinInvite(onDone);
  const busy = previewing || join.pending;
  // A failed step says why; a server that is late says it will open by itself.
  const message = alert ?? (join.error ? inviteErrorText(join.error) : join.late ? JOINED_LATE : null);

  const submit = async (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (busy) return;
    setAlert(null);
    const code = parseInviteCode(input);
    if (code === null) {
      setFieldError(t("guild:join.notAnInvite"));
      return;
    }
    setFieldError(undefined);
    setPreviewing(true);
    try {
      const invite = await previewInvite(code);
      join.join({ code, channelId: invite.channel.id });
    } catch (err) {
      setAlert(inviteErrorText(err));
    } finally {
      setPreviewing(false);
    }
  };

  return (
    <DialogContent
      title={t("guild:join.title")}
      description={t("guild:join.text")}
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">{t("common:cancel")}</Button>
          </DialogClose>
          <Button type="submit" form={FORM_ID} loading={busy}>
            {t("guild:join.join")}
          </Button>
        </>
      }
    >
      {message !== null && <FormAlert>{message}</FormAlert>}
      <form id={FORM_ID} noValidate onSubmit={(e) => void submit(e)}>
        <Field htmlFor="join-guild-invite" label={t("guild:join.label")} hint={t("guild:join.hint")} error={fieldError}>
          <Input
            value={input}
            onChange={(e) => {
              setInput(e.currentTarget.value);
            }}
            autoComplete="off"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            autoFocus
          />
        </Field>
      </form>
    </DialogContent>
  );
};
