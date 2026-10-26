import { Check, ChevronDown } from "lucide-react";
import { Select as SelectPrimitive } from "radix-ui";
import type { Ref } from "react";
import { cn } from "../cn";
import { fieldClass, useFieldControl } from "./Input";

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

interface SelectProps {
  value?: string;
  defaultValue?: string;
  onValueChange?: (value: string) => void;
  onOpenChange?: (open: boolean) => void;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  required?: boolean;
  id?: string;
  name?: string;
  /** The trigger button (React 19 ref as a prop), for focusing it from a form. */
  ref?: Ref<HTMLButtonElement>;
  "aria-label"?: string;
  "aria-describedby"?: string;
  "aria-invalid"?: boolean;
  className?: string;
}

/**
 * A single choice from a short list (Radix Select: typeahead, arrows, Escape).
 * Inside a Field it takes the field's id, error state and description.
 */
export const Select = ({
  options,
  placeholder = "Выберите…",
  className,
  ref,
  value,
  defaultValue,
  onValueChange,
  onOpenChange,
  disabled,
  required,
  name,
  id,
  "aria-label": ariaLabel,
  "aria-describedby": describedBy,
  "aria-invalid": invalid,
}: SelectProps) => {
  const wiring = useFieldControl({ id, "aria-describedby": describedBy, "aria-invalid": invalid });
  return (
    <SelectPrimitive.Root
      value={value}
      defaultValue={defaultValue}
      onValueChange={onValueChange}
      onOpenChange={onOpenChange}
      disabled={disabled}
      required={required}
      name={name}
    >
      <SelectPrimitive.Trigger
        ref={ref}
        {...wiring}
        aria-label={ariaLabel}
        aria-required={required || undefined}
        className={cn(
          fieldClass,
          "inline-flex h-10 cursor-pointer items-center justify-between gap-2 text-left data-placeholder:text-fg-faint",
          className,
        )}
      >
        <SelectPrimitive.Value placeholder={placeholder} />
        <SelectPrimitive.Icon>
          <ChevronDown aria-hidden className="size-4 text-fg-muted" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          position="popper"
          sideOffset={4}
          className="z-(--z-dropdown) max-h-(--radix-select-content-available-height) min-w-(--radix-select-trigger-width) animate-fade-in overflow-hidden rounded-md bg-overlay p-1.5 text-fg shadow-float ring-1 ring-border"
        >
          <SelectPrimitive.Viewport>
            {options.map((o) => (
              <SelectPrimitive.Item
                key={o.value}
                value={o.value}
                {...(o.disabled ? { disabled: true } : {})}
                className="relative flex h-8 cursor-pointer items-center rounded-sm pr-8 pl-2 text-sm font-medium outline-none select-none data-disabled:cursor-not-allowed data-disabled:opacity-50 data-highlighted:bg-hover"
              >
                <SelectPrimitive.ItemText>{o.label}</SelectPrimitive.ItemText>
                <SelectPrimitive.ItemIndicator className="absolute right-2 inline-flex text-link">
                  <Check aria-hidden className="size-4" />
                </SelectPrimitive.ItemIndicator>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
};
