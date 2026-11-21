import { MessageCircle, X } from "lucide-react";
import { type ReactNode, type SubmitEvent, useState } from "react";
import type { User } from "@/entities/session";
import { t } from "@/shared/i18n";
import {
  Button,
  Dialog,
  DialogClose,
  DialogContent,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Field,
  Input,
  StatusDot,
} from "@/shared/ui";
import { CHOSEN_STATUSES, type ChosenStatus, STATUS_LINE_MAX, useSetStatus } from "../model/status";

const isChosen = (v: string): v is ChosenStatus => CHOSEN_STATUSES.some((s) => s.value === v);

/** The dot each choice shows: invisible looks offline to others. */
const dotOf = (s: ChosenStatus) => (s === "invisible" ? "offline" : s);

/**
 * The user's own status: «В сети», «Нет на месте», «Не беспокоить» or
 * «Невидимка», and their status line. `children` opens it.
 */
export const StatusMenu = ({ user, children }: { user: User; children: ReactNode }) => {
  const set = useSetStatus();
  const [editing, setEditing] = useState(false);
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
        <DropdownMenuContent align="start" side="top" className="w-64">
          <DropdownMenuLabel>{t("settings:status.title")}</DropdownMenuLabel>
          <DropdownMenuRadioGroup
            value={user.status}
            onValueChange={(v) => {
              if (isChosen(v) && v !== user.status) set.mutate({ status: v });
            }}
          >
            {CHOSEN_STATUSES.map((s) => (
              <DropdownMenuRadioItem key={s.value} value={s.value} className="h-auto py-1.5">
                <StatusDot status={dotOf(s.value)} size={10} className="mx-1" />
                <span className="min-w-0">
                  <span className="block">{s.label}</span>
                  {s.hint && <span className="block text-xs text-fg-faint">{s.hint}</span>}
                </span>
              </DropdownMenuRadioItem>
            ))}
          </DropdownMenuRadioGroup>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            onSelect={() => {
              setEditing(true);
            }}
          >
            {user.custom_status === null ? t("settings:status.set") : t("settings:status.edit")}
            <MessageCircle aria-hidden />
          </DropdownMenuItem>
          {user.custom_status !== null && (
            <DropdownMenuItem
              onSelect={() => {
                set.mutate({ custom_status: null });
              }}
            >
              {t("settings:status.clear")}
              <X aria-hidden />
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
      <Dialog open={editing} onOpenChange={setEditing}>
        {editing && (
          <LineForm
            line={user.custom_status}
            onDone={() => {
              setEditing(false);
            }}
          />
        )}
      </Dialog>
    </>
  );
};

const LINE_FORM = "status-line-form";

/** «Свой статус»: a line under the name that friends and servers see; empty clears it. */
const LineForm = ({ line, onDone }: { line: string | null; onDone: () => void }) => {
  const [value, setValue] = useState(line ?? "");
  const set = useSetStatus();
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const text = value.trim();
    set.mutate({ custom_status: text === "" ? null : text }, { onSuccess: onDone });
  };
  return (
    <DialogContent
      title={t("settings:status.dialogTitle")}
      description={t("settings:status.dialogText")}
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">{t("common:cancel")}</Button>
          </DialogClose>
          <Button type="submit" form={LINE_FORM} loading={set.isPending}>
            {t("settings:status.save")}
          </Button>
        </>
      }
    >
      <form id={LINE_FORM} noValidate onSubmit={submit}>
        <Field htmlFor="status-line" label={t("settings:status.what")}>
          <Input
            value={value}
            placeholder={t("settings:status.placeholder")}
            maxLength={STATUS_LINE_MAX}
            autoComplete="off"
            autoFocus
            onChange={(e) => {
              setValue(e.currentTarget.value);
            }}
          />
        </Field>
      </form>
    </DialogContent>
  );
};
