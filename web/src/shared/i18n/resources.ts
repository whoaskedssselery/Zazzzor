import enAuth from "./locales/en/auth.json";
import enCommon from "./locales/en/common.json";
import enErrors from "./locales/en/errors.json";
import enSettings from "./locales/en/settings.json";
import ruAuth from "./locales/ru/auth.json";
import ruCommon from "./locales/ru/common.json";
import ruErrors from "./locales/ru/errors.json";
import ruSettings from "./locales/ru/settings.json";

/**
 * The languages of the interface. English is the source: every text is
 * written in it first, and the others are translations of it.
 */
export const LANGUAGES = ["en", "ru"] as const;
export type Language = (typeof LANGUAGES)[number];

/**
 * Whether every text of the interface is translated. Until it is, nobody is
 * shown a mix of two languages: the interface starts in Russian whatever the
 * browser's language, and the settings list no language section (its page
 * still opens by its address, for those translating). translated.test.ts
 * ties this to the code: it may be true only with no Russian left outside
 * the texts, and must be once none is.
 */
export const TRANSLATED: boolean = false;

/** The source language, and the one shown for a text another language lacks. */
export const SOURCE_LANGUAGE: Language = "en";

/** Each language's own name, as the picker shows it whatever the current language. */
export const LANGUAGE_NAMES: Record<Language, string> = { en: "English", ru: "Русский" };

/**
 * Every text of the interface, by language and namespace. The keys and their
 * types come from English, and locales.test.ts holds every translation to the
 * same keys, placeholders and plural forms.
 */
export const resources = {
  en: { common: enCommon, errors: enErrors, settings: enSettings, auth: enAuth },
  ru: { common: ruCommon, errors: ruErrors, settings: ruSettings, auth: ruAuth },
} as const satisfies Record<Language, Record<string, unknown>>;

/** The texts of the source language: the shape every language has. */
export type Texts = (typeof resources)["en"];

/** The namespaces of the texts, one per area of the interface. */
export const NAMESPACES = Object.keys(resources.en) as (keyof Texts)[];

export const isLanguage = (v: unknown): v is Language => (LANGUAGES as readonly unknown[]).includes(v);
