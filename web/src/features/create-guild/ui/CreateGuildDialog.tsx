import { useMutation } from "@tanstack/react-query";
import { type SubmitEvent, useState } from "react";
import { useNavigateToFallback } from "@/entities/channel";
import { waitForGuild } from "@/entities/guild";
import { ApiError, api, errorText, unwrap } from "@/shared/api";
import { runeLength, trimContent } from "@/shared/lib";
import { Button, Dialog, DialogClose, DialogContent, Field, FormAlert, Input } from "@/shared/ui";

const NAME_RULE = "Название: от 2 до 100 символов.";

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
  const openGuild = useNavigateToFallback();

  // The server is announced over the gateway (GUILD_CREATE) a moment after
  // the answer; it opens once the store holds it.
  const create = useMutation({
    mutationFn: async (value: string) => {
      const guild = unwrap(await api.POST("/guilds", { body: { name: value } }));
      await waitForGuild(guild.id);
      return guild.id;
    },
    onSuccess: (guildId) => {
      onDone();
      openGuild(guildId);
    },
    onError: (e) => {
      if (e instanceof ApiError && "name" in e.fields) setFieldError(NAME_RULE);
      else setAlert(errorText(e));
    },
  });

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (create.isPending) return;
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
            <Button variant="ghost">Отмена</Button>
          </DialogClose>
          <Button type="submit" form={FORM_ID} loading={create.isPending}>
            Создать
          </Button>
        </>
      }
    >
      {alert && <FormAlert>{alert}</FormAlert>}
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
