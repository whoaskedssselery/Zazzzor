import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod/mini";
import { ApiError, api, errorText, fieldErrorText, tooManyAttemptsText, unwrap } from "@/shared/api";
import { onLanguage, t } from "@/shared/i18n";
import { runeLength } from "@/shared/lib";
import { Button, Field, FormAlert, Input, useToast } from "@/shared/ui";

export let PASSWORDS_DIFFER = "";
export let PASSWORD_CHANGED = "";
onLanguage(() => {
  PASSWORDS_DIFFER = t("auth:passwordsDiffer");
  PASSWORD_CHANGED = t("auth:passwordChanged");
});

const schema = z
  .object({
    current_password: z.string().check(z.minLength(1, { error: () => t("auth:enterCurrent") })),
    new_password: z
      .string()
      .check(
        z.refine((s) => runeLength(s) >= 8 && runeLength(s) <= 128, { error: () => fieldErrorText("new_password") }),
      ),
    repeat: z.string(),
  })
  .check(
    z.refine((v) => v.repeat === v.new_password, { error: () => PASSWORDS_DIFFER, path: ["repeat"], when: () => true }),
  );
type Values = z.infer<typeof schema>;

const EMPTY: Values = { current_password: "", new_password: "", repeat: "" };

/**
 * «Сменить пароль»: the current one, the new one twice. The two new ones
 * must match before anything is sent. On success the server signs out every
 * other session (this one stays) and the form empties; a wrong current
 * password lands under its field.
 */
export const PasswordTab = () => {
  const toast = useToast();
  const [alert, setAlert] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<Values>({ resolver: zodResolver(schema), defaultValues: EMPTY });

  const onSubmit = handleSubmit(async ({ current_password, new_password }) => {
    setAlert(null);
    try {
      unwrap(await api.POST("/users/@me/password", { body: { current_password, new_password } }));
      reset(EMPTY);
      toast.show({ text: PASSWORD_CHANGED, tone: "success" });
    } catch (e) {
      if (!(e instanceof ApiError)) {
        setAlert(errorText(e));
        return;
      }
      if (e.code === "rate_limited") {
        setAlert(tooManyAttemptsText(e.retryAfter));
        return;
      }
      let placed = false;
      for (const field of ["current_password", "new_password"] as const) {
        if (!Object.hasOwn(e.fields, field)) continue;
        setError(field, { type: "server", message: fieldErrorText(field) }, { shouldFocus: !placed });
        placed = true;
      }
      if (!placed) setAlert(errorText(e));
    }
  });

  return (
    <section aria-labelledby="password-title">
      <h2 id="password-title" className="mb-5 text-xl font-semibold">
        {t("auth:changePassword")}
      </h2>
      {alert && <FormAlert>{alert}</FormAlert>}
      <form noValidate onSubmit={(e) => void onSubmit(e)} className="flex max-w-100 flex-col gap-5">
        <Field htmlFor="password-current" label={t("auth:currentPassword")} error={errors.current_password?.message}>
          <Input {...register("current_password")} type="password" autoComplete="current-password" />
        </Field>
        <Field
          htmlFor="password-new"
          label={t("auth:newPassword")}
          hint={t("auth:passwordHint")}
          error={errors.new_password?.message}
        >
          <Input {...register("new_password")} type="password" autoComplete="new-password" />
        </Field>
        <Field htmlFor="password-repeat" label={t("auth:repeatPassword")} error={errors.repeat?.message}>
          <Input {...register("repeat")} type="password" autoComplete="new-password" />
        </Field>
        <p className="text-sm text-fg-muted">{t("auth:changeNote")}</p>
        <Button type="submit" loading={isSubmitting} className="self-start">
          {t("auth:changePassword")}
        </Button>
      </form>
    </section>
  );
};
