import { ContextMenu as Menu } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "../cn";
import {
  menuContentClass,
  menuItemClass,
  menuItemDangerClass,
  menuLabelClass,
  menuSeparatorClass,
} from "./menu-styles";

/**
 * Right-click menus (messages, members, channels). Every action in a context
 * menu must also be reachable without a right click, from a visible button
 * or a dropdown, for keyboard and touch users.
 */
export const ContextMenu = Menu.Root;
export const ContextMenuTrigger = Menu.Trigger;
export const ContextMenuGroup = Menu.Group;

export const ContextMenuContent = ({ className, ...props }: ComponentProps<typeof Menu.Content>) => (
  <Menu.Portal>
    <Menu.Content collisionPadding={8} className={cn(menuContentClass, className)} {...props} />
  </Menu.Portal>
);

interface ItemProps extends ComponentProps<typeof Menu.Item> {
  variant?: "default" | "danger";
}

export const ContextMenuItem = ({ variant = "default", className, ...props }: ItemProps) => (
  <Menu.Item className={cn(menuItemClass, variant === "danger" && menuItemDangerClass, className)} {...props} />
);

export const ContextMenuSeparator = ({ className, ...props }: ComponentProps<typeof Menu.Separator>) => (
  <Menu.Separator className={cn(menuSeparatorClass, className)} {...props} />
);

export const ContextMenuLabel = ({ className, ...props }: ComponentProps<typeof Menu.Label>) => (
  <Menu.Label className={cn(menuLabelClass, className)} {...props} />
);
