import { useMutation } from "@tanstack/react-query";
import { logout } from "@/entities/session";
import { errorText } from "@/shared/api";
import { Button, useToast } from "@/shared/ui";

/** «Выйти»: signs out and goes to sign-in; a failed request shows a toast and keeps the session. */
export function LogoutButton({ className }: { className?: string }) {
  const toast = useToast();
  const signOut = useMutation({
    mutationFn: logout,
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
  return (
    <Button
      variant="secondary"
      className={className}
      loading={signOut.isPending}
      onClick={() => {
        signOut.mutate();
      }}
    >
      Выйти
    </Button>
  );
}
