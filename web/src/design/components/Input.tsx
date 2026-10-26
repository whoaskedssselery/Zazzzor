import { type ComponentProps, createContext, type ReactNode, use } from "react";
import { cn } from "../cn";

/**
 * Shared look of text fields: a filled well, no border at rest, the focus
 * ring on focus, a danger outline when aria-invalid is set.
 */
export const fieldClass =
  "w-full rounded-md bg-input px-3 text-base text-fg outline-none transition-[box-shadow] duration-(--duration-fast) " +
  "focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus " +
  "aria-invalid:ring-2 aria-invalid:ring-danger disabled:cursor-not-allowed disabled:opacity-50";

interface FieldContextValue {
  id: string;
  /** The hint's or the error's id, whichever is shown. */
  describedBy: string | undefined;
  invalid: boolean;
}

const FieldContext = createContext<FieldContextValue | null>(null);

/** Stable ids of a field's parts, derived from the control's id. */
export function fieldIds(id: string) {
  return { hintId: `${id}-hint`, errorId: `${id}-error` };
}

function joinIds(...ids: (string | undefined)[]): string | undefined {
  const joined = ids.filter(Boolean).join(" ");
  return joined || undefined;
}

interface FieldControlProps {
  id?: string | undefined;
  "aria-describedby"?: string | undefined;
  "aria-invalid"?: boolean | "true" | "false" | "grammar" | "spelling" | undefined;
}

/**
 * The id, aria-describedby and aria-invalid a control gets from the Field
 * around it; its own props win, and its own aria-describedby is kept after
 * the field's. Input, Textarea and Select call this; a custom control can too.
 */
export function useFieldControl(own: FieldControlProps) {
  const field = use(FieldContext);
  const invalid = own["aria-invalid"] ?? (field?.invalid ? true : undefined);
  return {
    id: own.id ?? field?.id,
    "aria-describedby": joinIds(field?.describedBy, own["aria-describedby"]),
    "aria-invalid": invalid,
  };
}

export function Input({
  className,
  type,
  id,
  "aria-describedby": describedBy,
  "aria-invalid": invalid,
  ...props
}: ComponentProps<"input">) {
  const wiring = useFieldControl({ id, "aria-describedby": describedBy, "aria-invalid": invalid });
  return <input type={type ?? "text"} className={cn(fieldClass, "h-10", className)} {...wiring} {...props} />;
}

interface FieldProps {
  /** id of the control; the control inside takes it from the Field. */
  htmlFor: string;
  label: string;
  /** Hint under the control, shown while there is no error. */
  hint?: string;
  /** Error text from the server's errors[].field or the form schema; marks the control invalid. */
  error?: string | undefined;
  children: ReactNode;
  className?: string;
}

/**
 * Label, control, then the hint or the error, stacked the Discord way. The
 * control inside (Input, Textarea, Select) gets the id, aria-invalid and an
 * aria-describedby pointing at the hint or the error (`fieldIds(htmlFor)`).
 */
export function Field({ htmlFor, label, hint, error, children, className }: FieldProps) {
  const { hintId, errorId } = fieldIds(htmlFor);
  const describedBy = error ? errorId : hint ? hintId : undefined;
  return (
    <FieldContext value={{ id: htmlFor, describedBy, invalid: Boolean(error) }}>
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
          hint && (
            <p id={hintId} className="text-sm text-fg-faint">
              {hint}
            </p>
          )
        )}
      </div>
    </FieldContext>
  );
}
