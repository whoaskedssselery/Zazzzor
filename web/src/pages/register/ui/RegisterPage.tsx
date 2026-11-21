import { AuthLayout, RegisterForm } from "@/features/auth";
import { t } from "@/shared/i18n";

/** /register. The new account is signed in at once and goes to `next` or home. */
export const RegisterPage = ({ next }: { next?: string | undefined }) => (
  <AuthLayout title={t("auth:createAccount")} subtitle={t("auth:createAccountText")}>
    <RegisterForm next={next} />
  </AuthLayout>
);
