import { zodResolver } from "@hookform/resolvers/zod";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { type ReactNode, useState } from "react";
import { useForm } from "react-hook-form";
import { safeNext, setSignedIn } from "@/entities/session";
import { ApiError, api, unwrap } from "@/shared/api";
import { trimContent } from "@/shared/lib";
import { Button, Field, Input } from "@/shared/ui";
import { isFieldOf, lowerCaseInput, type RegisterValues, registerSchema, showRequestError } from "../model/form";
import { FormAlert } from "./AuthLayout";

const linkClass = "font-medium text-link underline-offset-2 hover:underline";

/**
 * The registration form; the new account is signed in at once and goes to `next` or
 * home. An instance may close registration, which the server answers with
 * registration_closed: the alert then points to sign-in.
 */
export function RegisterForm({ next }: { next?: string | undefined }) {
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
        await api.POST("/auth/register", {
          body: name ? { username, password, display_name: name } : { username, password },
        }),
      );
      setSignedIn(queryClient, user);
      await navigate({ href: safeNext(next), replace: true });
    } catch (e) {
      let focused = false;
      const text = showRequestError(e, (field, message) => {
        if (!isFieldOf(registerSchema, field)) return false;
        // The first field with a server error takes focus, so its error is read out.
        setError(field, { type: "server", message }, { shouldFocus: !focused });
        focused = true;
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
    <>
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
          hint="От 2 до 32 символов: латиница, цифры, «_» и «.»."
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
    </>
  );
}
