import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { api, errorText, unwrap } from "@/shared/api";
import { t } from "@/shared/i18n";
import { ConfirmDialog, useToast } from "@/shared/ui";

interface LeaveGuildDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  guildId: string;
  guildName: string;
}

/**
 * Asks before leaving a server (not offered to its owner), then leaves it
 * and goes home at once. Going home first means the GUILD_DELETE the server
 * sends, which may arrive before the answer, never reads as losing access to
 * the open channel. The result comes as a toast.
 */
export const LeaveGuildDialog = ({ open, onOpenChange, guildId, guildName }: LeaveGuildDialogProps) => {
  const toast = useToast();
  const navigate = useNavigate();
  const leave = useMutation({
    mutationFn: async () => {
      unwrap(await api.DELETE("/users/@me/guilds/{guild_id}", { params: { path: { guild_id: guildId } } }));
    },
    // Hook-level callbacks: they still run after the page they started on is gone.
    onSuccess: () => {
      toast.show({ text: t("guild:leave.done", { name: guildName }) });
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("guild:leave.title", { name: guildName })}
      description={t("guild:leave.text")}
      confirmLabel={t("guild:leave.confirm")}
      tone="danger"
      onConfirm={() => {
        leave.mutate();
        onOpenChange(false);
        void navigate({ to: "/channels/@me" });
      }}
    />
  );
};
