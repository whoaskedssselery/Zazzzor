import { useMutation } from "@tanstack/react-query";
import { type SubmitEvent, useState } from "react";
import { useOpenGuild } from "@/entities/channel";
import { ApiError, api, errorText, unwrap } from "@/shared/api";
import { currentLanguage, t } from "@/shared/i18n";
import { runeLength, trimContent } from "@/shared/lib";
import { Button, Dialog, DialogClose, DialogContent, Field, FormAlert, Input } from "@/shared/ui";

const createdLate = (name: string): string => t("guild:createGuild.createdLate", { name });

interface CreateGuildDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/** «Создать сервер»: a name, then the new server opens on its first text channel. */
export const CreateGuildDialog = ({ open, onOpenChange }: CreateGuildDialogProps) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    {open && (
      <CreateForm
        onDone={() => {
          onOpenChange(false);
        }}
      />
    )}
  </Dialog>
);

const FORM_ID = "create-guild-form";

const CreateForm = ({ onDone }: { onDone: () => void }) => {
  const [name, setName] = useState("");
  const [fieldError, setFieldError] = useState<string | undefined>(undefined);
  const [alert, setAlert] = useState<string | null>(null);
  // The server is announced over the gateway (GUILD_CREATE) a moment after
  // the answer; it opens once the store holds it, closing the dialog.
  const arrival = useOpenGuild(onDone);
  const create = useMutation({
    mutationFn: async (value: string) =>
      unwrap(await api.POST("/guilds", { body: { name: value, locale: currentLanguage() } })),
    onSuccess: (guild) => {
      arrival.open(guild.id);
    },
    onError: (e) => {
      if (e instanceof ApiError && "name" in e.fields) setFieldError(t("guild:createGuild.nameRule"));
      else setAlert(errorText(e));
    },
  });

  const created = create.data;
  const busy = create.isPending || (arrival.waiting && !arrival.late);
  // Created, but its GUILD_CREATE is overdue: say so, and offer no second creation.
  const overdue = created !== undefined && arrival.late;
  const message = overdue ? createdLate(created.name) : alert;

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (busy || created) return;
    setAlert(null);
    const value = trimContent(name);
    const length = runeLength(value);
    if (length < 2 || length > 100) {
      setFieldError(t("guild:createGuild.nameRule"));
      return;
    }
    setFieldError(undefined);
    create.mutate(value);
  };

  return (
    <DialogContent
      title={t("guild:createGuild.title")}
      description={t("guild:createGuild.text")}
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">{overdue ? t("common:close") : t("common:cancel")}</Button>
          </DialogClose>
          {!overdue && (
            <Button type="submit" form={FORM_ID} loading={busy}>
              {t("guild:createChannel.create")}
            </Button>
          )}
        </>
      }
    >
      {message !== null && <FormAlert>{message}</FormAlert>}
      <form id={FORM_ID} noValidate onSubmit={submit}>
        <Field htmlFor="create-guild-name" label={t("guild:createGuild.name")} error={fieldError}>
          <Input
            value={name}
            onChange={(e) => {
              setName(e.currentTarget.value);
            }}
            maxLength={200}
            autoComplete="off"
            autoFocus
          />
        </Field>
      </form>
    </DialogContent>
  );
};
