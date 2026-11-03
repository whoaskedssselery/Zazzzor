import { has, PERMISSION_GROUPS } from "@/entities/guild";
import { Switch } from "@/shared/ui";

export const NOT_YOURS = "У вас нет этого права";

/**
 * A role's permissions as switches, in PERMISSION_GROUPS order. A switch
 * the user may not change (`canChange` false: they lack the bit, as the
 * server's CanGrant decides, or the role is above them) is disabled, and
 * says why when it is the bit they lack.
 */
export const PermissionList = ({
  mask,
  onChange,
  canChange,
  locked,
}: {
  mask: bigint;
  onChange: (mask: bigint) => void;
  canChange: (bit: bigint) => boolean;
  /** The whole role is out of reach: no reasons per switch. */
  locked: boolean;
}) => (
  <div className="flex flex-col gap-8">
    {PERMISSION_GROUPS.map((group) => (
      <section key={group.title} aria-labelledby={`perm-group-${group.title}`}>
        <h3 id={`perm-group-${group.title}`} className="mb-2 text-xs font-semibold text-fg-faint uppercase">
          {group.title}
        </h3>
        <ul className="flex flex-col divide-y divide-border">
          {group.items.map((item) => {
            const id = `perm-${item.bit.toString()}`;
            const allowed = !locked && canChange(item.bit);
            return (
              <li key={id} className="flex items-start gap-4 py-3">
                <div className="min-w-0 flex-1">
                  <label htmlFor={id} className="text-base font-medium">
                    {item.name}
                  </label>
                  <p id={`${id}-hint`} className="mt-0.5 text-sm text-fg-muted">
                    {item.description}
                    {!locked && !allowed && <span className="mt-0.5 block text-fg-warning">{NOT_YOURS}</span>}
                  </p>
                </div>
                <Switch
                  id={id}
                  aria-describedby={`${id}-hint`}
                  checked={has(mask, item.bit)}
                  disabled={!allowed}
                  onCheckedChange={(on) => {
                    onChange(on ? mask | item.bit : mask & ~item.bit);
                  }}
                />
              </li>
            );
          })}
        </ul>
      </section>
    ))}
  </div>
);
