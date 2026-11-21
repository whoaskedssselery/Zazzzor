import { FileText, RotateCw, X } from "lucide-react";
import { formatSize } from "@/entities/message";
import { t } from "@/shared/i18n";
import { cn, IconButton } from "@/shared/ui";
import { removeUpload, retryUpload, type Upload } from "../model/uploads";

/** One file in the composer: a preview (or an icon), its name and size, the upload's progress or why it failed. */
const UploadTile = ({ channelId, upload: u }: { channelId: string; upload: Upload }) => {
  const name = u.file.name || t("chat:send.file");
  return (
    <li
      aria-label={name}
      className={cn(
        "relative flex w-44 shrink-0 animate-pop-in flex-col gap-1.5 rounded-lg bg-sidebar p-2",
        u.status === "failed" && "ring-1 ring-danger",
      )}
    >
      <div className="flex h-24 items-center justify-center overflow-hidden rounded-md bg-chat">
        {u.previewUrl ? (
          <img src={u.previewUrl} alt="" className="size-full object-cover" />
        ) : (
          <FileText aria-hidden className="size-10 text-fg-muted" strokeWidth={1.5} />
        )}
      </div>
      <p className="truncate text-sm" title={name}>
        {name}
      </p>
      {u.status === "failed" ? (
        <p className="flex items-center gap-1 text-xs text-fg-danger">
          <span className="min-w-0 flex-1 truncate" title={u.error ?? undefined}>
            {u.error}
          </span>
          <IconButton
            size="sm"
            label={t("chat:send.retryUpload")}
            icon={RotateCw}
            onClick={() => {
              retryUpload(channelId, u.key);
            }}
          />
        </p>
      ) : (
        <div className="flex items-center gap-2 text-xs text-fg-faint">
          <span className="shrink-0">{formatSize(u.file.size)}</span>
          {u.status === "uploading" && (
            <progress
              aria-label={t("chat:send.uploadOf", { name })}
              value={u.progress}
              max={1}
              className="h-1 min-w-0 flex-1 appearance-none overflow-hidden rounded-full bg-control [&::-moz-progress-bar]:bg-accent [&::-webkit-progress-bar]:bg-control [&::-webkit-progress-value]:bg-accent [&::-webkit-progress-value]:transition-[width]"
            />
          )}
        </div>
      )}
      <IconButton
        size="sm"
        label={t("chat:send.remove", { name })}
        icon={X}
        className="absolute top-1 right-1 rounded-full bg-overlay shadow-float"
        onClick={() => {
          removeUpload(channelId, u.key);
        }}
      />
    </li>
  );
};

/** The files waiting in the composer, above the text field. */
export const UploadTray = ({ channelId, uploads }: { channelId: string; uploads: readonly Upload[] }) =>
  uploads.length === 0 ? null : (
    <ul aria-label={t("chat:send.attached")} className="flex gap-2 overflow-x-auto px-3 pt-3 pb-1">
      {uploads.map((u) => (
        <UploadTile key={u.key} channelId={channelId} upload={u} />
      ))}
    </ul>
  );
