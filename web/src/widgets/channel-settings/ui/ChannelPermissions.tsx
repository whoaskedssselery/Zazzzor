import { useMutation } from "@tanstack/react-query";
import { Check, Plus, Slash, X } from "lucide-react";
import { useEffect, useState } from "react";
import {
  byRank,
  CHANNEL_PERMISSION_GROUPS,
  type Channel,
  canGrant,
  canManageRole,
  formatMask,
  type GuildEntry,
  has,
  parseMask,
  userDisplay,
} from "@/entities/guild";
import { api, errorText, unwrap } from "@/shared/api";
import {
  Button,
  cn,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
  FormAlert,
  RoleColorDot,
  SaveBar,
  useToast,
} from "@/shared/ui";

export const SYNCED = "Синхронизировано с категорией";
export const NOT_SYNCED = "Права отличаются от категории";

/** One target of an overwrite: a role (type 0) or a member (type 1). */
interface Target {
  id: string;
  type: 0 | 1;
  name: string;
  color: number;
}

type Tri = "deny" | "inherit" | "allow";

const triOf = (allow: bigint, deny: bigint, bit: bigint): Tri =>
  has(allow, bit) ? "allow" : has(deny, bit) ? "deny" : "inherit";

const TRI: { value: Tri; label: string; icon: typeof X; on: string }[] = [
  { value: "deny", label: "Запретить", icon: X, on: "bg-danger text-on-danger" },
  { value: "inherit", label: "Наследовать", icon: Slash, on: "bg-control-hover text-fg" },
  { value: "allow", label: "Разрешить", icon: Check, on: "bg-accent text-on-accent" },
];

/** Deny, inherit or allow one permission for the chosen target. */
const TriSwitch = ({
  id,
  name,
  value,
  disabled,
  onChange,
}: {
  id: string;
  name: string;
  value: Tri;
  disabled: boolean;
  onChange: (v: Tri) => void;
}) => (
  <fieldset className="flex shrink-0 overflow-hidden rounded-md border border-border-strong" disabled={disabled}>
    <legend className="sr-only">{name}</legend>
    {TRI.map((t) => (
      <label
        key={t.value}
        title={t.label}
        className={cn(
          "relative flex size-8 cursor-pointer items-center justify-center text-fg-muted transition-colors has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-focus",
          value === t.value ? t.on : "hover:bg-hover",
          disabled && "cursor-not-allowed opacity-50",
        )}
      >
        <input
          type="radio"
          name={id}
          className="sr-only"
          checked={value === t.value}
          aria-label={t.label}
          onChange={() => {
            onChange(t.value);
          }}
        />
        <t.icon aria-hidden className="size-4" strokeWidth={2.5} />
      </label>
    ))}
  </fieldset>
);

/**
 * The permission overwrites of a channel: its targets (@everyone always,
 * then the roles and members that have one), the chosen target's
 * permissions as deny / inherit / allow, and whether the channel follows
 * its category. A permission the user does not hold cannot be changed, nor
 * can a role not below their highest (the server's rules). Saving sets the
 * overwrite, or removes it when everything inherits.
 */
