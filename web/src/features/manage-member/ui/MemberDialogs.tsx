import { type SubmitEvent, useState } from "react";
import type { GuildEntry, Member } from "@/entities/guild";
import { runeLength, trimContent } from "@/shared/lib";
import {
  Button,
  Checkbox,
  ConfirmDialog,
  Dialog,
  DialogClose,
  DialogContent,
  Field,
  Input,
  RoleColorDot,
  Textarea,
} from "@/shared/ui";
import { assignableRoles, useMemberMutations } from "../model/actions";

export type MemberDialog = "nick" | "roles" | "kick" | "ban";

const NICK_RULE = "Ник: до 32 символов.";
const REASON_MAX = 512;

const nameOf = (member: Member) => member.nick ?? member.user.display_name;

const NickForm = ({ guildId, member, onDone }: { guildId: string; member: Member; onDone: () => void }) => {
  const [nick, setNick] = useState(member.nick ?? "");
  const [error, setError] = useState<string | undefined>(undefined);
  const { update } = useMemberMutations(guildId);
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const value = trimContent(nick);
    if (runeLength(value) > 32) {
      setError(NICK_RULE);
      return;
    }
    update.mutate({ userId: member.user.id, body: { nick: value === "" ? null : value } }, { onSuccess: onDone });
  };
  return (
    <DialogContent
      title="Изменить ник"
      description={`Ник на этом сервере вместо «${member.user.display_name}». Пустой — без ника.`}
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">Отмена</Button>
          </DialogClose>
          <Button type="submit" form="member-nick-form" loading={update.isPending}>
            Сохранить
          </Button>
        </>
      }
    >
      <form id="member-nick-form" noValidate onSubmit={submit}>
        <Field htmlFor="member-nick" label="Ник" error={error}>
          <Input
            value={nick}
            maxLength={64}
            autoComplete="off"
            autoFocus
            onChange={(e) => {
              setNick(e.currentTarget.value);
            }}
          />
        </Field>
      </form>
    </DialogContent>
  );
};

const RolesForm = ({
  entry,
  meId,
  member,
  onDone,
}: {
  entry: GuildEntry;
  meId: string;
  member: Member;
  onDone: () => void;
}) => {
  const allowed = assignableRoles(entry, meId);
  const [roles, setRoles] = useState<ReadonlySet<string>>(new Set(member.roles));
  const { update } = useMemberMutations(entry.guild.id);
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    update.mutate({ userId: member.user.id, body: { roles: [...roles] } }, { onSuccess: onDone });
  };
  return (
    <DialogContent
      title={`Роли — ${nameOf(member)}`}
      description="Можно выдать и снять только роли ниже вашей высшей."
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">Отмена</Button>
          </DialogClose>
          <Button type="submit" form="member-roles-form" loading={update.isPending}>
            Сохранить
          </Button>
        </>
      }
    >
      <form id="member-roles-form" onSubmit={submit}>
        <ul className="flex flex-col gap-1">
          {allowed.map((r) => (
            <li key={r.id} className="flex items-center gap-3 rounded-md px-2 py-1.5 hover:bg-hover">
              <Checkbox
                id={`member-role-${r.id}`}
                checked={roles.has(r.id)}
                onCheckedChange={(on) => {
                  setRoles((s) => {
                    const next = new Set(s);
                    if (on === true) next.add(r.id);
                    else next.delete(r.id);
                    return next;
                  });
                }}
              />
              <RoleColorDot color={r.color} />
              <label htmlFor={`member-role-${r.id}`} className="min-w-0 flex-1 truncate text-base">
                {r.name}
              </label>
            </li>
          ))}
        </ul>
      </form>
    </DialogContent>
  );
};

const BanForm = ({ guildId, member, onDone }: { guildId: string; member: Member; onDone: () => void }) => {
  const [reason, setReason] = useState("");
  const { ban } = useMemberMutations(guildId);
  const tooLong = runeLength(trimContent(reason)) > REASON_MAX;
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (tooLong) return;
    const value = trimContent(reason);
    ban.mutate({ userId: member.user.id, reason: value === "" ? null : value }, { onSuccess: onDone });
  };
  return (
    <DialogContent
      title={`Забанить ${nameOf(member)}?`}
      description="Участник покинет сервер и не сможет вернуться, пока его не разбанят."
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">Отмена</Button>
          </DialogClose>
          <Button type="submit" form="member-ban-form" variant="danger" disabled={tooLong} loading={ban.isPending}>
            Забанить
          </Button>
        </>
      }
    >
      <form id="member-ban-form" noValidate onSubmit={submit}>
        <Field
          htmlFor="member-ban-reason"
          label="Причина"
          hint={`Необязательно, до ${String(REASON_MAX)} символов.`}
          error={tooLong ? `Причина: до ${String(REASON_MAX)} символов.` : undefined}
        >
          <Textarea
            value={reason}
            rows={3}
            onChange={(e) => {
              setReason(e.currentTarget.value);
            }}
          />
        </Field>
      </form>
    </DialogContent>
  );
};

/**
 * The dialog of one member action (null: none open). The menu that opens
 * it decides whether the action is allowed; the server checks it again.
 */
export const MemberDialogs = ({
  entry,
  meId,
  member,
  open,
  onClose,
}: {
  entry: GuildEntry;
  meId: string;
  member: Member;
  open: MemberDialog | null;
  onClose: () => void;
}) => {
  const { kick } = useMemberMutations(entry.guild.id);
  const onOpenChange = (o: boolean) => {
    if (!o) onClose();
  };
  return (
    <>
      <Dialog open={open === "nick" || open === "roles" || open === "ban"} onOpenChange={onOpenChange}>
        {open === "nick" && <NickForm guildId={entry.guild.id} member={member} onDone={onClose} />}
        {open === "roles" && <RolesForm entry={entry} meId={meId} member={member} onDone={onClose} />}
        {open === "ban" && <BanForm guildId={entry.guild.id} member={member} onDone={onClose} />}
      </Dialog>
      <ConfirmDialog
        open={open === "kick"}
        onOpenChange={onOpenChange}
        title={`Выгнать ${nameOf(member)}?`}
        description="Участник покинет сервер, но сможет вернуться по приглашению."
        confirmLabel="Выгнать"
        tone="danger"
        onConfirm={() => {
          kick.mutate(member.user.id);
          onClose();
        }}
      />
    </>
  );
};
