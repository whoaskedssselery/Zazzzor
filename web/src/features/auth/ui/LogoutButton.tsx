import { useMutation } from "@tanstack/react-query";
import { LogOut } from "lucide-react";
import { useState } from "react";
import { logout } from "@/entities/session";
import { errorText } from "@/shared/api";
import { useTranslation } from "@/shared/i18n";
import { Button, ConfirmDialog, cn, useToast } from "@/shared/ui";

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

/** «Log out»: signs out and goes to sign-in; a failed request shows a toast and keeps the session. */
export const LogoutButton = ({ className }: LogoutButtonProps) => {
  const { t } = useTranslation("auth");
  const signOut = useLogout();
  const run = () => {
    if (!signOut.isPending) signOut.mutate();
  };
  return (
    <Button variant="secondary" className={className} loading={signOut.isPending} onClick={run}>
      {t("logout.action")}
    </Button>
  );
};

/**
 * «Log out» as an item of the user settings, as in Discord: it asks first,
 * since it sits among sections a stray click should not cost the session.
 */
export const LogoutItem = ({ className }: { className?: string }) => {
  const { t } = useTranslation("auth");
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
        {t("logout.action")}
        <LogOut aria-hidden className="size-4" />
      </button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={t("logout.confirm")}
        description={t("logout.description")}
        confirmLabel={t("logout.action")}
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
