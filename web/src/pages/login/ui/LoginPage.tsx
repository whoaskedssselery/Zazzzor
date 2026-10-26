import { AuthLayout, LoginForm } from "@/features/auth";

/** /login. After signing in the user goes to `next` (a checked same-origin path) or home. */
export const LoginPage = ({ next }: { next?: string | undefined }) => (
  <AuthLayout title="С возвращением!" subtitle="Рады снова вас видеть.">
    <LoginForm next={next} />
  </AuthLayout>
);
