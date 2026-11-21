import type { Message } from "@/entities/message";
import { t } from "@/shared/i18n";
import { ConfirmDialog } from "@/shared/ui";

/**
 * Asks before deleting `message` (null: closed). The chat holds one for the
 * whole list and runs the deletion itself, so Shift+click can skip this.
 */
export const DeleteMessageDialog = ({
  message,
  onClose,
  onConfirm,
}: {
  message: Message | null;
  onClose: () => void;
  onConfirm: (message: Message) => void;
}) => (
  <ConfirmDialog
    open={message !== null}
    onOpenChange={(open) => {
      if (!open) onClose();
    }}
    title={t("chat:deleteTitle")}
    description={t("chat:deleteText")}
    confirmLabel={t("chat:delete")}
    tone="danger"
    onConfirm={() => {
      if (message) onConfirm(message);
      onClose();
    }}
  />
);
