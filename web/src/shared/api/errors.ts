import { i18n, type Texts, t } from "@/shared/i18n";

type ErrorCode = keyof Texts["errors"]["codes"];
type FieldName = keyof Texts["errors"]["fields"];

/** Whether the source texts have `key` under `group`: an own key, never a path like "toString". */
const hasText = (group: "codes" | "fields", key: string): boolean =>
  Object.hasOwn((i18n.getResourceBundle("ru", "errors") as Texts["errors"])[group], key);

/**
 * The user-facing error texts, in the interface's language: the `errors`
 * namespace of shared/i18n, keyed by the `code` of the API's problem+json
 * responses (api/openapi.yaml). Codes without a text there fall back to the
 * generic one; `field` errors of validation_failed are shown under the form
 * fields instead.
 *
 * Deliberately left without a text (the generic one), because each one means
 * a bug in the client or the server rather than something the user can fix:
 * unsupported_media_type, upgrade_required, method_not_allowed, internal.
 */
export const genericErrorText = (): string => t("errors:generic");

/** A request that never got a response (offline, DNS, server down). */
export const networkErrorText = (): string => t("errors:network");

/** The text for the API's error `code`, or null for a code without one. */
export const errorCodeText = (code: string): string | null =>
  hasText("codes", code) ? t(`errors:codes.${code as ErrorCode}`) : null;

/**
 * Sign-in and registration past their limits (429 rate_limited): the wait
 * comes from Retry-After, in seconds under a minute and in whole minutes,
 * rounded up, from a minute on (registration waits up to an hour). Without
 * Retry-After the plain rate_limited text is used.
 */
export function tooManyAttemptsText(retryAfter: number | undefined): string {
  if (!retryAfter) return errorCodeText("rate_limited") ?? genericErrorText();
  const wait =
    retryAfter < 60
      ? t("errors:waitSeconds", { count: retryAfter })
      : t("errors:waitMinutes", { count: Math.ceil(retryAfter / 60) });
  return t("errors:tooManyAttempts", { wait });
}

/**
 * The text for a request field of a `validation_failed` error, and for the
 * same rules checked in the forms before sending. The server's own messages
 * are English and technical, so the UI shows these by field name instead; a
 * field without one gets the generic field text.
 */
export function fieldErrorText(field: string): string {
  return hasText("fields", field) ? t(`errors:fields.${field as FieldName}`) : t("errors:field");
}
