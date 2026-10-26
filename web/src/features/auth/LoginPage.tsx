import { zodResolver } from "@hookform/resolvers/zod";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { api, unwrap } from "../../api/client";
import { Button } from "../../design/components/Button";
import { Field, Input } from "../../design/components/Input";
import { AuthLayout, FormAlert } from "./AuthLayout";
import { isFieldOf, type LoginValues, loginSchema, lowerCaseInput, showRequestError } from "./form";
import { safeNext, setSignedIn } from "./session";

const linkClass = "font-medium text-link underline-offset-2 hover:underline";

/** Sign-in. After it the user goes to `next` (a checked same-origin path) or home. */
export function LoginPage({ next }: { next?: string | undefined }) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [alert, setAlert] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<LoginValues>({ resolver: zodResolver(loginSchema), defaultValues: { username: "", password: "" } });

  const onSubmit = handleSubmit(async (values) => {
    setAlert(null);
    try {
      const user = unwrap(await api.POST("/auth/login", { body: values }));
      setSignedIn(queryClient, user);
      await navigate({ href: safeNext(next), replace: true });
    } catch (e) {
      let focused = false;
      setAlert(
        showRequestError(e, (field, message) => {
          if (!isFieldOf(loginSchema, field)) return false;
          // The first field with a server error takes focus, so its error is read out.
          setError(field, { type: "server", message }, { shouldFocus: !focused });
          focused = true;
          return true;
        }),
      );
    }
  });

  const username = register("username");

  return (
    <AuthLayout title="С возвращением!" subtitle="Рады снова вас видеть.">
      {alert && <FormAlert>{alert}</FormAlert>}
      <form noValidate onSubmit={(e) => void onSubmit(e)} className="flex flex-col gap-5">
        <Field htmlFor="login-username" label="Логин" error={errors.username?.message}>
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
            autoFocus
          />
        </Field>
        <Field htmlFor="login-password" label="Пароль" error={errors.password?.message}>
          <Input {...register("password")} type="password" autoComplete="current-password" />
        </Field>
        <Button type="submit" size="lg" loading={isSubmitting} className="mt-1 w-full">
          Войти
        </Button>
      </form>
      <p className="mt-4 text-sm text-fg-muted">
        Нет учётной записи?{" "}
        <Link to="/register" search={{ next }} className={linkClass}>
          Зарегистрироваться
        </Link>
      </p>
    </AuthLayout>
  );
}
