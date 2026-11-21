import { LANGUAGES, resources } from "./resources";

/** Every key of `tree` as a dotted path, with its text. */
const leaves = (tree: unknown, prefix = ""): [string, string][] =>
  Object.entries(tree as Record<string, unknown>).flatMap(([k, v]) =>
    typeof v === "string" ? [[prefix + k, v] as [string, string]] : leaves(v, `${prefix}${k}.`),
  );

const PLURAL = /_(zero|one|two|few|many|other)$/;
/** A key without its plural suffix: Russian has three forms where English has two. */
const base = (key: string) => key.replace(PLURAL, "");
/** The plural forms each language must have for a counted text (Intl.PluralRules). */
const forms = (lng: string) =>
  [...new Intl.PluralRules(lng).resolvedOptions().pluralCategories].sort().map((c) => `_${c}`);

describe.each(LANGUAGES.filter((l) => l !== "ru"))("the %s texts", (lng) => {
  const namespaces = Object.keys(resources.ru) as (keyof typeof resources.ru)[];

  it("have the same namespaces as the Russian ones", () => {
    expect(Object.keys(resources[lng]).sort()).toEqual(namespaces.sort());
  });

  it.each(namespaces)("have every key of the Russian %s and no other", (ns) => {
    const ru = new Set(leaves(resources.ru[ns]).map(([k]) => base(k)));
    const own = new Set(leaves(resources[lng][ns]).map(([k]) => base(k)));
    expect([...own].sort()).toEqual([...ru].sort());
  });

  it.each(namespaces)("keep the placeholders of the Russian %s", (ns) => {
    const holes = (s: string) => [...(s.match(/\{\{\s*\w+\s*\}\}/g) ?? [])].sort();
    const ru = new Map(leaves(resources.ru[ns]).map(([k, v]) => [base(k), holes(v)]));
    for (const [k, v] of leaves(resources[lng][ns])) expect(holes(v), `${ns}:${k}`).toEqual(ru.get(base(k)));
  });

  it.each(namespaces)("are not left in Russian in %s", (ns) => {
    for (const [k, v] of leaves(resources[lng][ns])) expect(v, `${ns}:${k}`).not.toMatch(/\p{Script=Cyrillic}/u);
  });
});

describe.each(LANGUAGES)("the counted %s texts", (lng) => {
  it("have every plural form of the language", () => {
    for (const [ns, tree] of Object.entries(resources[lng])) {
      const counted = new Map<string, string[]>();
      for (const [k] of leaves(tree)) {
        const suffix = PLURAL.exec(k)?.[0];
        if (suffix) counted.set(base(k), [...(counted.get(base(k)) ?? []), suffix]);
      }
      for (const [k, have] of counted) expect(have.sort(), `${ns}:${k}`).toEqual(forms(lng));
    }
  });
});
