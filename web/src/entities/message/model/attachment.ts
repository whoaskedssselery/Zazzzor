import type { Message } from "./cache";

/**
 * A file of a message, as the server describes it (schema Attachment).
 * `url` serves the original bytes to whoever may read the channel; `pending`
 * is true while it is uploaded but not yet sent in a message. Width and
 * height are known for images only.
 *
 * Kept here, by hand, until the contract with attachments is generated into
 * shared/api; the shape is the one agreed in docs/codex/C7-attachments.md.
 */
export interface Attachment {
  id: string;
  filename: string;
  content_type: string;
  size: number;
  width: number | null;
  height: number | null;
  url: string;
  pending: boolean;
}

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
  "audio/wav": "audio",
};

/** The way `contentType` (the type the server sniffed, not the file name) is shown. */
export const attachmentKind = (contentType: string): AttachmentKind => INLINE[contentType] ?? "file";

const number = new Intl.NumberFormat("ru-RU", { maximumFractionDigits: 1 });

/** «512 Б», «1,2 КБ», «24,8 МБ». */
export const formatSize = (bytes: number): string => {
  if (bytes < 1024) return `${String(bytes)} Б`;
  if (bytes < 1024 * 1024) return `${number.format(bytes / 1024)} КБ`;
  return `${number.format(bytes / (1024 * 1024))} МБ`;
};

/** The files of `message`; none for a server that does not send them yet. */
export const attachmentsOf = (message: Message): Attachment[] =>
  (message as Message & { attachments?: Attachment[] }).attachments ?? [];
