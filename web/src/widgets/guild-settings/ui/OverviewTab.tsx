import { useMutation } from "@tanstack/react-query";
import { type ChangeEvent, useEffect, useRef, useState } from "react";
import type { Guild } from "@/entities/guild";
import { ApiError, api, errorText, unwrap } from "@/shared/api";
import { t } from "@/shared/i18n";
import { fileForm, IMAGE_ACCEPT, imageProblem, runeLength, trimContent, useObjectUrl } from "@/shared/lib";
import { Button, Field, FormAlert, GuildIcon, Input, SaveBar, useToast } from "@/shared/ui";

/** PUT /guilds/{id}/icon with the file as multipart `file`. */
const uploadIcon = async (guildId: string, file: File) =>
  unwrap(
    await api.PUT("/guilds/{guild_id}/icon", {
      params: { path: { guild_id: guildId } },
      body: { file: "" },
      bodySerializer: fileForm(file),
    }),
  );

/**
 * The server's name and icon. Changes wait in the form until «Сохранить
 * изменения»; the bar that offers it also says they are unsaved, and the
 * settings ask before leaving them (`onDirtyChange`). The store follows the
 * server's GUILD_UPDATE; the form starts over from it once saved.
 */
export const OverviewTab = ({ guild, onDirtyChange }: { guild: Guild; onDirtyChange: (dirty: boolean) => void }) => {
  const [name, setName] = useState(guild.name);
  const [icon, setIcon] = useState<File | null>(null);
  const [nameError, setNameError] = useState<string | undefined>(undefined);
  const [iconError, setIconError] = useState<string | null>(null);
  const preview = useObjectUrl(icon);
  const picker = useRef<HTMLInputElement>(null);
  const toast = useToast();

  // A rename by someone else shows here while this form has nothing of its own.
  const [base, setBase] = useState(guild.name);
  if (base !== guild.name) {
    setBase(guild.name);
    if (trimContent(name) === base) setName(guild.name);
  }

  const dirty = trimContent(name) !== guild.name || icon !== null;
  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

  const save = useMutation({
    // Takes the icon it sends: another may be picked while this one uploads.
    mutationFn: async (sent: File | null) => {
      const value = trimContent(name);
      if (value !== guild.name) {
        unwrap(
          await api.PATCH("/guilds/{guild_id}", { params: { path: { guild_id: guild.id } }, body: { name: value } }),
        );
      }
      if (sent !== null) await uploadIcon(guild.id, sent);
    },
    onSuccess: (_, sent) => {
      setIcon((current) => (current === sent ? null : current));
      toast.show({ text: t("guild:overview.saved"), tone: "success" });
    },
    onError: (e) => {
      if (e instanceof ApiError && "name" in e.fields) setNameError(t("guild:overview.nameRule"));
    },
  });

  const pick = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = "";
    if (!file) return;
    const problem = imageProblem(file);
    setIconError(problem);
    if (problem === null) setIcon(file);
  };

  const submit = () => {
    const length = runeLength(trimContent(name));
    if (length < 2 || length > 100) {
      setNameError(t("guild:overview.nameRule"));
      return;
    }
    setNameError(undefined);
    save.mutate(icon);
  };

  const reset = () => {
    setName(guild.name);
    setIcon(null);
    setNameError(undefined);
    setIconError(null);
    save.reset();
  };

  const failed = save.isError && !(save.error instanceof ApiError && "name" in save.error.fields);
  return (
    <section aria-labelledby="overview-title">
      <h2 id="overview-title" className="mb-5 text-xl font-semibold">
        {t("guild:overview.title")}
      </h2>
      {failed && <FormAlert>{errorText(save.error)}</FormAlert>}
      <div className="flex gap-8 max-sm:flex-col">
        <div className="flex flex-col items-center gap-3">
          <GuildIcon name={trimContent(name) || guild.name} src={preview ?? guild.icon_url} size={80} active />
          <input
            ref={picker}
            type="file"
            accept={IMAGE_ACCEPT}
            className="sr-only"
            tabIndex={-1}
            aria-hidden
            onChange={pick}
          />
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              picker.current?.click();
            }}
          >
            {t("guild:overview.changeIcon")}
          </Button>
          <p className="max-w-40 text-center text-xs text-fg-muted">{t("guild:overview.iconRule")}</p>
          {iconError && (
            <p role="alert" className="max-w-40 text-center text-xs text-fg-danger">
              {iconError}
            </p>
          )}
        </div>
        <form
          className="min-w-0 flex-1"
          noValidate
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <Field htmlFor="guild-name" label={t("guild:overview.name")} error={nameError}>
            <Input
              value={name}
              maxLength={200}
              autoComplete="off"
              onChange={(e) => {
                setName(e.currentTarget.value);
              }}
            />
          </Field>
        </form>
      </div>
      {dirty && <SaveBar pending={save.isPending} onReset={reset} onSave={submit} />}
    </section>
  );
};
