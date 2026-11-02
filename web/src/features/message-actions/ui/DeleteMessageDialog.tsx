import type { Message } from "@/entities/message";
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
    title="Удалить сообщение?"
    description="Его не получится вернуть. Чтобы удалять без вопроса, держите Shift."
    confirmLabel="Удалить"
    tone="danger"
    onConfirm={() => {
      if (message) onConfirm(message);
      onClose();
    }}
  />
);
