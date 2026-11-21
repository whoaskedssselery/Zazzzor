import { X } from "lucide-react";
import { t } from "@/shared/i18n";
import { IconButton } from "@/shared/ui";

/** Above the composer while answering a message: who is answered, and a way out. */
export const ReplyBar = ({ name, onCancel }: { name: string; onCancel: () => void }) => (
  <div className="flex h-9 items-center gap-2 rounded-t-lg bg-panel pr-2 pl-4 text-sm text-fg-muted">
    <p className="min-w-0 flex-1 truncate">
      {t("chat:send.replyingTo")} <bdi className="font-semibold text-fg">{name}</bdi>
    </p>
    <IconButton size="sm" label={t("chat:send.cancelReply")} icon={X} onClick={onCancel} />
  </div>
);
