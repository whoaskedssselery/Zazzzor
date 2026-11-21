import { useEffect, useMemo } from "react";
import { t } from "@/shared/i18n";

/** The image types the server takes for avatars and server icons. */
export const IMAGE_ACCEPT = "image/png,image/jpeg,image/gif,image/webp";
const IMAGE_TYPES = new Set(IMAGE_ACCEPT.split(","));
const MAX_IMAGE_BYTES = 4 * 1024 * 1024;
export const wrongImageText = (): string => t("common:image.wrong");
export const imageTooLargeText = (): string => t("common:image.tooLarge");

/** Why the server would refuse `file` as an avatar or icon, before sending it; null when it would not. */
export const imageProblem = (file: File): string | null => {
  if (!IMAGE_TYPES.has(file.type)) return wrongImageText();
  if (file.size > MAX_IMAGE_BYTES) return imageTooLargeText();
  return null;
};

/**
 * The `bodySerializer` of an upload: `file` as the multipart part `file`,
 * the shape of PUT /users/@me/avatar and PUT /guilds/{id}/icon.
 */
export const fileForm = (file: File) => () => {
  const form = new FormData();
  form.append("file", file);
  return form;
};

/** A local URL showing `file` while it is chosen, released when it changes. */
export const useObjectUrl = (file: File | null): string | null => {
  const url = useMemo(() => (file === null ? null : URL.createObjectURL(file)), [file]);
  useEffect(
    () => () => {
      if (url !== null) URL.revokeObjectURL(url);
    },
    [url],
  );
  return url;
};
