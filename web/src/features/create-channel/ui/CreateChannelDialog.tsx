import { useMutation } from "@tanstack/react-query";
import { Hash, Volume2 } from "lucide-react";
import { type SubmitEvent, useState } from "react";
import { homeCategory } from "@/entities/channel";
import { useGuilds } from "@/entities/guild";
import { ApiError, api, errorText, unwrap } from "@/shared/api";
import { t } from "@/shared/i18n";
import { runeLength, trimContent } from "@/shared/lib";
import { Button, cn, Dialog, DialogClose, DialogContent, Field, FormAlert, Input } from "@/shared/ui";

/** What is created: a text or voice channel, or a category. */
export type NewChannelKind = "channel" | "category";

const TEXT = 0 as const;
const VOICE = 2 as const;
const CATEGORY = 4 as const;
type ChannelType = typeof TEXT | typeof VOICE | typeof CATEGORY;

interface CreateChannelDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  guildId: string;
  kind: NewChannelKind;
  /**
   * The category a new channel goes into. Null: the category that already
   * holds channels of the chosen type, or the top level when none does.
   */
  parent: { id: string; name: string } | null;
}

/**
 * «Создать канал» (text or voice, in a category or with the channels of its
 * type) and
 * «Создать категорию». The server puts it last; a channel in a category
 * starts synced with it.
 */
export const CreateChannelDialog = ({ open, onOpenChange, guildId, kind, parent }: CreateChannelDialogProps) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    {open && (
      <CreateForm
        guildId={guildId}
        kind={kind}
        parent={parent}
        onDone={() => {
          onOpenChange(false);
        }}
      />
    )}
  </Dialog>
);

const FORM_ID = "create-channel-form";

const TypeOption = ({
  type,
  current,
  onChange,
}: {
  type: typeof TEXT | typeof VOICE;
  current: ChannelType;
  onChange: (t: ChannelType) => void;
}) => {
  const Icon = type === TEXT ? Hash : Volume2;
  const id = `channel-type-${String(type)}`;
  return (
    <label
      htmlFor={id}
      className={cn(
        "flex cursor-pointer items-center gap-3 rounded-md bg-input px-3 py-2.5 transition-colors hover:bg-control",
        current === type && "bg-control ring-1 ring-border-strong",
      )}
    >
      <Icon aria-hidden className="size-6 shrink-0 text-fg-muted" strokeWidth={1.75} />
      <span className="flex-1">
        <span className="block text-base font-medium">
          {type === TEXT ? t("guild:createChannel.text") : t("guild:createChannel.voice")}
        </span>
        <span className="block text-sm text-fg-muted">
          {type === TEXT ? t("guild:createChannel.textHint") : t("guild:createChannel.voiceHint")}
        </span>
      </span>
      <input
        id={id}
        type="radio"
        name="channel-type"
        className="size-4 accent-(--accent)"
        checked={current === type}
        onChange={() => {
          onChange(type);
        }}
      />
    </label>
  );
};

const CreateForm = ({
  guildId,
  kind,
  parent,
  onDone,
}: {
  guildId: string;
  kind: NewChannelKind;
  parent: { id: string; name: string } | null;
  onDone: () => void;
}) => {
  const [type, setType] = useState<ChannelType>(kind === "category" ? CATEGORY : TEXT);
  // From the server menu a channel joins the channels of its type (text with text, voice with voice).
  const home = useGuilds((s) => {
    const entry = s.guilds[guildId];
    return parent !== null || kind === "category" || !entry ? null : homeCategory(entry, type);
  });
  const into = kind === "category" ? null : (parent ?? (home ? { id: home.id, name: home.name } : null));
  const [name, setName] = useState("");
  const [fieldError, setFieldError] = useState<string | undefined>(undefined);
  const create = useMutation({
    mutationFn: async (value: string) =>
      unwrap(
        await api.POST("/guilds/{guild_id}/channels", {
          params: { path: { guild_id: guildId } },
          body: { name: value, type, parent_id: into?.id ?? null },
        }),
      ),
    // The list shows it with its CHANNEL_CREATE.
    onSuccess: onDone,
    onError: (e) => {
      if (e instanceof ApiError && "name" in e.fields) setFieldError(t("guild:createChannel.nameRule"));
    },
  });

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const value = trimContent(name);
    const length = runeLength(value);
    if (length < 1 || length > 100) {
      setFieldError(t("guild:createChannel.nameRule"));
      return;
    }
    setFieldError(undefined);
    create.mutate(value);
  };

  const failed = create.isError && !(create.error instanceof ApiError && "name" in create.error.fields);
  return (
    <DialogContent
      title={kind === "category" ? t("guild:createChannel.createCategory") : t("guild:createChannel.createChannel")}
      {...(into ? { description: t("guild:createChannel.inCategory", { name: into.name }) } : {})}
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">{t("common:cancel")}</Button>
          </DialogClose>
          <Button type="submit" form={FORM_ID} loading={create.isPending}>
            {t("guild:createChannel.create")}
          </Button>
        </>
      }
    >
      {failed && <FormAlert>{errorText(create.error)}</FormAlert>}
      <form id={FORM_ID} noValidate onSubmit={submit} className="flex flex-col gap-4">
        {kind === "channel" && (
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-2 text-sm font-semibold text-fg-muted">{t("guild:createChannel.type")}</legend>
            <TypeOption type={TEXT} current={type} onChange={setType} />
            <TypeOption type={VOICE} current={type} onChange={setType} />
          </fieldset>
        )}
        <Field
          htmlFor="new-channel-name"
          label={kind === "category" ? t("guild:createChannel.categoryName") : t("guild:createChannel.channelName")}
          error={fieldError}
        >
          <Input
            value={name}
            maxLength={200}
            autoComplete="off"
            autoFocus
            onChange={(e) => {
              setName(e.currentTarget.value);
            }}
          />
        </Field>
      </form>
    </DialogContent>
  );
};
