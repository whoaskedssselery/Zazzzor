import { useMutation } from "@tanstack/react-query";
import { LogOut } from "lucide-react";
import { logout } from "@/entities/session";
import { errorText } from "@/shared/api";
import { Button, IconButton, useToast } from "@/shared/ui";

interface LogoutButtonProps {
  className?: string;
  /** An icon with a tooltip (the user panel) instead of a text button. */
  iconOnly?: boolean;
}

/** «Выйти»: signs out and goes to sign-in; a failed request shows a toast and keeps the session. */
export const LogoutButton = ({ className, iconOnly = false }: LogoutButtonProps) => {
  const toast = useToast();
  const signOut = useMutation({
    mutationFn: logout,
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
  const run = () => {
    if (!signOut.isPending) signOut.mutate();
  };
  if (iconOnly) {
    return <IconButton label="Выйти" icon={LogOut} className={className} disabled={signOut.isPending} onClick={run} />;
  }
  return (
    <Button variant="secondary" className={className} loading={signOut.isPending} onClick={run}>
      Выйти
    </Button>
  );
};
