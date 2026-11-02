import { useMutation } from "@tanstack/react-query";
import { type SubmitEvent, useState } from "react";
import { useOpenGuild } from "@/entities/channel";
import { ApiError, api, errorText, unwrap } from "@/shared/api";
import { runeLength, trimContent } from "@/shared/lib";
import { Button, Dialog, DialogClose, DialogContent, Field, FormAlert, Input } from "@/shared/ui";

const NAME_RULE = "Название: от 2 до 100 символов.";

const createdLate = (name: string): string =>
  `Сервер «${name}» создан, но связь прервалась. Он откроется сам, когда подключение восстановится.`;

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
    mutationFn: async (value: string) => unwrap(await api.POST("/guilds", { body: { name: value } })),
    onSuccess: (guild) => {
      arrival.open(guild.id);
    },
    onError: (e) => {
      if (e instanceof ApiError && "name" in e.fields) setFieldError(NAME_RULE);
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
      setFieldError(NAME_RULE);
      return;
    }
    setFieldError(undefined);
    create.mutate(value);
  };

  return (
    <DialogContent
      title="Создать сервер"
      description="Место для вас и ваших друзей. Каналы и роли настроите потом."
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">{overdue ? "Закрыть" : "Отмена"}</Button>
          </DialogClose>
          {!overdue && (
            <Button type="submit" form={FORM_ID} loading={busy}>
              Создать
            </Button>
          )}
        </>
      }
    >
      {message !== null && <FormAlert>{message}</FormAlert>}
      <form id={FORM_ID} noValidate onSubmit={submit}>
        <Field htmlFor="create-guild-name" label="Название сервера" error={fieldError}>
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
