import { useMutation } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { useEffect, useState } from "react";
import { canGrant, canManageRole, formatMask, type GuildEntry, parseMask, type Role } from "@/entities/guild";
import { api, errorText, unwrap } from "@/shared/api";
import { runeLength, trimContent } from "@/shared/lib";
import {
  Button,
  Checkbox,
  ConfirmDialog,
  cn,
  Field,
  FormAlert,
  Input,
  roleColorCss,
  SaveBar,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/shared/ui";
import { PermissionList } from "./PermissionList";

export const ROLE_LOCKED = "Эта роль не ниже вашей высшей: изменить её нельзя.";
const NAME_RULE = "Название роли: от 1 до 100 символов.";

/** Discord's role palette: two rows, the brighter ones first. */
// biome-ignore format: two rows of ten, as the palette shows them.
export const ROLE_COLORS = [
  0x1abc9c, 0x2ecc71, 0x3498db, 0x9b59b6, 0xe91e63, 0xf1c40f, 0xe67e22, 0xe74c3c, 0x95a5a6, 0x607d8b,
  0x11806a, 0x1f8b4c, 0x206694, 0x71368a, 0xad1457, 0xc27c0e, 0xa84300, 0x992d22, 0x979c9f, 0x546e7a,
];

interface Draft {
  name: string;
  color: number;
  hoist: boolean;
  mentionable: boolean;
  permissions: bigint;
}

const draftOf = (role: Role): Draft => ({
  name: role.name,
  color: role.color,
  hoist: role.hoist,
  mentionable: role.mentionable,
  permissions: parseMask(role.permissions) ?? 0n,
});

/** The fields of `draft` that differ from `role`, as PATCH takes them. */
const changesOf = (role: Role, draft: Draft) => {
  const base = draftOf(role);
  const name = trimContent(draft.name);
  return {
    ...(name !== base.name ? { name } : {}),
    ...(draft.color !== base.color ? { color: draft.color } : {}),
    ...(draft.hoist !== base.hoist ? { hoist: draft.hoist } : {}),
    ...(draft.mentionable !== base.mentionable ? { mentionable: draft.mentionable } : {}),
    ...(draft.permissions !== base.permissions ? { permissions: formatMask(draft.permissions) } : {}),
  };
};

const ColorPicker = ({
  color,
  onChange,
  disabled,
}: {
  color: number;
  onChange: (c: number) => void;
  disabled: boolean;
}) => {
  const swatch = (c: number, label: string) => (
    <button
      key={c}
      type="button"
      aria-label={label}
      aria-pressed={color === c}
      disabled={disabled}
      className={cn(
        "size-8 cursor-pointer rounded-md ring-offset-2 ring-offset-chat disabled:cursor-not-allowed disabled:opacity-50",
        color === c && "ring-2 ring-fg",
        c === 0 && "border-2 border-border-strong",
      )}
      style={c === 0 ? undefined : { backgroundColor: roleColorCss(c) ?? undefined }}
      onClick={() => {
        onChange(c);
      }}
    />
  );
  return (
    <fieldset>
      <legend className="mb-2 text-sm font-semibold">Цвет роли</legend>
      <div className="flex flex-wrap items-center gap-2">
        {swatch(0, "Без цвета")}
        <label className="relative size-8 cursor-pointer overflow-hidden rounded-md border-2 border-border-strong">
          <span className="sr-only">Свой цвет</span>
          <input
            type="color"
            disabled={disabled}
            className="absolute -inset-2 size-12 cursor-pointer disabled:cursor-not-allowed"
            value={roleColorCss(color) ?? "#99aab5"}
            onChange={(e) => {
              onChange(Number.parseInt(e.currentTarget.value.slice(1), 16));
            }}
          />
        </label>
        <span aria-hidden className="mx-1 h-8 w-px bg-border" />
        <div className="grid grid-cols-10 gap-2">
          {ROLE_COLORS.map((c) => swatch(c, `Цвет ${roleColorCss(c) ?? ""}`))}
        </div>
      </div>
    </fieldset>
  );
};

/** A checkbox with its label beside it. */
const CheckRow = ({
  id,
  label,
  checked,
  disabled,
  onChange,
}: {
  id: string;
  label: string;
  checked: boolean;
  disabled: boolean;
  onChange: (on: boolean) => void;
}) => (
  <div className="flex items-center gap-3">
    <Checkbox
      id={id}
      checked={checked}
      disabled={disabled}
      onCheckedChange={(on) => {
        onChange(on === true);
      }}
    />
    <label htmlFor={id} className="text-base">
      {label}
    </label>
  </div>
);

/**
 * One role's settings: how it shows (name, colour, shown apart, may be
 * mentioned) and its permissions. A role not below the user's highest one
 * is read-only and says so; of @everyone only the permissions change. A
 * permission the user does not hold cannot be switched (the server's
 * CanGrant). Changes wait for «Сохранить изменения»; the store follows
 * the server's GUILD_ROLE_UPDATE.
 */
export const RoleEditor = ({
  entry,
  meId,
  role,
  onDirtyChange,
}: {
  entry: GuildEntry;
  meId: string;
  role: Role;
  onDirtyChange: (dirty: boolean) => void;
}) => {
  const everyone = role.id === entry.guild.id;
  const locked = !canManageRole(entry, meId, role);
  const [draft, setDraft] = useState<Draft>(() => draftOf(role));
  const [nameError, setNameError] = useState<string | undefined>(undefined);
  const [deleting, setDeleting] = useState(false);

  // A change by someone else shows here while this form has nothing of its own.
  const [base, setBase] = useState(role);
  if (base !== role) {
    const untouched = Object.keys(changesOf(base, draft)).length === 0;
    setBase(role);
    if (untouched) setDraft(draftOf(role));
  }

  const changes = changesOf(role, draft);
  const dirty = Object.keys(changes).length > 0;
  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

  const path = { guild_id: entry.guild.id, role_id: role.id };
  const save = useMutation({
    mutationFn: async () =>
      unwrap(await api.PATCH("/guilds/{guild_id}/roles/{role_id}", { params: { path }, body: changes })),
  });
  const remove = useMutation({
    mutationFn: async () => {
      unwrap(await api.DELETE("/guilds/{guild_id}/roles/{role_id}", { params: { path } }));
    },
  });

  const submit = () => {
    const length = runeLength(trimContent(draft.name));
    if (length < 1 || length > 100) {
      setNameError(NAME_RULE);
      return;
    }
    setNameError(undefined);
    save.mutate();
  };
  const set = (patch: Partial<Draft>) => {
    setDraft((d) => ({ ...d, ...patch }));
  };

  const error = save.error ?? remove.error;
  return (
    <section aria-labelledby="role-editor-title" className="min-w-0 flex-1">
      <h3 id="role-editor-title" className="mb-4 flex items-center gap-2 text-lg font-semibold">
        <span className="truncate">{`Изменить роль — ${role.name}`}</span>
        {locked && <Lock aria-hidden className="size-4 shrink-0 text-fg-faint" />}
      </h3>
      {locked && <p className="mb-4 rounded-md bg-panel px-3 py-2 text-sm text-fg-muted">{ROLE_LOCKED}</p>}
      {error && <FormAlert>{errorText(error)}</FormAlert>}
      <Tabs defaultValue={everyone ? "permissions" : "display"}>
        <TabsList aria-label="Настройки роли">
          {!everyone && <TabsTrigger value="display">Отображение</TabsTrigger>}
          <TabsTrigger value="permissions">Права</TabsTrigger>
        </TabsList>
        {!everyone && (
          <TabsContent value="display" className="flex flex-col gap-6 pt-6">
            <Field htmlFor="role-name" label="Название роли" error={nameError}>
              <Input
                value={draft.name}
                disabled={locked}
                maxLength={200}
                autoComplete="off"
                onChange={(e) => {
                  set({ name: e.currentTarget.value });
                }}
              />
            </Field>
            <ColorPicker
              color={draft.color}
              disabled={locked}
              onChange={(color) => {
                set({ color });
              }}
            />
            <div className="flex flex-col gap-3">
              <CheckRow
                id="role-hoist"
                label="Показывать участников с этой ролью отдельно"
                checked={draft.hoist}
                disabled={locked}
                onChange={(hoist) => {
                  set({ hoist });
                }}
              />
              <CheckRow
                id="role-mentionable"
                label="Разрешить упоминать эту роль"
                checked={draft.mentionable}
                disabled={locked}
                onChange={(mentionable) => {
                  set({ mentionable });
                }}
              />
            </div>
            {!locked && (
              <div>
                <Button
                  variant="danger"
                  onClick={() => {
                    setDeleting(true);
                  }}
                >
                  Удалить роль
                </Button>
              </div>
            )}
          </TabsContent>
        )}
        <TabsContent value="permissions" className="pt-6">
          <PermissionList
            mask={draft.permissions}
            locked={locked}
            canChange={(bit) => canGrant(entry, meId, bit)}
            onChange={(permissions) => {
              set({ permissions });
            }}
          />
        </TabsContent>
      </Tabs>
      {dirty && !locked && (
        <SaveBar
          pending={save.isPending}
          onReset={() => {
            setDraft(draftOf(role));
            setNameError(undefined);
            save.reset();
          }}
          onSave={submit}
        />
      )}
      <ConfirmDialog
        open={deleting}
        onOpenChange={setDeleting}
        title={`Удалить роль «${role.name}»?`}
        description="Участники потеряют её, а права каналов для неё пропадут."
        confirmLabel="Удалить роль"
        tone="danger"
        onConfirm={() => {
          remove.mutate();
          setDeleting(false);
        }}
      />
    </section>
  );
};
