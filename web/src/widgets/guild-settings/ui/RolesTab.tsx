import { useMutation } from "@tanstack/react-query";
import { GripVertical, Lock, Plus } from "lucide-react";
import { type DragEvent, type KeyboardEvent, useState } from "react";
import { byRank, canManageRole, type GuildEntry, highestPosition, type Role } from "@/entities/guild";
import { api, errorText, unwrap } from "@/shared/api";
import { Button, ConfirmDialog, cn, LEAVE_UNSAVED, RoleColorDot, useToast } from "@/shared/ui";
import { RoleEditor } from "./RoleEditor";

const NEW_ROLE = "новая роль";

/**
 * Where the role at list index `index` lands: the list runs from the
 * highest role down, and positions run 1..n from the lowest up (@everyone,
 * at 0, is not in the list).
 */
export const positionAt = (index: number, count: number): number => count - index;

/**
 * The roles of a server: the list from the highest down with @everyone
 * last, and the editor of the chosen one. A role is moved by dragging it,
 * or with Alt+↑/↓ on it; only roles below the user's highest move, and
 * only to places still below it (the server checks the same). A new role
 * starts right above @everyone, named «новая роль», and opens in the
 * editor. Leaving a role with unsaved changes asks first.
 */
export const RolesTab = ({
  entry,
  meId,
  onDirtyChange,
}: {
  entry: GuildEntry;
  meId: string;
  onDirtyChange: (dirty: boolean) => void;
}) => {
  const everyone = entry.roles[entry.guild.id];
  const roles = Object.values(entry.roles)
    .filter((r) => r.id !== entry.guild.id)
    .sort(byRank);
  const isOwner = entry.guild.owner_id === meId;
  const top = isOwner ? Number.POSITIVE_INFINITY : highestPosition(entry, meId);
  const toast = useToast();

  const [selectedId, setSelectedId] = useState<string | undefined>(roles[0]?.id ?? everyone?.id);
  const [dirty, setDirty] = useState(false);
  const [pendingSelect, setPendingSelect] = useState<string | null>(null);
  const [dragging, setDragging] = useState<string | null>(null);
  const [over, setOver] = useState<number | null>(null);
  const selected = selectedId === undefined ? undefined : entry.roles[selectedId];

  const markDirty = (d: boolean) => {
    setDirty(d);
    onDirtyChange(d);
  };
  const select = (id: string) => {
    if (id === selectedId) return;
    if (dirty) setPendingSelect(id);
    else setSelectedId(id);
  };

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/guilds/{guild_id}/roles", {
          params: { path: { guild_id: entry.guild.id } },
          body: { name: NEW_ROLE },
        }),
      ),
    onSuccess: (role) => {
      // The editor shows it once its GUILD_ROLE_CREATE is in the store.
      setSelectedId(role.id);
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });

  const move = useMutation({
    mutationFn: async ({ role, index }: { role: Role; index: number }) =>
      unwrap(
        await api.PATCH("/guilds/{guild_id}/roles", {
          params: { path: { guild_id: entry.guild.id } },
          body: [{ id: role.id, position: positionAt(index, roles.length) }],
        }),
      ),
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });

  const movable = (r: Role) => canManageRole(entry, meId, r);
  /** Whether a role may land at list index `index`: still below the user's highest role. */
  const fits = (index: number) => positionAt(index, roles.length) < top;
  const moveTo = (role: Role, index: number) => {
    const from = roles.findIndex((r) => r.id === role.id);
    if (index === from || index < 0 || index >= roles.length || !movable(role) || !fits(index)) return;
    move.mutate({ role, index });
  };

  const onKeyDown = (e: KeyboardEvent, role: Role, index: number) => {
    if (!e.altKey || (e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
    e.preventDefault();
    moveTo(role, e.key === "ArrowUp" ? index - 1 : index + 1);
  };
  const onDrop = (e: DragEvent, index: number) => {
    e.preventDefault();
    const role = dragging === null ? undefined : entry.roles[dragging];
    setDragging(null);
    setOver(null);
    if (role) moveTo(role, index);
  };

  const row = (r: Role, index: number | null) => {
    const canMove = index !== null && movable(r);
    const current = r.id === selectedId;
    return (
      // biome-ignore lint/a11y/noNoninteractiveElementInteractions: dragging is the pointer way to reorder; the row's button takes Alt+↑/↓.
      <li
        key={r.id}
        draggable={canMove}
        onDragStart={(e) => {
          e.dataTransfer.effectAllowed = "move";
          setDragging(r.id);
        }}
        onDragEnd={() => {
          setDragging(null);
          setOver(null);
        }}
        onDragOver={(e) => {
          if (dragging === null || index === null || !fits(index)) return;
          e.preventDefault();
          setOver(index);
        }}
        onDrop={(e) => {
          if (index !== null) onDrop(e, index);
        }}
        className={cn("rounded-md", over === index && dragging !== r.id && "ring-2 ring-accent")}
      >
        <button
          type="button"
          aria-current={current ? "true" : undefined}
          aria-keyshortcuts={canMove ? "Alt+ArrowUp Alt+ArrowDown" : undefined}
          className={cn(
            "flex h-9 w-full cursor-pointer items-center gap-2 rounded-md px-2 text-left text-base transition-colors hover:bg-hover",
            current && "bg-selected",
            dragging === r.id && "opacity-50",
          )}
          onClick={() => {
            select(r.id);
          }}
          onKeyDown={(e) => {
            if (index !== null) onKeyDown(e, r, index);
          }}
        >
          {canMove ? (
            <GripVertical aria-hidden className="size-4 shrink-0 text-fg-faint" />
          ) : (
            <span aria-hidden className="size-4 shrink-0" />
          )}
          <RoleColorDot color={r.color} />
          <span className="min-w-0 flex-1 truncate">{r.id === entry.guild.id ? "@everyone" : r.name}</span>
          {index !== null && !movable(r) && (
            <Lock aria-label="Роль не ниже вашей высшей" className="size-4 shrink-0 text-fg-faint" />
          )}
        </button>
      </li>
    );
  };

  return (
    <section aria-labelledby="roles-title">
      <h2 id="roles-title" className="mb-1 text-xl font-semibold">
        Роли
      </h2>
      <p className="mb-5 text-sm text-fg-muted">
        Роли выше в списке главнее. Перетаскивайте их или двигайте клавишами Alt+↑ и Alt+↓.
      </p>
      <div className="flex gap-6 max-lg:flex-col">
        <div className="flex w-56 shrink-0 flex-col gap-2 max-lg:w-full">
          <Button
            variant="secondary"
            size="sm"
            loading={create.isPending}
            onClick={() => {
              create.mutate();
            }}
          >
            <Plus aria-hidden className="size-4" />
            Создать роль
          </Button>
          <ul aria-label="Роли сервера" className="flex flex-col gap-0.5">
            {roles.map((r, i) => row(r, i))}
            {everyone && row(everyone, null)}
          </ul>
        </div>
        {selected ? (
          <RoleEditor key={selected.id} entry={entry} meId={meId} role={selected} onDirtyChange={markDirty} />
        ) : (
          <p className="text-base text-fg-muted">Выберите роль слева.</p>
        )}
      </div>
      <ConfirmDialog
        open={pendingSelect !== null}
        onOpenChange={(open) => {
          if (!open) setPendingSelect(null);
        }}
        title={LEAVE_UNSAVED}
        description="Несохранённые изменения роли пропадут."
        confirmLabel="Уйти"
        tone="danger"
        onConfirm={() => {
          if (pendingSelect !== null) setSelectedId(pendingSelect);
          setPendingSelect(null);
          markDirty(false);
        }}
      />
    </section>
  );
};
