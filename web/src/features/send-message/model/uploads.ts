import { create } from "zustand";
import { type Attachment, formatSize, MAX_ATTACHMENT_BYTES, MAX_ATTACHMENTS } from "@/entities/message";
import { ApiError, errorText, unwrap } from "@/shared/api";

export const TOO_MANY_FILES = `Не больше ${String(MAX_ATTACHMENTS)} файлов в одном сообщении`;
export const FILE_TOO_BIG = `Файл больше ${formatSize(MAX_ATTACHMENT_BYTES)}`;
export const EMPTY_FILE = "Пустой файл";
export const NO_ATTACH_PERMISSION = "У вас нет прав прикреплять файлы в этом канале";

/**
 * A file in the composer. It uploads the moment it is added, so it is
 * usually ready by the time the message is sent; `attachment` is the
 * server's pending attachment once it is.
 */
export interface Upload {
  key: string;
  file: File;
  /** Shows an image before and while it uploads; null for other files. */
  previewUrl: string | null;
  /** From 0 to 1. */
  progress: number;
  status: "uploading" | "done" | "failed";
  attachment: Attachment | null;
  /** Why it failed, in the user's words. */
  error: string | null;
}

interface UploadStore {
  /** The composer's files by channel, in the order they were added; a channel with none has no key. */
  byChannel: Record<string, Upload[]>;
}

const store = create<UploadStore>()(() => ({ byChannel: {} }));
/** Requests in flight by upload key, to abort when a file is dropped. */
const requests = new Map<string, XMLHttpRequest>();
/**
 * Uploads at once, in all: the server lets a user run two (more get 429),
 * so the rest wait their turn in `waiting`, in the order they were added.
 */
export const MAX_PARALLEL_UPLOADS = 2;
const waiting: { channelId: string; key: string; file: File }[] = [];

const NONE: Upload[] = [];

/** The files in the composer of `channelId`. */
export const useUploads = (channelId: string): Upload[] => store((s) => s.byChannel[channelId] ?? NONE);

const list = (channelId: string): Upload[] => store.getState().byChannel[channelId] ?? NONE;

/** The files in the composer of `channelId`, outside React. */
export const peekUploads = list;

const setList = (channelId: string, next: Upload[]): void => {
  const { [channelId]: _old, ...rest } = store.getState().byChannel;
  store.setState({ byChannel: next.length === 0 ? rest : { ...rest, [channelId]: next } });
};

const patch = (channelId: string, key: string, change: Partial<Upload>): void => {
  const current = list(channelId);
  if (!current.some((u) => u.key === key)) return;
  setList(
    channelId,
    current.map((u) => (u.key === key ? { ...u, ...change } : u)),
  );
};

const isPreviewable = (file: File) => /^image\/(png|jpeg|gif|webp)$/.test(file.type);

/** Why the server would refuse an upload, in the user's words. */
const uploadError = (status: number, body: string, headers: Headers): string => {
  if (status === 413) return FILE_TOO_BIG;
  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    parsed = undefined;
  }
  try {
    unwrap({ error: parsed, response: new Response(null, { status, headers }) });
  } catch (e) {
    if (e instanceof ApiError && e.code === "missing_permission") return NO_ATTACH_PERMISSION;
    return errorText(e);
  }
  return errorText(new ApiError(status, "unknown"));
};

const responseHeaders = (xhr: XMLHttpRequest): Headers => {
  const headers = new Headers();
  for (const line of xhr
    .getAllResponseHeaders()
    .trim()
    .split(/[\r\n]+/)) {
    const at = line.indexOf(":");
    if (at > 0) headers.append(line.slice(0, at).trim(), line.slice(at + 1).trim());
  }
  return headers;
};

/**
 * Uploads one file with XMLHttpRequest, the only browser API that reports
 * upload progress: POST /channels/{id}/attachments, multipart field `file`.
 */
const start = (channelId: string, key: string, file: File): void => {
  const xhr = new XMLHttpRequest();
  requests.set(key, xhr);
  xhr.open("POST", `/api/v1/channels/${encodeURIComponent(channelId)}/attachments`);
  xhr.withCredentials = true;
  xhr.upload.onprogress = (e) => {
    if (e.lengthComputable) patch(channelId, key, { progress: e.loaded / e.total });
  };
  xhr.onload = () => {
    finished(key);
    if (xhr.status === 201) {
      try {
        const attachment = JSON.parse(xhr.responseText) as Attachment;
        patch(channelId, key, { status: "done", progress: 1, attachment, error: null });
        return;
      } catch {
        // A proxy page instead of JSON: treated as a failure below.
      }
    }
    patch(channelId, key, {
      status: "failed",
      error: uploadError(xhr.status, xhr.responseText, responseHeaders(xhr)),
    });
  };
  xhr.onerror = () => {
    finished(key);
    patch(channelId, key, { status: "failed", error: errorText(new TypeError("network")) });
  };
  const form = new FormData();
  form.append("file", file);
  xhr.send(form);
};

