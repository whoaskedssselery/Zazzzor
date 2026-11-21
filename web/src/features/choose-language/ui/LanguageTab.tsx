import { RadioGroup } from "radix-ui";
import { LANGUAGE_NAMES, LANGUAGES, type Language, useLanguage, useTranslation } from "@/shared/i18n";
import { cn } from "@/shared/ui";

/**
 * «Язык»: the interface language, applied at once and kept in this browser,
 * as the theme is. Each language is named in itself, so someone who landed
 * in the wrong one still finds their own.
 */
export const LanguageTab = () => {
  const { t } = useTranslation();
  const { language, setLanguage } = useLanguage();
  return (
    <section aria-labelledby="language-title" lang={language}>
      <h2 id="language-title" className="mb-5 text-xl font-semibold">
        {t("language.title")}
      </h2>
      <h3 id="language-label" className="mb-2 text-xs font-semibold text-fg-muted uppercase">
        {t("language.label")}
      </h3>
      <RadioGroup.Root
        aria-labelledby="language-label"
        value={language}
        onValueChange={(v) => {
          setLanguage(v as Language);
        }}
        className="flex max-w-100 flex-col gap-2"
      >
        {LANGUAGES.map((lng) => (
          <RadioGroup.Item
            key={lng}
            value={lng}
            lang={lng}
            aria-label={LANGUAGE_NAMES[lng]}
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
            <span className="text-base font-medium">{LANGUAGE_NAMES[lng]}</span>
          </RadioGroup.Item>
        ))}
      </RadioGroup.Root>
      <p className="mt-3 max-w-100 text-sm text-fg-muted">{t("language.hint")}</p>
    </section>
  );
};
