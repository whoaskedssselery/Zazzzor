import { AuthLayout, LoginForm } from "@/features/auth";
import { t } from "@/shared/i18n";

/** /login. After signing in the user goes to `next` (a checked same-origin path) or home. */
export const LoginPage = ({ next }: { next?: string | undefined }) => (
  <AuthLayout title={t("auth:welcomeBack")} subtitle={t("auth:welcomeBackText")}>
    <LoginForm next={next} />
  </AuthLayout>
);
