import { Check } from "lucide-react";
import { DropdownMenu as Menu } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../cn";
import { menuContentClass, menuItemClass, menuItemDangerClass, menuLabelClass, menuSeparatorClass } from "./menu-styles";

export const DropdownMenu = Menu.Root;
export const DropdownMenuTrigger = Menu.Trigger;
export const DropdownMenuGroup = Menu.Group;

export function DropdownMenuContent({ className, sideOffset = 6, ...props }: ComponentProps<typeof Menu.Content>) {
  return (
    <Menu.Portal>
      <Menu.Content sideOffset={sideOffset} collisionPadding={8} className={cn(menuContentClass, className)} {...props} />
    </Menu.Portal>
  );
}

interface ItemProps extends ComponentProps<typeof Menu.Item> {
  variant?: "default" | "danger";
}

/** A menu action. Put a trailing lucide icon after the text; it aligns right. */
export function DropdownMenuItem({ variant = "default", className, ...props }: ItemProps) {
  return <Menu.Item className={cn(menuItemClass, variant === "danger" && menuItemDangerClass, className)} {...props} />;
}

export function DropdownMenuCheckboxItem({ className, children, ...props }: ComponentProps<typeof Menu.CheckboxItem>) {
  return (
    <Menu.CheckboxItem className={cn(menuItemClass, "pr-8", className)} {...props}>
      {children}
      <Menu.ItemIndicator className="absolute right-2 inline-flex text-link">
        <Check aria-hidden className="size-4" />
      </Menu.ItemIndicator>
    </Menu.CheckboxItem>
  );
}

export function DropdownMenuSeparator({ className, ...props }: ComponentProps<typeof Menu.Separator>) {
  return <Menu.Separator className={cn(menuSeparatorClass, className)} {...props} />;
}

export function DropdownMenuLabel({ className, ...props }: ComponentProps<typeof Menu.Label>) {
  return <Menu.Label className={cn(menuLabelClass, className)} {...props} />;
}
