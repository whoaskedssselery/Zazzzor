import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type SubmitEvent, useState } from "react";
import { api, errorText, unwrap } from "@/shared/api";
import { t } from "@/shared/i18n";
import { Button, Dialog, DialogClose, DialogContent, Field, FormAlert, Input, useToast } from "@/shared/ui";

const FORM_ID = "delete-guild-form";

/**
 * «Удалить сервер», for its owner only (the caller decides). The name must
 * be typed to confirm: deleting takes every channel and message with it.
 * The user goes home at once, before the GUILD_DELETE the server sends.
 */
export const DeleteGuild = ({ guildId, guildName }: { guildId: string; guildName: string }) => {
  const [open, setOpen] = useState(false);
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <Button
        variant="ghost"
        className="w-full justify-start text-fg-danger"
        onClick={() => {
          setOpen(true);
        }}
      >
        {t("guild:deleteGuild.button")}
      </Button>
      {open && <DeleteForm guildId={guildId} guildName={guildName} />}
    </Dialog>
  );
};

const DeleteForm = ({ guildId, guildName }: { guildId: string; guildName: string }) => {
  const [typed, setTyped] = useState("");
  const toast = useToast();
  const navigate = useNavigate();
  const remove = useMutation({
    mutationFn: async () => {
      unwrap(await api.DELETE("/guilds/{guild_id}", { params: { path: { guild_id: guildId } } }));
    },
    onSuccess: () => {
      toast.show({ text: t("guild:deleteGuild.deleted", { name: guildName }) });
      void navigate({ to: "/channels/@me" });
    },
  });
  const matches = typed.trim() === guildName;
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (matches && !remove.isPending) remove.mutate();
  };
  return (
    <DialogContent
      title={t("guild:deleteGuild.title", { name: guildName })}
      description={t("guild:deleteGuild.text")}
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">{t("common:cancel")}</Button>
          </DialogClose>
          <Button type="submit" form={FORM_ID} variant="danger" disabled={!matches} loading={remove.isPending}>
            {t("guild:deleteGuild.button")}
          </Button>
        </>
      }
    >
      <form id={FORM_ID} onSubmit={submit} noValidate>
        {remove.isError && <FormAlert>{errorText(remove.error)}</FormAlert>}
        <Field htmlFor="delete-guild-name" label={t("guild:deleteGuild.typeName")}>
          <Input
            autoComplete="off"
            autoFocus
            value={typed}
            onChange={(e) => {
              setTyped(e.target.value);
            }}
          />
        </Field>
      </form>
    </DialogContent>
  );
};
