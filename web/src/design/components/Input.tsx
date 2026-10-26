import type { ComponentProps } from "react";
import { cn } from "../cn";

/**
 * Shared look of text fields: a filled well, no border at rest, the focus
 * ring on focus, a danger outline when aria-invalid is set (the form library
 * sets it together with the error text under the field).
 */
export const fieldClass =
  "w-full rounded-md bg-input px-3 text-base text-fg outline-none transition-[box-shadow] duration-(--duration-fast) " +
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus " +
  "aria-invalid:ring-2 aria-invalid:ring-danger disabled:cursor-not-allowed disabled:opacity-50";

export function Input({ className, type, ...props }: ComponentProps<"input">) {
  return <input type={type ?? "text"} className={cn(fieldClass, "h-10", className)} {...props} />;
}

interface FieldProps {
  /** id of the control the label is for. */
  htmlFor: string;
  label: string;
  /** Hint under the label, shown while there is no error. */
  hint?: string;
  /** Error text from the server's errors[].field or the form schema. */
  error?: string | undefined;
  /** id for the error text; pass it to the control's aria-describedby. */
  errorId?: string;
  children: React.ReactNode;
  className?: string;
}

/** Label, control, then the hint or the error, stacked the Discord way (label above the field). */
export function Field({ htmlFor, label, hint, error, errorId, children, className }: FieldProps) {
  return (
    <div className={cn("flex flex-col gap-2", className)}>
      <label htmlFor={htmlFor} className="text-sm font-semibold text-fg-muted">
        {label}
      </label>
      {children}
      {error ? (
        <p id={errorId} className="text-sm text-fg-danger">
          {error}
        </p>
      ) : (
        hint && <p className="text-sm text-fg-faint">{hint}</p>
      )}
    </div>
  );
}
