import { Search } from "lucide-react";
import { type SubmitEvent, useMemo, useState } from "react";
import { friendLists, useGuilds } from "@/entities/guild";
import { errorText } from "@/shared/api";
import { Avatar, Button, Checkbox, Dialog, DialogClose, DialogContent, FormAlert, Input } from "@/shared/ui";

export interface PickFriendsDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  /** Friends already there: not offered. */
  exclude?: readonly string[];
  /** How many may be picked at most. */
  max: number;
  confirmLabel: (picked: number) => string;
  /** Does what was asked; a refusal shows in the dialog, success closes it. */
  onConfirm: (userIds: string[]) => Promise<unknown>;
}

const FORM_ID = "pick-friends-form";

/**
 * A choice of the user's friends, as Discord's «Выберите друзей»: a search
 * by name, ticks up to `max`, and one button that says what happens.
 */
export const PickFriendsDialog = (props: PickFriendsDialogProps) => (
  <Dialog open={props.open} onOpenChange={props.onOpenChange}>
    {props.open && <PickForm {...props} />}
  </Dialog>
);

const PickForm = ({
  onOpenChange,
  title,
  description,
  exclude = [],
  max,
  confirmLabel,
  onConfirm,
}: PickFriendsDialogProps) => {
  const relationships = useGuilds((s) => s.relationships);
  const presences = useGuilds((s) => s.presences);
  const all = useMemo(() => friendLists({ relationships, presences }).all, [relationships, presences]);
  const friends = useMemo(() => all.filter((r) => !exclude.includes(r.id)), [all, exclude]);
  const [query, setQuery] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const q = query.trim().toLocaleLowerCase("ru");
  const shown =
    q === ""
      ? friends
      : friends.filter((r) =>
          [r.user.display_name, r.user.username].some((n) => n.toLocaleLowerCase("ru").includes(q)),
        );
  const left = max - picked.length;

  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    if (busy || picked.length === 0) return;
    setBusy(true);
    setError(null);
    onConfirm(picked).then(
      () => {
        onOpenChange(false);
      },
      (err: unknown) => {
        setBusy(false);
        setError(errorText(err));
      },
    );
  };

  return (
    <DialogContent
      title={title}
      description={description}
      size="sm"
      footer={
        <>
          <DialogClose asChild>
            <Button variant="ghost">Отмена</Button>
          </DialogClose>
          <Button type="submit" form={FORM_ID} loading={busy} disabled={picked.length === 0}>
            {confirmLabel(picked.length)}
          </Button>
        </>
      }
    >
      {error !== null && <FormAlert>{error}</FormAlert>}
      {/* The padding keeps the field's focus ring inside the dialog's scrolling body, which clips it. */}
      <form id={FORM_ID} noValidate onSubmit={submit} className="-mx-1 flex flex-col gap-3 px-1 pt-1">
        {friends.length === 0 ? (
          <p className="py-6 text-center text-base text-fg-muted">
            {all.length === 0
              ? "Сюда можно позвать только друзей, а их пока нет. Добавьте кого-нибудь на главной."
              : "Все ваши друзья уже здесь."}
          </p>
        ) : (
          <>
            <div className="relative">
              <Search
                aria-hidden
                className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-fg-faint"
              />
              <Input
                value={query}
                aria-label="Найти друга"
                placeholder="Имя друга"
                autoComplete="off"
                autoFocus
                className="pl-9"
                onChange={(e) => {
                  setQuery(e.currentTarget.value);
                }}
              />
            </div>
            <p aria-live="polite" className="text-sm text-fg-muted">
              {left > 0 ? `Можно выбрать ещё ${String(left)}` : "Больше выбрать нельзя"}
            </p>
            <ul aria-label="Друзья" className="-mx-2 max-h-72 overflow-y-auto">
              {shown.map((r) => {
                const on = picked.includes(r.id);
                const id = `pick-${r.id}`;
                return (
                  <li key={r.id}>
                    <label
                      htmlFor={id}
                      className="flex cursor-pointer items-center gap-3 rounded-md px-2 py-1.5 hover:bg-hover has-disabled:cursor-not-allowed has-disabled:opacity-60"
                    >
                      <Avatar name={r.user.display_name} src={r.user.avatar_url} size={32} />
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-base font-medium">{r.user.display_name}</span>
                        <span className="block truncate text-sm text-fg-faint">{r.user.username}</span>
                      </span>
                      <Checkbox
                        id={id}
                        checked={on}
                        disabled={!on && left <= 0}
                        onCheckedChange={(v) => {
                          setPicked((p) => (v === true ? [...p, r.id] : p.filter((x) => x !== r.id)));
                        }}
                      />
                    </label>
                  </li>
                );
              })}
              {shown.length === 0 && <li className="py-4 text-center text-sm text-fg-muted">Никого не нашлось.</li>}
            </ul>
          </>
        )}
      </form>
    </DialogContent>
  );
};
