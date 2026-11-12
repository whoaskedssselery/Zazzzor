import { useNavigate } from "@tanstack/react-router";
import { Plus, UserPlus } from "lucide-react";
import { type SubmitEvent, useState } from "react";
import {
  createGroup,
  type DMChannel,
  dmPeer,
  openDirect,
  privateName,
  useAddRecipients,
  useRenameGroup,
} from "@/entities/guild";
import { Button, Dialog, DialogClose, DialogContent, Field, IconButton, Input } from "@/shared/ui";
import { PickFriendsDialog } from "./PickFriendsDialog";

/** People in a group at most, the owner included. */
export const GROUP_LIMIT = 10;

/**
 * «Создать личное сообщение» by the list's header, as in Discord: one
 * friend opens the DM with them, more make a group (up to nine besides the
 * user). The new channel opens.
 */
export const NewPrivateButton = () => {
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  return (
    <>
      <IconButton
        label="Создать личное сообщение"
        icon={Plus}
        className="size-6 [&>svg]:size-4"
        onClick={() => {
          setOpen(true);
        }}
      />
      <PickFriendsDialog
        open={open}
        onOpenChange={setOpen}
        title="Выберите друзей"
        description="Один друг — личная переписка, двое и больше — группа до 10 человек."
        max={GROUP_LIMIT - 1}
        confirmLabel={(n) => (n > 1 ? "Создать группу" : "Написать")}
        onConfirm={async (ids) => {
          const [only] = ids;
          const channel = ids.length === 1 && only !== undefined ? await openDirect(only) : await createGroup(ids);
          void navigate({ to: "/channels/@me/$channelId", params: { channelId: channel.id } });
        }}
      />
    </>
  );
};

/**
 * «Добавить друзей» in a DM's or group's header. In a group the friends
 * join it (up to 10 people); in a DM a new group starts with the other
 * person and them, as in Discord, and opens.
 */
export const AddPeopleButton = ({ channel }: { channel: DMChannel }) => {
  const [open, setOpen] = useState(false);
  const navigate = useNavigate();
  const add = useAddRecipients();
  const peer = dmPeer(channel);
  const there = channel.recipients.map((r) => r.id);
  const room = GROUP_LIMIT - 1 - there.length;
  return (
    <>
      <IconButton
        label={peer ? "Создать группу" : "Добавить друзей"}
        icon={UserPlus}
        tooltipSide="bottom"
        disabled={!peer && room <= 0}
        onClick={() => {
          setOpen(true);
        }}
      />
      <PickFriendsDialog
        open={open}
        onOpenChange={setOpen}
        title={peer ? `Новая группа с @${peer.display_name}` : `Позвать в «${privateName(channel)}»`}
        description={
          peer
            ? "Выберите, кого ещё позвать. Эта переписка останется как есть, группа будет новой."
            : "Новые участники увидят всю историю группы."
        }
        exclude={there}
        max={room}
        confirmLabel={(n) => (peer ? "Создать группу" : n > 1 ? "Добавить друзей" : "Добавить друга")}
        onConfirm={async (ids) => {
          if (!peer) {
            await add.mutateAsync({ id: channel.id, userIds: ids });
            return;
          }
          const group = await createGroup([peer.id, ...ids]);
          void navigate({ to: "/channels/@me/$channelId", params: { channelId: group.id } });
        }}
      />
    </>
  );
};

const RENAME_FORM = "rename-group-form";

/** «Название группы»: any member may set it; empty means the members' names, as in Discord. */
export const RenameGroupDialog = ({
  channel,
  open,
  onOpenChange,
}: {
  channel: DMChannel;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    {open && (
      <RenameForm
        channel={channel}
        onDone={() => {
          onOpenChange(false);
        }}
      />
    )}
  </Dialog>
);

const RenameForm = ({ channel, onDone }: { channel: DMChannel; onDone: () => void }) => {
  const [name, setName] = useState(channel.name ?? "");
  const rename = useRenameGroup();
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const value = name.trim();
    rename.mutate({ id: channel.id, name: value === "" ? null : value }, { onSuccess: onDone });
  };
  return (
    <DialogContent
      title="Название группы"
      description="Его видят все участники. Оставьте пустым, чтобы показывать имена участников."
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">Отмена</Button>
          </DialogClose>
          <Button type="submit" form={RENAME_FORM} loading={rename.isPending}>
            Сохранить
          </Button>
        </>
      }
    >
      <form id={RENAME_FORM} noValidate onSubmit={submit}>
        <Field htmlFor="group-name" label="Название">
          <Input
            value={name}
            placeholder={privateName({ name: null, recipients: channel.recipients })}
            maxLength={100}
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
