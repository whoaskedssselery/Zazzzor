import { Check, ChevronDown } from "lucide-react";
import { Select as SelectPrimitive } from "radix-ui";
import { cn } from "../cn";
import { fieldClass } from "./Input";

export interface SelectOption {
  value: string;
  label: string;
  disabled?: boolean;
}

interface SelectProps {
  value?: string;
  defaultValue?: string;
  onValueChange?: (value: string) => void;
  options: SelectOption[];
  placeholder?: string;
  disabled?: boolean;
  id?: string;
  name?: string;
  "aria-label"?: string;
  "aria-invalid"?: boolean;
  className?: string;
}

/** A single choice from a short list (Radix Select: typeahead, arrows, Escape). */
export function Select({ options, placeholder = "Выберите…", className, ...props }: SelectProps) {
  const { value, defaultValue, onValueChange, disabled, name, id } = props;
  return (
    <SelectPrimitive.Root
      {...(value === undefined ? {} : { value })}
      {...(defaultValue === undefined ? {} : { defaultValue })}
      {...(onValueChange ? { onValueChange } : {})}
      {...(disabled === undefined ? {} : { disabled })}
      {...(name === undefined ? {} : { name })}
    >
      <SelectPrimitive.Trigger
        {...(id === undefined ? {} : { id })}
        aria-label={props["aria-label"]}
        aria-invalid={props["aria-invalid"]}
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
          className="z-(--z-dropdown) max-h-(--radix-select-content-available-height) min-w-(--radix-select-trigger-width) animate-fade-in overflow-hidden rounded-md bg-overlay p-1.5 text-fg shadow-overlay ring-1 ring-border"
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
}
