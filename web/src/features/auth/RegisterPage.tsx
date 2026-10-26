import { zodResolver } from "@hookform/resolvers/zod";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { api, ApiError, unwrap } from "../../api/client";
import { Button } from "../../design/components/Button";
import { Field, Input } from "../../design/components/Input";
import { trimContent } from "../../lib/text";
import { AuthLayout, FormAlert } from "./AuthLayout";
import { isFieldOf, lowerCaseInput, registerSchema, showRequestError, type RegisterValues } from "./form";
import { safeNext, setSignedIn } from "./session";

const linkClass = "font-medium text-link underline-offset-2 hover:underline";

/**
 * Registration; the new account is signed in at once and goes to `next` or
 * home. An instance may close registration, which the server answers with
 * registration_closed: the alert then points to sign-in.
 */
export function RegisterPage({ next }: { next?: string | undefined }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [alert, setAlert] = useState<ReactNode>(null);
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<RegisterValues>({
    resolver: zodResolver(registerSchema),
    defaultValues: { display_name: "", username: "", password: "" },
  });

  const onSubmit = handleSubmit(async ({ display_name, username, password }) => {
    setAlert(null);
    const name = trimContent(display_name);
    try {
      const user = unwrap(
        await api.POST("/auth/register", { body: name ? { username, password, display_name: name } : { username, password } }),
      );
      setSignedIn(queryClient, user);
      await navigate({ href: safeNext(next), replace: true });
    } catch (e) {
      const text = showRequestError(e, (field, message) => {
        if (!isFieldOf(registerSchema, field)) return false;
        setError(field, { type: "server", message });
        return true;
      });
      const closed = e instanceof ApiError && e.code === "registration_closed";
      setAlert(
        closed ? (
          <>
            {text} Уже есть учётная запись?{" "}
            <Link to="/login" search={{ next }} className={linkClass}>
              Войти
            </Link>
          </>
        ) : (
          text
        ),
      );
    }
  });

  const username = register("username");

  return (
    <AuthLayout title="Создать учётную запись" subtitle="Пара полей, и вы с друзьями.">
      {alert && <FormAlert>{alert}</FormAlert>}
      <form noValidate onSubmit={(e) => void onSubmit(e)} className="flex flex-col gap-5">
        <Field
          htmlFor="register-display-name"
          label="Отображаемое имя"
          hint="Так вас увидят друзья. Можно оставить пустым."
          error={errors.display_name?.message}
        >
          <Input {...register("display_name")} autoComplete="nickname" autoFocus />
        </Field>
        <Field
          htmlFor="register-username"
          label="Логин"
          hint="Для входа: латиница, цифры, «_» и «.»."
          error={errors.username?.message}
        >
          <Input
            {...username}
            onChange={(e) => {
              lowerCaseInput(e.currentTarget);
              void username.onChange(e);
            }}
            autoComplete="username"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
          />
        </Field>
        <Field htmlFor="register-password" label="Пароль" hint="От 8 до 128 символов." error={errors.password?.message}>
          <Input {...register("password")} type="password" autoComplete="new-password" />
        </Field>
        <Button type="submit" size="lg" loading={isSubmitting} className="mt-1 w-full">
          Зарегистрироваться
        </Button>
      </form>
      <p className="mt-4 text-sm text-fg-muted">
        Уже есть учётная запись?{" "}
        <Link to="/login" search={{ next }} className={linkClass}>
          Войти
        </Link>
      </p>
    </AuthLayout>
  );
}
