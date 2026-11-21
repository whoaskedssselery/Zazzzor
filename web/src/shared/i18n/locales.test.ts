import { LANGUAGES, resources, SOURCE_LANGUAGE } from "./resources";

const source = resources[SOURCE_LANGUAGE];

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

describe.each(LANGUAGES.filter((l) => l !== SOURCE_LANGUAGE))("the %s texts", (lng) => {
  const namespaces = Object.keys(source) as (keyof typeof source)[];

  it("have the same namespaces as the English ones", () => {
    expect(Object.keys(resources[lng]).sort()).toEqual(namespaces.sort());
  });

  it.each(namespaces)("have every key of the English %s and no other", (ns) => {
    const want = new Set(leaves(source[ns]).map(([k]) => base(k)));
    const own = new Set(leaves(resources[lng][ns]).map(([k]) => base(k)));
    expect([...own].sort()).toEqual([...want].sort());
  });

  it.each(namespaces)("keep the placeholders of the English %s", (ns) => {
    const holes = (s: string) => [...(s.match(/\{\{\s*\w+\s*\}\}/g) ?? [])].sort();
    const want = new Map(leaves(source[ns]).map(([k, v]) => [base(k), holes(v)]));
    for (const [k, v] of leaves(resources[lng][ns])) expect(holes(v), `${ns}:${k}`).toEqual(want.get(base(k)));
  });
});

describe("the English texts", () => {
  it.each(Object.keys(source) as (keyof typeof source)[])("have no Russian left in %s", (ns) => {
    for (const [k, v] of leaves(source[ns])) expect(v, `${ns}:${k}`).not.toMatch(/\p{Script=Cyrillic}/u);
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
