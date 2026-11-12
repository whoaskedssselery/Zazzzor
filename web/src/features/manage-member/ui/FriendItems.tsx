import { useNavigate } from "@tanstack/react-router";
import { Ban, MessageCircle, UserCheck, UserPlus, UserX } from "lucide-react";
import {
  RELATIONSHIP,
  requestErrorText,
  useGuilds,
  useOpenDirect,
  useRemoveRelationship,
  useRequestFriend,
  useSetRelationship,
} from "@/entities/guild";
import { ConfirmDialog, ContextMenuItem, DropdownMenuItem, useToast } from "@/shared/ui";

/** The question before blocking someone; mounted only while it is asked. */
export const BlockDialog = ({ userId, name, onClose }: { userId: string; name: string; onClose: () => void }) => {
  const block = useSetRelationship();
  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={`Заблокировать ${name}?`}
      description="Дружба и заявки между вами закончатся, новые заявки от этого человека перестанут приходить. Он об этом не узнает."
      confirmLabel="Заблокировать"
      tone="danger"
      pending={block.isPending}
      onConfirm={() => {
        block.mutate({ id: userId, type: 2 }, { onSettled: onClose });
      }}
    />
  );
};

/**
 * The personal items of someone's menu, by where the user stands with
 * them: «Написать сообщение» (a DM: the server says if it is not allowed);
 * add, accept, cancel the request, or unblock; and «Заблокировать» for
 * anyone not blocked yet, which asks first (`onBlock`).
 */
export const FriendItems = ({
  userId,
  username,
  kind,
  onBlock,
}: {
  userId: string;
  username: string;
  kind: "context" | "dropdown";
  onBlock: () => void;
}) => {
  const type = useGuilds((s) => s.relationships[userId]?.type);
  const toast = useToast();
  const request = useRequestFriend();
  const set = useSetRelationship();
  const remove = useRemoveRelationship();
  const open = useOpenDirect();
  const navigate = useNavigate();
  const Row = kind === "context" ? ContextMenuItem : DropdownMenuItem;

  let item = null;
  switch (type) {
    case undefined:
      item = (
        <Row
          onSelect={() => {
            request.mutate(username, {
              onSuccess: () => {
                toast.show({ text: "Заявка в друзья отправлена", tone: "success" });
              },
              onError: (e) => {
                toast.show({ text: requestErrorText(e), tone: "error" });
              },
            });
          }}
        >
          Добавить в друзья
          <UserPlus aria-hidden />
        </Row>
      );
      break;
    case RELATIONSHIP.INCOMING:
      item = (
        <Row
          onSelect={() => {
            set.mutate({ id: userId, type: 1 });
          }}
        >
          Принять заявку в друзья
          <UserCheck aria-hidden />
        </Row>
      );
      break;
    case RELATIONSHIP.OUTGOING:
      item = (
        <Row
          onSelect={() => {
            remove.mutate(userId);
          }}
        >
          Отменить заявку в друзья
          <UserX aria-hidden />
        </Row>
      );
      break;
    case RELATIONSHIP.BLOCKED:
      return (
        <Row
          onSelect={() => {
            remove.mutate(userId);
          }}
        >
          Разблокировать
          <Ban aria-hidden />
        </Row>
      );
    default:
      break;
  }
  return (
    <>
      <Row
        onSelect={() => {
          open.mutate(userId, {
            onSuccess: (channel) => {
              void navigate({ to: "/channels/@me/$channelId", params: { channelId: channel.id } });
            },
          });
        }}
      >
        Написать сообщение
        <MessageCircle aria-hidden />
      </Row>
      {item}
      <Row variant="danger" onSelect={onBlock}>
        Заблокировать
        <Ban aria-hidden />
      </Row>
    </>
  );
};