export const ChannelPermissions = ({
  entry,
  meId,
  channel,
  onDirtyChange,
}: {
  entry: GuildEntry;
  meId: string;
  channel: Channel;
  onDirtyChange: (dirty: boolean) => void;
}) => {
  const toast = useToast();
  const guildId = entry.guild.id;
  const targetOf = (id: string, type: 0 | 1): Target | null => {
    if (type === 0) {
      const role = entry.roles[id];
      return role ? { id, type, name: id === guildId ? "@everyone" : role.name, color: role.color } : null;
    }
    const member = entry.members[id];
    if (!member) return null;
    const d = userDisplay(entry, member.user);
    return { id, type, name: d.name, color: d.color };
  };
  const listed = channel.permission_overwrites
    .map((o) => targetOf(o.id, o.type))
    .filter((t): t is Target => t !== null && t.id !== guildId);
  const everyone = targetOf(guildId, 0);
  const targets = everyone ? [everyone, ...listed] : listed;
  const [chosenId, setChosenId] = useState(guildId);
  const [added, setAdded] = useState<Target | null>(null);
  const chosen = targets.find((t) => t.id === chosenId) ?? (added?.id === chosenId ? added : targets[0]);

  const overwrite = chosen ? channel.permission_overwrites.find((o) => o.id === chosen.id) : undefined;
  const baseAllow = parseMask(overwrite?.allow ?? "0") ?? 0n;
  const baseDeny = parseMask(overwrite?.deny ?? "0") ?? 0n;
  const [draft, setDraft] = useState({ key: "", allow: 0n, deny: 0n });
  const key = `${chosen?.id ?? ""}:${String(baseAllow)}:${String(baseDeny)}`;
  if (draft.key !== key) setDraft({ key, allow: baseAllow, deny: baseDeny });
  const dirty = draft.allow !== baseAllow || draft.deny !== baseDeny;
  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);

  const isOwner = entry.guild.owner_id === meId;
  const roleLocked = (t: Target) => {
    if (t.type !== 0 || isOwner) return false;
    const role = entry.roles[t.id];
    return role === undefined || !canManageRole(entry, meId, role);
  };
  const locked = chosen === undefined || roleLocked(chosen);

  const save = useMutation({
    mutationFn: async () => {
      if (!chosen) return;
      const path = { channel_id: channel.id, target_id: chosen.id };
      if (draft.allow === 0n && draft.deny === 0n) {
        unwrap(await api.DELETE("/channels/{channel_id}/permissions/{target_id}", { params: { path } }));
      } else {
        unwrap(
          await api.PUT("/channels/{channel_id}/permissions/{target_id}", {
            params: { path },
            body: { type: chosen.type, allow: formatMask(draft.allow), deny: formatMask(draft.deny) },
          }),
        );
      }
    },
    onSuccess: () => {
      setAdded(null);
    },
  });
  const remove = useMutation({
    mutationFn: async (t: Target) => {
      unwrap(
        await api.DELETE("/channels/{channel_id}/permissions/{target_id}", {
          params: { path: { channel_id: channel.id, target_id: t.id } },
        }),
      );
    },
    onSuccess: () => {
      setChosenId(guildId);
    },
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });
  const sync = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PATCH("/channels/{channel_id}", {
          params: { path: { channel_id: channel.id } },
          body: { sync_permissions: true },
        }),
      ),
    onError: (e) => {
      toast.show({ text: errorText(e), tone: "error" });
    },
  });

  const listedIds = new Set(targets.map((t) => t.id));
  const addableRoles = Object.values(entry.roles)
    .filter((r) => r.id !== guildId && !listedIds.has(r.id))
    .sort(byRank);
  const addableMembers = Object.values(entry.members).filter((m) => !listedIds.has(m.user.id));
  const choose = (t: Target) => {
    setAdded(listedIds.has(t.id) ? null : t);
    setChosenId(t.id);
  };

  const parent = channel.parent_id === null ? undefined : entry.channels[channel.parent_id];
  const shown = added && !listedIds.has(added.id) ? [...targets, added] : targets;
  return (
    <section aria-labelledby="channel-permissions-title">
      <h2 id="channel-permissions-title" className="mb-2 text-xl font-semibold">
        Права доступа
      </h2>
      {parent && (
        <div className="mb-5 flex items-center gap-3 rounded-md bg-panel px-3 py-2 text-sm">
          <p className="min-w-0 flex-1">
            {channel.permission_synced ? (
              <>
                <span className="font-semibold text-fg-success">{SYNCED}</span> «{parent.name}»
              </>
            ) : (
              <span className="text-fg-muted">{`${NOT_SYNCED} «${parent.name}»`}</span>
            )}
          </p>
          {!channel.permission_synced && (
            <Button
              size="sm"
              variant="secondary"
              loading={sync.isPending}
              onClick={() => {
                sync.mutate();
              }}
            >
              Синхронизировать
            </Button>
          )}
        </div>
      )}
      {(save.error ?? null) && <FormAlert>{errorText(save.error)}</FormAlert>}
      <div className="flex gap-6 max-lg:flex-col">
        <div className="flex w-56 shrink-0 flex-col gap-2 max-lg:w-full">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="secondary" size="sm">
                <Plus aria-hidden className="size-4" />
                Добавить роль или участника
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="max-h-80 overflow-y-auto">
              {addableRoles.length > 0 && <DropdownMenuLabel>Роли</DropdownMenuLabel>}
              {addableRoles.map((r) => (
                <DropdownMenuItem
                  key={r.id}
                  onSelect={() => {
                    choose({ id: r.id, type: 0, name: r.name, color: r.color });
                  }}
                >
                  {r.name}
                </DropdownMenuItem>
              ))}
              {addableMembers.length > 0 && <DropdownMenuLabel>Участники</DropdownMenuLabel>}
              {addableMembers.map((m) => {
                const d = userDisplay(entry, m.user);
                return (
                  <DropdownMenuItem
                    key={m.user.id}
                    onSelect={() => {
                      choose({ id: m.user.id, type: 1, name: d.name, color: d.color });
                    }}
                  >
                    {d.name}
                  </DropdownMenuItem>
                );
              })}
            </DropdownMenuContent>
          </DropdownMenu>
          <ul aria-label="Роли и участники с правами" className="flex flex-col gap-0.5">
            {shown.map((t) => (
              <li key={t.id}>
                <button
                  type="button"
                  aria-current={t.id === chosen?.id ? "true" : undefined}
                  className={cn(
                    "flex h-9 w-full cursor-pointer items-center gap-2 rounded-md px-2 text-left text-base hover:bg-hover",
                    t.id === chosen?.id && "bg-selected",
                  )}
                  onClick={() => {
                    choose(t);
                  }}
                >
                  {t.type === 0 && <RoleColorDot color={t.color} />}
                  <span className="min-w-0 flex-1 truncate">{t.name}</span>
                  {t.type === 1 && <span className="text-xs text-fg-faint">участник</span>}
                </button>
              </li>
            ))}
          </ul>
        </div>
        {chosen && (
          <div className="min-w-0 flex-1">
            <div className="mb-4 flex items-center gap-3">
              <h3 className="min-w-0 flex-1 truncate text-lg font-semibold">{chosen.name}</h3>
              {chosen.id !== guildId && overwrite && !locked && (
                <Button
                  size="sm"
                  variant="ghost"
                  className="text-fg-danger"
                  onClick={() => {
                    remove.mutate(chosen);
                  }}
                >
                  Убрать
                </Button>
              )}
            </div>
            {locked && (
              <p className="mb-4 rounded-md bg-panel px-3 py-2 text-sm text-fg-muted">
                Эта роль не ниже вашей высшей: менять её права нельзя.
              </p>
            )}
            <div className="flex flex-col gap-8">
              {CHANNEL_PERMISSION_GROUPS.map((group) => (
                <section key={group.title}>
                  <h4 className="mb-2 text-xs font-semibold text-fg-faint uppercase">{group.title}</h4>
                  <ul className="flex flex-col divide-y divide-border">
                    {group.items.map((item) => {
                      const id = `ow-${chosen.id}-${item.bit.toString()}`;
                      const grantable = canGrant(entry, meId, item.bit);
                      return (
                        <li key={id} className="flex items-start gap-4 py-3">
                          <div className="min-w-0 flex-1">
                            <p className="text-base font-medium">{item.name}</p>
                            <p className="mt-0.5 text-sm text-fg-muted">
                              {item.description}
                              {!locked && !grantable && (
                                <span className="mt-0.5 block text-fg-warning">У вас нет этого права</span>
                              )}
                            </p>
                          </div>
                          <TriSwitch
                            id={id}
                            name={item.name}
                            value={triOf(draft.allow, draft.deny, item.bit)}
                            disabled={locked || !grantable}
                            onChange={(v) => {
                              setDraft((d) => ({
                                ...d,
                                allow: v === "allow" ? d.allow | item.bit : d.allow & ~item.bit,
                                deny: v === "deny" ? d.deny | item.bit : d.deny & ~item.bit,
                              }));
                            }}
                          />
                        </li>
                      );
                    })}
                  </ul>
                </section>
              ))}
            </div>
          </div>
        )}
      </div>
      {dirty && !locked && (
        <SaveBar
          pending={save.isPending}
          onReset={() => {
            setDraft({ key, allow: baseAllow, deny: baseDeny });
            save.reset();
          }}
          onSave={() => {
            save.mutate();
          }}
        />
      )}
    </section>
  );
};
