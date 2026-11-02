import { Copy, Ellipsis, Hash, Pencil, Reply, Trash2 } from "lucide-react";
import type { MouseEvent, ReactNode } from "react";
import type { Message } from "@/entities/message";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  IconButton,
  useToast,
} from "@/shared/ui";

/** What the user may do with one message, and what the chat does when they do. */
export interface MessageActionHandlers {
  message: Message;
  /** The author: only they edit. */
  canEdit: boolean;
  /** The author, or MANAGE_MESSAGES in the channel. */
  canDelete: boolean;
  /** READ_MESSAGE_HISTORY (a reply needs it) and SEND_MESSAGES. */
  canReply: boolean;
  onReply: () => void;
  onEdit: () => void;
  /** `skipConfirm` when Shift was held, as Discord does. */
  onDelete: (skipConfirm: boolean) => void;
}

/** Copies text and says so; a browser that refuses says that instead. */
const useCopy = () => {
  const toast = useToast();
  return (text: string, done: string) => {
    navigator.clipboard.writeText(text).then(
      () => {
        toast.show({ text: done, tone: "success" });
      },
      () => {
        toast.show({ text: "Не удалось скопировать", tone: "error" });
      },
    );
  };
};

interface Item {
  key: string;
  label: string;
  icon: typeof Copy;
  danger?: boolean;
  run: (e: { shiftKey: boolean }) => void;
}

/** The actions of a message in menu order: reply, edit, copy, then delete apart. */
const useItems = ({
  message,
  canEdit,
  canDelete,
  canReply,
  onReply,
  onEdit,
  onDelete,
}: MessageActionHandlers): Item[] => {
  const copy = useCopy();
  const items: Item[] = [];
  if (canReply) items.push({ key: "reply", label: "Ответить", icon: Reply, run: onReply });
  if (canEdit) items.push({ key: "edit", label: "Изменить", icon: Pencil, run: onEdit });
  items.push(
    {
      key: "copy-text",
      label: "Копировать текст",
      icon: Copy,
      run: () => {
        copy(message.content, "Текст скопирован");
      },
    },
    {
      key: "copy-id",
      label: "Копировать ID",
      icon: Hash,
      run: () => {
        copy(message.id, "ID скопирован");
      },
    },
  );
  if (canDelete) {
    items.push({
      key: "delete",
      label: "Удалить",
      icon: Trash2,
      danger: true,
      run: (e) => {
        onDelete(e.shiftKey);
      },
    });
  }
  return items;
};

/**
 * The bar over a message on hover or focus: reply, edit and delete at
 * hand, the rest under «Ещё». Shift+click on delete skips the question.
 */
export const MessageToolbar = (props: MessageActionHandlers) => {
  const items = useItems(props);
  const quick = items.filter((i) => i.key === "reply" || i.key === "edit" || i.key === "delete");
  return (
    <div className="absolute -top-4 right-4 z-10 hidden items-center rounded-md bg-overlay p-0.5 shadow-float group-focus-within:flex group-hover:flex has-[[data-state=open]]:flex">
      {quick.map((i) => (
        <IconButton
          key={i.key}
          size="sm"
          label={i.label}
          icon={i.icon}
          variant={i.danger ? "danger" : "ghost"}
          onClick={(e: MouseEvent) => {
            i.run({ shiftKey: e.shiftKey });
          }}
        />
      ))}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <IconButton size="sm" label="Ещё" icon={Ellipsis} />
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {items.map((i) => (
            <MenuRow key={i.key} item={i} kind="dropdown" />
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
};

const MenuRow = ({ item, kind }: { item: Item; kind: "dropdown" | "context" }) => {
  const Icon = item.icon;
  const Row = kind === "dropdown" ? DropdownMenuItem : ContextMenuItem;
  const Separator = kind === "dropdown" ? DropdownMenuSeparator : ContextMenuSeparator;
  return (
    <>
      {item.danger && <Separator />}
      <Row
        variant={item.danger ? "danger" : "default"}
        onSelect={() => {
          // From a menu, deleting always asks: Shift is for the quick button.
          item.run({ shiftKey: false });
        }}
      >
        <Icon aria-hidden className="size-4" />
        {item.label}
      </Row>
    </>
  );
};

/** The same actions on right click (or the context menu key) anywhere on the message. */
export const MessageContextMenu = ({ children, ...props }: MessageActionHandlers & { children: ReactNode }) => {
  const items = useItems(props);
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>{children}</ContextMenuTrigger>
      <ContextMenuContent>
        {items.map((i) => (
          <MenuRow key={i.key} item={i} kind="context" />
        ))}
      </ContextMenuContent>
    </ContextMenu>
  );
};
