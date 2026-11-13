import { useMutation } from "@tanstack/react-query";
import { LogOut } from "lucide-react";
import { useState } from "react";
import { logout } from "@/entities/session";
import { errorText } from "@/shared/api";
import { Button, ConfirmDialog, cn, useToast } from "@/shared/ui";

export const LOGOUT_CONFIRM = "Выйти из аккаунта?";

/** Signs out; a failed request shows a toast and keeps the session. */
const useLogout = () => {
  const toast = useToast();
  return useMutation({
    mutationFn: logout,
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
};

interface LogoutButtonProps {
  className?: string;
}

/** «Выйти»: signs out and goes to sign-in; a failed request shows a toast and keeps the session. */
export const LogoutButton = ({ className }: LogoutButtonProps) => {
  const signOut = useLogout();
  const run = () => {
    if (!signOut.isPending) signOut.mutate();
  };
  return (
    <Button variant="secondary" className={className} loading={signOut.isPending} onClick={run}>
      Выйти
    </Button>
  );
};

/**
 * «Выйти» as an item of the user settings, as in Discord: it asks first,
 * since it sits among sections a stray click should not cost the session.
 */
export const LogoutItem = ({ className }: { className?: string }) => {
  const [open, setOpen] = useState(false);
  const signOut = useLogout();
  return (
    <>
      <button
        type="button"
        className={cn("flex items-center gap-2", className)}
        onClick={() => {
          setOpen(true);
        }}
      >
        Выйти
        <LogOut aria-hidden className="size-4" />
      </button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={LOGOUT_CONFIRM}
        description="Чтобы вернуться, понадобится снова ввести логин и пароль."
        confirmLabel="Выйти"
        tone="danger"
        pending={signOut.isPending}
        onConfirm={() => {
          signOut.mutate(undefined, {
            onError: () => {
              setOpen(false);
            },
          });
        }}
      />
    </>
  );
};
