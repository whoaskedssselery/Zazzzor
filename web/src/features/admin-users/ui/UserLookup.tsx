import { useMutation } from "@tanstack/react-query";
import { type SubmitEvent, useRef, useState } from "react";
import { ApiError, api, type components, errorText, unwrap } from "@/shared/api";
import {
  Badge,
  Button,
  ConfirmDialog,
  Dialog,
  DialogClose,
  DialogContent,
  Field,
  FormAlert,
  Input,
  useToast,
} from "@/shared/ui";

type AdminUser = components["schemas"]["AdminUser"];

export const NO_SUCH_USER = "Пользователь с таким логином не найден.";
export const PASSWORD_ONCE = "Пароль показывается только сейчас. Передайте его пользователю и попросите сменить.";
const COPIED = "Пароль скопирован";
const COPY_FAILED = "Не получилось скопировать. Пароль выделен: скопируйте его вручную.";

const joined = new Intl.DateTimeFormat("ru", { day: "numeric", month: "long", year: "numeric" });

/**
 * «Пользователи»: an account looked up by its exact login (the server finds
 * no other way), then its card with what an administrator may do to it.
 */
export const UserLookup = () => {
  const [username, setUsername] = useState("");
  const find = useMutation({
    mutationFn: async (name: string) =>
      unwrap(await api.GET("/admin/users", { params: { query: { username: name } } })),
  });
  const submit = (e: SubmitEvent<HTMLFormElement>) => {
    e.preventDefault();
    const name = username.trim().toLowerCase();
    if (name !== "" && !find.isPending) find.mutate(name);
  };
  const missing = find.error instanceof ApiError && find.error.code === "not_found";
  return (
    <section aria-labelledby="users-title">
      <h2 id="users-title" className="mb-5 text-xl font-semibold">
        Пользователи
      </h2>
      <form noValidate onSubmit={submit} className="flex items-start gap-2">
        <Field
          htmlFor="lookup-username"
          label="Логин"
          className="min-w-0 flex-1"
          error={missing ? NO_SUCH_USER : undefined}
        >
          <Input
            value={username}
            autoComplete="off"
            autoCapitalize="none"
            spellCheck={false}
            onChange={(e) => {
              setUsername(e.currentTarget.value);
            }}
          />
        </Field>
        <Button type="submit" className="mt-7 h-10" loading={find.isPending} disabled={username.trim() === ""}>
          Найти
        </Button>
      </form>
      {find.isError && !missing && <FormAlert className="mt-4">{errorText(find.error)}</FormAlert>}
      {find.data && <UserCard key={find.data.id} found={find.data} />}
    </section>
  );
};

/** One account: its name, login and state, a password reset and the block switch, each asked first. */
const UserCard = ({ found }: { found: AdminUser }) => {
  const toast = useToast();
  const [user, setUser] = useState(found);
  const [asking, setAsking] = useState<"reset" | "block" | null>(null);
  const [password, setPassword] = useState<string | null>(null);

  const reset = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/admin/users/{user_id}/password-reset", { params: { path: { user_id: user.id } } })),
    onSuccess: (r) => {
      setAsking(null);
      setPassword(r.temporary_password);
    },
    onError: (e) => {
      setAsking(null);
      toast.show({ text: errorText(e), tone: "error" });
    },
  });

  const block = useMutation({
    mutationFn: async (disable: boolean) => {
      const params = { params: { path: { user_id: user.id } } };
      unwrap(
        disable
          ? await api.POST("/admin/users/{user_id}/disable", params)
          : await api.POST("/admin/users/{user_id}/enable", params),
      );
      return disable;
    },
    onSuccess: (disabled) => {
      setAsking(null);
      setUser((u) => ({ ...u, disabled }));
      toast.show({ text: disabled ? `${user.username} заблокирован` : `${user.username} разблокирован` });
    },
    onError: (e) => {
      setAsking(null);
      toast.show({ text: errorText(e), tone: "error" });
    },
  });

  return (
    <article aria-label={user.username} className="mt-6 rounded-lg bg-sidebar p-4">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <h3 className="truncate text-lg font-semibold">{user.display_name}</h3>
          <p className="truncate text-sm text-fg-muted">{user.username}</p>
          <p className="mt-1 text-xs text-fg-faint">С нами с {joined.format(new Date(user.created_at))}</p>
        </div>
        {user.disabled && <Badge>Заблокирован</Badge>}
      </div>
      <div className="mt-4 flex flex-wrap gap-2">
        <Button
          variant="secondary"
          size="sm"
          onClick={() => {
            setAsking("reset");
          }}
        >
          Сбросить пароль
        </Button>
        <Button
          variant={user.disabled ? "secondary" : "danger"}
          size="sm"
          onClick={() => {
            setAsking("block");
          }}
        >
          {user.disabled ? "Разблокировать" : "Заблокировать"}
        </Button>
      </div>
      <ConfirmDialog
        open={asking === "reset"}
        onOpenChange={(o) => {
          if (!o) setAsking(null);
        }}
        title={`Сбросить пароль ${user.username}?`}
        description="Старый пароль перестанет работать, а все сеансы пользователя завершатся."
        confirmLabel="Сбросить пароль"
        tone="danger"
        pending={reset.isPending}
        onConfirm={() => {
          reset.mutate();
        }}
      />
      <ConfirmDialog
        open={asking === "block"}
        onOpenChange={(o) => {
          if (!o) setAsking(null);
        }}
        title={user.disabled ? `Разблокировать ${user.username}?` : `Заблокировать ${user.username}?`}
        description={
          user.disabled
            ? "Пользователь снова сможет войти."
            : "Пользователь выйдет на всех устройствах и не сможет войти, пока его не разблокируют."
        }
        confirmLabel={user.disabled ? "Разблокировать" : "Заблокировать"}
        tone={user.disabled ? "primary" : "danger"}
        pending={block.isPending}
        onConfirm={() => {
          block.mutate(!user.disabled);
        }}
      />
      <TemporaryPassword
        password={password}
        username={user.username}
        onClose={() => {
          setPassword(null);
        }}
      />
    </article>
  );
};

/** The new password, shown once: it is gone from the page when the dialog closes. */
const TemporaryPassword = ({
  password,
  username,
  onClose,
}: {
  password: string | null;
  username: string;
  onClose: () => void;
}) => {
  const toast = useToast();
  const field = useRef<HTMLInputElement>(null);
  const copy = async (text: string) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.show({ text: COPIED, tone: "success" });
    } catch {
      // No clipboard (HTTP) or no permission: the administrator copies it themselves.
      field.current?.focus();
      field.current?.select();
      toast.show({ text: COPY_FAILED });
    }
  };
  return (
    <Dialog
      open={password !== null}
      onOpenChange={(o) => {
        if (!o) onClose();
      }}
    >
      {password !== null && (
        <DialogContent
          title={`Временный пароль ${username}`}
          description={PASSWORD_ONCE}
          footer={
            <>
              <DialogClose asChild>
                <Button variant="ghost">Готово</Button>
              </DialogClose>
              <Button onClick={() => void copy(password)}>Скопировать</Button>
            </>
          }
        >
          <Field htmlFor="temporary-password" label="Пароль">
            <Input ref={field} readOnly value={password} className="font-mono" />
          </Field>
        </DialogContent>
      )}
    </Dialog>
  );
};
