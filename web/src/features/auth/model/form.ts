import { z } from "zod/mini";
import { ApiError, errorCodeText, errorText, fieldErrorText, tooManyAttemptsText } from "@/shared/api";
import { t } from "@/shared/i18n";
import { runeLength, trimContent } from "@/shared/lib";

/** The server's username rule, after the input is lower-cased. */
const USERNAME = /^[a-z0-9_.]{2,32}$/;

const username = z
  .string()
  .check(
    z.minLength(1, { error: () => t("auth:enterUsername") }),
    z.regex(USERNAME, { error: () => fieldErrorText("username") }),
  );

/** 8 to 128 characters as the server counts them (code points, not UTF-16 units). */
const password = z.string().check(
  z.minLength(1, { error: () => t("auth:enterPassword") }),
  z.refine((s) => runeLength(s) >= 8 && runeLength(s) <= 128, { error: () => fieldErrorText("password") }),
);

/**
 * The same rules on both forms: no account can have a login or password
 * outside them, so a sign-in attempt that breaks them is refused here
 * instead of spending one of the 10 attempts the server allows per minute.
 */
export const loginSchema = z.object({ username, password });
export type LoginValues = z.infer<typeof loginSchema>;

export const registerSchema = z.object({
  // Optional: empty (or only spaces) means "same as the login".
  display_name: z.string().check(z.refine((s) => runeLength(trimContent(s)) <= 32, fieldErrorText("display_name"))),
  username,
  password,
});
export type RegisterValues = z.infer<typeof registerSchema>;

/**
 * Lower-cases a login input as it is typed, keeping the caret where it was.
 * Call it in onChange before the form library reads the value.
 */
export function lowerCaseInput(input: HTMLInputElement): void {
  const lower = input.value.toLowerCase();
  if (lower === input.value) return;
  const { selectionStart, selectionEnd } = input;
  input.value = lower;
  if (selectionStart !== null && selectionEnd !== null) input.setSelectionRange(selectionStart, selectionEnd);
}

/** Whether `field` (from the server's errors[]) is one of the form's fields. */
export function isFieldOf<S extends z.ZodMiniObject>(
  schema: S,
  field: string,
): field is Extract<keyof z.infer<S>, string> {
  return Object.hasOwn(schema.shape, field);
}

/**
 * Shows an error of a sign-in or registration request. Field errors of the
 * server go under the form's fields through `placeField` (the text by
 * field name; it answers false for a field the form does not have), and the
 * rest comes back as the text for the form's alert, or null when every part
 * of the error landed under a field.
 */
export function showRequestError(e: unknown, placeField: (field: string, message: string) => boolean): string | null {
  if (!(e instanceof ApiError)) return errorText(e);
  if (e.code === "rate_limited") return tooManyAttemptsText(e.retryAfter);
  if (e.code === "username_taken" && placeField("username", errorCodeText("username_taken") ?? errorText(e)))
    return null;
  let placed = false;
  for (const field of Object.keys(e.fields)) {
    if (placeField(field, fieldErrorText(field))) placed = true;
  }
  return placed ? null : errorText(e);
}
