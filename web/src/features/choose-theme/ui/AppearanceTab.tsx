import { RadioGroup } from "radix-ui";
import { cn, type ThemePreference, useTheme } from "@/shared/ui";

const OPTIONS: { value: ThemePreference; label: string; hint: string }[] = [
  { value: "dark", label: "Тёмная", hint: "Для вечеров и тёмных комнат." },
  { value: "light", label: "Светлая", hint: "Для дня и яркого света." },
  { value: "system", label: "Как в системе", hint: "Меняется вместе с темой устройства." },
];

/**
 * «Внешний вид»: the theme, applied at once and kept in this browser (the
 * choice is not part of the account, as each device may want its own).
 */
export const AppearanceTab = () => {
  const { theme, setTheme } = useTheme();
  return (
    <section aria-labelledby="appearance-title">
      <h2 id="appearance-title" className="mb-5 text-xl font-semibold">
        Внешний вид
      </h2>
      <h3 id="theme-label" className="mb-2 text-xs font-semibold text-fg-muted uppercase">
        Тема
      </h3>
      <RadioGroup.Root
        aria-labelledby="theme-label"
        value={theme}
        onValueChange={(v) => {
          setTheme(v as ThemePreference);
        }}
        className="flex max-w-100 flex-col gap-2"
      >
        {OPTIONS.map((o) => (
          <RadioGroup.Item
            key={o.value}
            value={o.value}
            aria-label={o.label}
            aria-describedby={`theme-${o.value}-hint`}
            className={cn(
              "group flex cursor-pointer items-center gap-3 rounded-md bg-control px-3 py-2.5 text-left transition-colors",
              "hover:bg-control-hover data-[state=checked]:bg-selected",
            )}
          >
            <span
              aria-hidden
              className="flex size-5 shrink-0 items-center justify-center rounded-full border-2 border-fg-muted group-data-[state=checked]:border-accent"
            >
              <RadioGroup.Indicator className="size-2.5 rounded-full bg-accent" />
            </span>
            <span className="min-w-0">
              <span className="block text-base font-medium">{o.label}</span>
              <span id={`theme-${o.value}-hint`} className="block text-sm text-fg-muted">
                {o.hint}
              </span>
            </span>
          </RadioGroup.Item>
        ))}
      </RadioGroup.Root>
    </section>
  );
};
