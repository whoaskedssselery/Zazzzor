import i18next from "i18next";
import { initReactI18next, useTranslation } from "react-i18next";
import { isLanguage, type Language, NAMESPACES, resources, SOURCE_LANGUAGE, TRANSLATED } from "./resources";

export const LANGUAGE_STORAGE_KEY = "zazzzor.language";

/**
 * The language a browser asks for: the first of its own languages that the
 * interface has, else English (a stranger is likelier to read it than
 * Russian).
 */
export const browserLanguage = (): Language => {
  const wanted =
    typeof navigator === "undefined" ? [] : navigator.languages.length > 0 ? navigator.languages : [navigator.language];
  for (const tag of wanted) {
    const base = tag.toLowerCase().split("-")[0];
    if (isLanguage(base)) return base;
  }
  return SOURCE_LANGUAGE;
};

/**
 * The language to start in: the one chosen in this browser, else the
 * browser's own once the interface is translated (Russian until then).
 */
export const readLanguage = (): Language => {
  try {
    const stored = localStorage.getItem(LANGUAGE_STORAGE_KEY);
    if (isLanguage(stored)) return stored;
  } catch {
    // Storage blocked (private mode): the browser's languages decide.
  }
  return TRANSLATED ? browserLanguage() : "ru";
};

/**
 * The one i18next instance. Every text is bundled, so it is ready as soon as
 * this module loads, before the first render, and `t` works outside React
 * too (the error texts of shared/api).
 */
export const i18n = i18next.createInstance();
void i18n.use(initReactI18next).init({
  resources,
  lng: readLanguage(),
  fallbackLng: SOURCE_LANGUAGE,
  defaultNS: "common",
  ns: NAMESPACES,
  initAsync: false,
  // React escapes what it renders; escaping here too would show &amp; and the like.
  interpolation: { escapeValue: false },
  returnNull: false,
});

const syncDocument = (lng: string) => {
  if (typeof document !== "undefined") document.documentElement.lang = lng;
};
syncDocument(i18n.language);
i18n.on("languageChanged", syncDocument);

/** The texts' `t` for code outside React; it reads the language at each call. */
export const t = i18n.t.bind(i18n);

/** The current language. */
export const currentLanguage = (): Language => (isLanguage(i18n.language) ? i18n.language : SOURCE_LANGUAGE);

/** Switches the interface to `lng` at once and keeps the choice in this browser. */
export const setLanguage = (lng: Language): void => {
  try {
    localStorage.setItem(LANGUAGE_STORAGE_KEY, lng);
  } catch {
    // Storage blocked: the choice lasts for this tab only.
  }
  void i18n.changeLanguage(lng);
};

/** The current language, re-rendering when it changes, and its setter. */
export const useLanguage = (): { language: Language; setLanguage: (lng: Language) => void } => {
  const { i18n: instance } = useTranslation();
  return { language: isLanguage(instance.language) ? instance.language : SOURCE_LANGUAGE, setLanguage };
};

export { useTranslation };

/**
 * A constant table of texts whose values are read when used, in the language
 * of that moment: `PRESENCE_LABEL[status]` stays as it was, but each value
 * is `read()` again on every access.
 */
export const textTable = <K extends string>(read: Record<K, () => string>): Readonly<Record<K, string>> => {
  const table = {} as Record<K, string>;
  for (const key of Object.keys(read) as K[]) Object.defineProperty(table, key, { get: read[key], enumerable: true });
  return table;
};
