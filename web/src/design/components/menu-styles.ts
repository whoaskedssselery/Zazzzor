/**
 * Shared look of DropdownMenu and ContextMenu, so both read as one menu.
 * Floating surfaces get a shadow and a 1px ring: on the dark theme a shadow
 * alone does not separate a menu from the chat behind it.
 */
export const menuContentClass =
  "z-(--z-dropdown) min-w-48 animate-fade-in overflow-hidden rounded-md bg-overlay p-1.5 text-fg shadow-overlay ring-1 ring-border";

export const menuItemClass =
  "relative flex h-8 cursor-pointer items-center gap-2 rounded-sm px-2 text-sm font-medium outline-none select-none " +
  "data-disabled:cursor-not-allowed data-disabled:opacity-50 data-highlighted:bg-hover " +
  "[&>svg]:ml-auto [&>svg]:size-4.5 [&>svg]:shrink-0 [&>svg]:text-fg-muted";

/** Destructive items (delete, kick, ban) keep the danger text on hover. */
export const menuItemDangerClass = "text-fg-danger [&>svg]:text-fg-danger";

export const menuSeparatorClass = "mx-1 my-1.5 h-px bg-border";

export const menuLabelClass = "px-2 pt-1.5 pb-1 text-xs font-semibold text-fg-faint";
