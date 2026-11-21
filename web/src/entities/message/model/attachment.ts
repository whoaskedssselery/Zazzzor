import type { components } from "@/shared/api";
import { currentLanguage, t } from "@/shared/i18n";
import type { Message } from "./cache";

/**
 * A file of a message (schema Attachment). `url` serves the original bytes
 * to whoever may read the channel; `pending` is true while it is uploaded
 * but not yet sent in a message. Width and height are known for images only.
 */
export type Attachment = components["schemas"]["Attachment"];

/** Files per message, and bytes per file, as the server allows. */
export const MAX_ATTACHMENTS = 10;
export const MAX_ATTACHMENT_BYTES = 25 * 1024 * 1024;

/** How a file shows in the chat: in place for what the server serves inline, as a card otherwise. */
export type AttachmentKind = "image" | "video" | "audio" | "file";

const INLINE: Readonly<Record<string, AttachmentKind>> = {
  "image/png": "image",
  "image/jpeg": "image",
  "image/webp": "image",
  "image/gif": "image",
  "video/mp4": "video",
  "video/webm": "video",
  "audio/mpeg": "audio",
  "audio/ogg": "audio",
  "audio/wave": "audio",
};

/** The way `contentType` (the type the server sniffed, not the file name) is shown. */
export const attachmentKind = (contentType: string): AttachmentKind => INLINE[contentType] ?? "file";

const number = () => new Intl.NumberFormat(currentLanguage(), { maximumFractionDigits: 1 });

/** «512 Б», «1,2 КБ», «24,8 МБ». */
export const formatSize = (bytes: number): string => {
  if (bytes < 1024) return t("common:size.bytes", { size: String(bytes) });
  if (bytes < 1024 * 1024) return t("common:size.kilobytes", { size: number().format(bytes / 1024) });
  return t("common:size.megabytes", { size: number().format(bytes / (1024 * 1024)) });
};

/** The files of `message`. */
export const attachmentsOf = (message: Message): Attachment[] => message.attachments;
