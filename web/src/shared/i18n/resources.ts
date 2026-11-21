import enCommon from "./locales/en/common.json";
import enErrors from "./locales/en/errors.json";
import ruCommon from "./locales/ru/common.json";
import ruErrors from "./locales/ru/errors.json";

/** The languages of the interface, Russian first: it is the original and every text is written in it first. */
export const LANGUAGES = ["ru", "en"] as const;
export type Language = (typeof LANGUAGES)[number];

/** Each language's own name, as the picker shows it whatever the current language. */
export const LANGUAGE_NAMES: Record<Language, string> = { ru: "Русский", en: "English" };

/**
 * Every text of the interface, by language and namespace. Russian is the
 * source: the keys and their types come from it, and locales.test.ts holds
 * English to the same keys.
 */
export const resources = {
  ru: { common: ruCommon, errors: ruErrors },
  en: { common: enCommon, errors: enErrors },
} as const satisfies Record<Language, Record<string, unknown>>;

/** The texts of the source language, Russian: the shape every language has. */
export type Texts = (typeof resources)["ru"];

export const isLanguage = (v: unknown): v is Language => (LANGUAGES as readonly unknown[]).includes(v);