/** Starts waiting uploads while there is room. */
const pump = (): void => {
  while (requests.size < MAX_PARALLEL_UPLOADS) {
    const next = waiting.shift();
    if (!next) return;
    start(next.channelId, next.key, next.file);
  }
};

/** An upload ended (or was aborted): its slot goes to the next one waiting. */
function finished(key: string): void {
  requests.delete(key);
  pump();
}

const enqueue = (channelId: string, key: string, file: File): void => {
  waiting.push({ channelId, key, file });
  pump();
};

/**
 * Adds files to the composer of `channelId` and uploads them, two at a time.
 * Files over the limits are left out; the returned text says why (null when
 * all were taken).
 */
export const addFiles = (channelId: string, files: readonly File[]): string | null => {
  let problem: string | null = null;
  const room = MAX_ATTACHMENTS - list(channelId).length;
  const taken: File[] = [];
  for (const file of files) {
    if (file.size === 0) problem ??= EMPTY_FILE;
    else if (file.size > MAX_ATTACHMENT_BYTES) problem ??= FILE_TOO_BIG;
    else if (taken.length >= room) problem ??= TOO_MANY_FILES;
    else taken.push(file);
  }
  const added = taken.map(
    (file): Upload => ({
      key: crypto.randomUUID(),
      file,
      previewUrl: isPreviewable(file) ? URL.createObjectURL(file) : null,
      progress: 0,
      status: "uploading",
      attachment: null,
      error: null,
    }),
  );
  if (added.length > 0) setList(channelId, [...list(channelId), ...added]);
  for (const u of added) enqueue(channelId, u.key, u.file);
  return problem;
};

/** Uploads a failed file again. */
export const retryUpload = (channelId: string, key: string): void => {
  const u = list(channelId).find((x) => x.key === key);
  if (u?.status !== "failed") return;
  patch(channelId, key, { status: "uploading", progress: 0, error: null });
  enqueue(channelId, key, u.file);
};

const release = (u: Upload): void => {
  if (u.previewUrl !== null) URL.revokeObjectURL(u.previewUrl);
};

/**
 * Takes a file out of the composer: an upload in flight is aborted, and an
 * uploaded one is deleted on the server rather than left to expire.
 */
export const removeUpload = (channelId: string, key: string): void => {
  const u = list(channelId).find((x) => x.key === key);
  if (!u) return;
  const at = waiting.findIndex((w) => w.key === key);
  if (at !== -1) waiting.splice(at, 1);
  requests.get(key)?.abort();
  finished(key);
  release(u);
  setList(
    channelId,
    list(channelId).filter((x) => x.key !== key),
  );
  if (u.attachment !== null) {
    void fetch(`/api/v1/channels/${encodeURIComponent(channelId)}/attachments/${encodeURIComponent(u.attachment.id)}`, {
      method: "DELETE",
      credentials: "include",
    }).catch(() => undefined);
  }
};

/** Whether the composer of `channelId` still uploads a file (it cannot send until it is done). */
export const isUploading = (uploads: readonly Upload[]): boolean => uploads.some((u) => u.status === "uploading");

/**
 * Hands the uploaded files of `channelId` to a message being sent and clears
 * them from the composer; failed ones stay, to be retried or removed.
 */
export const takeUploads = (channelId: string): Attachment[] => {
  const current = list(channelId);
  const done = current.filter((u) => u.status === "done" && u.attachment !== null);
  for (const u of done) release(u);
  setList(
    channelId,
    current.filter((u) => !done.includes(u)),
  );
  return done.flatMap((u) => (u.attachment ? [u.attachment] : []));
};

/** Drops every file of every composer (the session ended). */
export const resetUploads = (): void => {
  waiting.length = 0;
  for (const xhr of requests.values()) xhr.abort();
  requests.clear();
  for (const uploads of Object.values(store.getState().byChannel)) for (const u of uploads) release(u);
  store.setState({ byChannel: {} });
};
