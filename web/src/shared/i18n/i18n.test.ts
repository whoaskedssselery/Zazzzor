import { browserLanguage, i18n, LANGUAGE_STORAGE_KEY, readLanguage, setLanguage, t } from "./i18n";
import { TRANSLATED } from "./resources";

const languages = (list: string[]) => {
  vi.spyOn(navigator, "languages", "get").mockReturnValue(list);
};

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
});

describe("readLanguage", () => {
  it("keeps the language chosen in this browser", () => {
    languages(["ru-RU"]);
    localStorage.setItem(LANGUAGE_STORAGE_KEY, "en");
    expect(readLanguage()).toBe("en");
  });

  it("ignores garbage in storage, and follows the browser only once everything is translated", () => {
    languages(["en-US"]);
    localStorage.setItem(LANGUAGE_STORAGE_KEY, "klingon");
    expect(readLanguage()).toBe(TRANSLATED ? "en" : "ru");
  });
});

describe("browserLanguage", () => {
  it("takes the first of the browser's languages that the interface has", () => {
    languages(["de-DE", "ru-RU", "en-US"]);
    expect(browserLanguage()).toBe("ru");
  });

  it("is English for anyone else", () => {
    languages(["de-DE", "fr"]);
    expect(browserLanguage()).toBe("en");
  });
});

describe("setLanguage", () => {
  it("switches the texts and the page's language at once, and keeps the choice", () => {
    setLanguage("en");
    expect(t("errors:network")).toBe("Can't reach the server. Check your connection.");
    expect(document.documentElement.lang).toBe("en");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("en");
    setLanguage("ru");
    expect(t("errors:network")).toBe("Нет связи с сервером. Проверьте подключение.");
    expect(i18n.language).toBe("ru");
  });
});
