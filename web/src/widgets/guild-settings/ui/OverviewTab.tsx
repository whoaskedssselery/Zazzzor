import { useMutation } from "@tanstack/react-query";
import { type ChangeEvent, useEffect, useMemo, useRef, useState } from "react";
import type { Guild } from "@/entities/guild";
import { ApiError, api, errorText, unwrap } from "@/shared/api";
import { runeLength, trimContent } from "@/shared/lib";
import { Button, Field, FormAlert, GuildIcon, Input, SaveBar, useToast } from "@/shared/ui";

const NAME_RULE = "Название: от 2 до 100 символов.";
const IMAGE_TYPES = new Set(["image/png", "image/jpeg", "image/gif", "image/webp"]);
const MAX_ICON_BYTES = 4 * 1024 * 1024;
export const WRONG_IMAGE = "Нужна картинка PNG, JPEG, GIF или WebP.";
export const IMAGE_TOO_LARGE = "Файл больше 4 МБ.";

/** A local URL showing `file` while it is chosen, released when it changes. */
const useObjectUrl = (file: File | null): string | null => {
  const url = useMemo(() => (file === null ? null : URL.createObjectURL(file)), [file]);
  useEffect(
    () => () => {
      if (url !== null) URL.revokeObjectURL(url);
    },
    [url],
  );
  return url;
};

/** PUT /guilds/{id}/icon with the file as multipart `file`. */
const uploadIcon = async (guildId: string, file: File) =>
  unwrap(
    await api.PUT("/guilds/{guild_id}/icon", {
      params: { path: { guild_id: guildId } },
      body: { file: "" },
      bodySerializer: () => {
        const form = new FormData();
        form.append("file", file);
        return form;
      },
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
    mutationFn: async () => {
      const value = trimContent(name);
      if (value !== guild.name) {
        unwrap(
          await api.PATCH("/guilds/{guild_id}", { params: { path: { guild_id: guild.id } }, body: { name: value } }),
        );
      }
      if (icon !== null) await uploadIcon(guild.id, icon);
    },
    onSuccess: () => {
      setIcon(null);
      toast.show({ text: "Изменения сохранены", tone: "success" });
    },
    onError: (e) => {
      if (e instanceof ApiError && "name" in e.fields) setNameError(NAME_RULE);
    },
  });

  const pick = (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.currentTarget.files?.[0];
    e.currentTarget.value = "";
    if (!file) return;
    if (!IMAGE_TYPES.has(file.type)) setIconError(WRONG_IMAGE);
    else if (file.size > MAX_ICON_BYTES) setIconError(IMAGE_TOO_LARGE);
    else {
      setIconError(null);
      setIcon(file);
    }
  };

  const submit = () => {
    const length = runeLength(trimContent(name));
    if (length < 2 || length > 100) {
      setNameError(NAME_RULE);
      return;
    }
    setNameError(undefined);
    save.mutate();
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
        Обзор сервера
      </h2>
      {failed && <FormAlert>{errorText(save.error)}</FormAlert>}
      <div className="flex gap-8 max-sm:flex-col">
        <div className="flex flex-col items-center gap-3">
          <GuildIcon name={trimContent(name) || guild.name} src={preview ?? guild.icon_url} size={80} active />
          <input
            ref={picker}
            type="file"
            accept="image/png,image/jpeg,image/gif,image/webp"
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
            Сменить иконку
          </Button>
          <p className="max-w-40 text-center text-xs text-fg-muted">Квадрат не меньше 128×128, до 4 МБ.</p>
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
          <Field htmlFor="guild-name" label="Название сервера" error={nameError}>
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
