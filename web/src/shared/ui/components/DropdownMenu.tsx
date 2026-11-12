import { Check, ChevronRight, Circle } from "lucide-react";
import { DropdownMenu as Menu } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../cn";
import {
  menuContentClass,
  menuItemClass,
  menuItemDangerClass,
  menuLabelClass,
  menuSeparatorClass,
} from "./menu-styles";

export const DropdownMenu = Menu.Root;
export const DropdownMenuTrigger = Menu.Trigger;
export const DropdownMenuGroup = Menu.Group;
export const DropdownMenuSub = Menu.Sub;
export const DropdownMenuRadioGroup = Menu.RadioGroup;

export const DropdownMenuContent = ({ className, sideOffset = 6, ...props }: ComponentProps<typeof Menu.Content>) => (
  <Menu.Portal>
    <Menu.Content sideOffset={sideOffset} collisionPadding={8} className={cn(menuContentClass, className)} {...props} />
  </Menu.Portal>
);

interface ItemProps extends ComponentProps<typeof Menu.Item> {
  variant?: "default" | "danger";
}

/** A menu action. Put a trailing lucide icon after the text; it aligns right. */
export const DropdownMenuItem = ({ variant = "default", className, ...props }: ItemProps) => (
  <Menu.Item className={cn(menuItemClass, variant === "danger" && menuItemDangerClass, className)} {...props} />
);

export const DropdownMenuCheckboxItem = ({
  className,
  children,
  ...props
}: ComponentProps<typeof Menu.CheckboxItem>) => (
  <Menu.CheckboxItem className={cn(menuItemClass, "pr-8", className)} {...props}>
    {children}
    <Menu.ItemIndicator className="absolute right-2 inline-flex text-link">
      <Check aria-hidden className="size-4" />
    </Menu.ItemIndicator>
  </Menu.CheckboxItem>
);

export const DropdownMenuSeparator = ({ className, ...props }: ComponentProps<typeof Menu.Separator>) => (
  <Menu.Separator className={cn(menuSeparatorClass, className)} {...props} />
);

export const DropdownMenuLabel = ({ className, ...props }: ComponentProps<typeof Menu.Label>) => (
  <Menu.Label className={cn(menuLabelClass, className)} {...props} />
);

/** Opens a nested menu: the text, then a chevron. */
export const DropdownMenuSubTrigger = ({ className, children, ...props }: ComponentProps<typeof Menu.SubTrigger>) => (
  <Menu.SubTrigger className={cn(menuItemClass, "data-[state=open]:bg-hover", className)} {...props}>
    {children}
    <ChevronRight aria-hidden />
  </Menu.SubTrigger>
);

export const DropdownMenuSubContent = ({
  className,
  sideOffset = 6,
  ...props
}: ComponentProps<typeof Menu.SubContent>) => (
  <Menu.Portal>
    <Menu.SubContent
      sideOffset={sideOffset}
      collisionPadding={8}
      className={cn(menuContentClass, className)}
      {...props}
    />
  </Menu.Portal>
);

/** One choice of a DropdownMenuRadioGroup; the chosen one shows a dot on the right. */
export const DropdownMenuRadioItem = ({ className, children, ...props }: ComponentProps<typeof Menu.RadioItem>) => (
  <Menu.RadioItem className={cn(menuItemClass, "pr-8", className)} {...props}>
    {children}
    <Menu.ItemIndicator className="absolute right-2 inline-flex text-link">
      <Circle aria-hidden className="size-2.5 fill-current" />
    </Menu.ItemIndicator>
  </Menu.RadioItem>
);
