import { AuthLayout, RegisterForm } from "@/features/auth";

/** /register. The new account is signed in at once and goes to `next` or home. */
export function RegisterPage({ next }: { next?: string | undefined }) {
  return (
    <AuthLayout title="Создать учётную запись" subtitle="Пара полей, и вы с друзьями.">
      <RegisterForm next={next} />
    </AuthLayout>
  );
}
