import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import type { Channel } from "@/entities/guild";
import { ApiError, api, errorText, unwrap } from "@/shared/api";
import { runeLength, trimContent } from "@/shared/lib";
import { Button, ConfirmDialog, Field, FormAlert, Input, SaveBar, Textarea } from "@/shared/ui";

const NAME_RULE = "Название: от 1 до 100 символов.";
const TOPIC_RULE = "Тема: до 1024 символов.";
const TEXT = 0;
const CATEGORY = 4;

/**
 * A channel's name, its topic (text channels only) and its deletion. Changes
 * wait for «Сохранить изменения»; the store follows the CHANNEL_UPDATE.
 * Deleting asks first and leaves for the server.
 */
export const ChannelOverview = ({
  channel,
  onDirtyChange,
}: {
  channel: Channel;
  onDirtyChange: (dirty: boolean) => void;
}) => {
  const [name, setName] = useState(channel.name);
  const [topic, setTopic] = useState(channel.topic ?? "");
  const [nameError, setNameError] = useState<string | undefined>(undefined);
  const [topicError, setTopicError] = useState<string | undefined>(undefined);
  const [deleting, setDeleting] = useState(false);
  const navigate = useNavigate();
  const hasTopic = channel.type === TEXT;
  const isCategory = channel.type === CATEGORY;

  const nextName = trimContent(name);
  const nextTopic = trimContent(topic);
  const changes = {
    ...(nextName !== channel.name ? { name: nextName } : {}),
    ...(hasTopic && nextTopic !== (channel.topic ?? "") ? { topic: nextTopic === "" ? null : nextTopic } : {}),
  };
  const dirty = Object.keys(changes).length > 0;
  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

  const path = { channel_id: channel.id };
  const save = useMutation({
    mutationFn: async () => unwrap(await api.PATCH("/channels/{channel_id}", { params: { path }, body: changes })),
    onError: (e) => {
      if (e instanceof ApiError && "name" in e.fields) setNameError(NAME_RULE);
      if (e instanceof ApiError && "topic" in e.fields) setTopicError(TOPIC_RULE);
    },
  });
  const remove = useMutation({
    mutationFn: async () => {
      unwrap(await api.DELETE("/channels/{channel_id}", { params: { path } }));
    },
    onSuccess: () => {
      void navigate({ to: "/channels/$guildId", params: { guildId: channel.guild_id } });
    },
  });

  const submit = () => {
    const length = runeLength(nextName);
    const nameBad = length < 1 || length > 100;
    const topicBad = runeLength(nextTopic) > 1024;
    setNameError(nameBad ? NAME_RULE : undefined);
    setTopicError(topicBad ? TOPIC_RULE : undefined);
    if (!nameBad && !topicBad) save.mutate();
  };

  const error = save.error ?? remove.error;
  const fieldError = error instanceof ApiError && ("name" in error.fields || "topic" in error.fields);
  const kind = isCategory ? "категорию" : "канал";
  return (
    <section aria-labelledby="channel-overview-title">
      <h2 id="channel-overview-title" className="mb-5 text-xl font-semibold">
        Обзор
      </h2>
      {error && !fieldError && <FormAlert>{errorText(error)}</FormAlert>}
      <form
        className="flex flex-col gap-5"
        noValidate
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <Field htmlFor="channel-name" label={isCategory ? "Название категории" : "Название канала"} error={nameError}>
          <Input
            value={name}
            maxLength={200}
            autoComplete="off"
            onChange={(e) => {
              setName(e.currentTarget.value);
            }}
          />
        </Field>
        {hasTopic && (
          <Field htmlFor="channel-topic" label="Тема канала" hint="Видна в шапке канала." error={topicError}>
            <Textarea
              value={topic}
              rows={3}
              onChange={(e) => {
                setTopic(e.currentTarget.value);
              }}
            />
          </Field>
        )}
      </form>
      <div className="mt-10 border-t border-border pt-6">
        <Button
          variant="danger"
          onClick={() => {
            setDeleting(true);
          }}
        >
          {`Удалить ${kind}`}
        </Button>
      </div>
      {dirty && (
        <SaveBar
          pending={save.isPending}
          onReset={() => {
            setName(channel.name);
            setTopic(channel.topic ?? "");
            setNameError(undefined);
            setTopicError(undefined);
            save.reset();
          }}
          onSave={submit}
        />
      )}
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={isCategory ? `Удалить категорию «${channel.name}»?` : `Удалить #${channel.name}?`}
        description={
          isCategory ? "Её каналы останутся и переедут наверх списка." : "Все сообщения канала пропадут навсегда."
        }
        confirmLabel={`Удалить ${kind}`}
        tone="danger"
        onConfirm={() => {
          remove.mutate();
          setDeleting(false);
        }}
      />
    </section>
  );
};
