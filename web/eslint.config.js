import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
import boundaries from "eslint-plugin-boundaries";
import react from "eslint-plugin-react";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

// HTML from strings is how markup injection happens. Messages are rendered as
// React elements from our own markdown AST, so none of these is ever needed.
const htmlSinks = [
  "innerHTML",
  "outerHTML",
  "insertAdjacentHTML",
  "createContextualFragment",
  "setHTMLUnsafe",
  "parseHTMLUnsafe",
].map((property) => ({
  property,
  message: "Build DOM with React elements; HTML strings are banned in web/.",
}));

// react/no-danger only sees the JSX attribute. These catch the prop smuggled
// in through an object (`<div {...{ dangerouslySetInnerHTML }} />`,
// createElement props) and HTML parsed into an iframe via srcDoc. A spread of
// an object built elsewhere is caught where that object literal is written.
const htmlProps = [
  {
    selector: 'Property[key.name="dangerouslySetInnerHTML"], Property[key.value="dangerouslySetInnerHTML"]',
    message: "dangerouslySetInnerHTML is banned in web/, including inside props objects.",
  },
  {
    selector: 'MemberExpression[property.name="dangerouslySetInnerHTML"]',
    message: "dangerouslySetInnerHTML is banned in web/.",
  },
  {
    selector: 'JSXAttribute[name.name="srcDoc"], Property[key.name="srcDoc"]',
    message: "srcDoc renders an HTML string; it is banned in web/.",
  },
];

// Biome (biome.json at the repo root) is the first linter. These ESLint rules
// have a Biome twin that is on for web/, so ESLint stays quiet about them and
// no problem is reported twice. ESLint keeps what Biome cannot do: the
// type-aware typescript-eslint rules, the HTML-sink and eval bans above,
// react-hooks and react/function-component-definition. The security bans and
// react-hooks stay ESLint's own; biome.json turns their Biome twins off.
const biomeOwns = [
  "@typescript-eslint/no-array-constructor",
  "@typescript-eslint/no-duplicate-enum-values",
  "@typescript-eslint/no-empty-object-type",
  "@typescript-eslint/no-explicit-any",
  "@typescript-eslint/no-extra-non-null-assertion",
  "@typescript-eslint/no-extraneous-class",
  "@typescript-eslint/no-invalid-void-type",
  "@typescript-eslint/no-misused-new",
  "@typescript-eslint/no-mixed-enums",
  "@typescript-eslint/no-namespace",
  "@typescript-eslint/no-non-null-asserted-optional-chain",
  "@typescript-eslint/no-non-null-assertion",
  "@typescript-eslint/no-require-imports",
  "@typescript-eslint/no-unnecessary-type-constraint",
  "@typescript-eslint/no-unsafe-declaration-merging",
  "@typescript-eslint/no-unsafe-function-type",
  "@typescript-eslint/no-unused-vars",
  "@typescript-eslint/no-useless-constructor",
  "@typescript-eslint/no-wrapper-object-types",
  "@typescript-eslint/prefer-as-const",
  "@typescript-eslint/prefer-literal-enum-member",
  "@typescript-eslint/prefer-namespace-keyword",
  "@typescript-eslint/require-await",
  "@typescript-eslint/unified-signatures",
  "constructor-super",
  "for-direction",
  "getter-return",
  "no-async-promise-executor",
  "no-case-declarations",
  "no-class-assign",
  "no-compare-neg-zero",
  "no-cond-assign",
  "no-const-assign",
  "no-constant-binary-expression",
  "no-constant-condition",
  "no-control-regex",
  "no-debugger",
  "no-dupe-args",
  "no-dupe-class-members",
  "no-dupe-else-if",
  "no-dupe-keys",
  "no-duplicate-case",
  "no-empty-character-class",
  "no-empty-pattern",
  "no-ex-assign",
  "no-extra-boolean-cast",
  "no-fallthrough",
  "no-func-assign",
  "no-global-assign",
  "no-import-assign",
  "no-irregular-whitespace",
  "no-loss-of-precision",
  "no-misleading-character-class",
  "no-new-native-nonconstructor",
  "no-nonoctal-decimal-escape",
  "no-obj-calls",
  "no-prototype-builtins",
  "no-redeclare",
  "no-regex-spaces",
  "no-self-assign",
  "no-setter-return",
  "no-shadow-restricted-names",
  "no-sparse-arrays",
  "no-this-before-super",
  "no-undef",
  "no-unreachable",
  "no-unsafe-finally",
  "no-unsafe-negation",
  "no-unsafe-optional-chaining",
  "no-unused-labels",
  "no-unused-private-class-members",
  "no-unused-vars",
  "no-useless-backreference",
  "no-useless-catch",
  "no-useless-escape",
  "no-var",
  "no-with",
  "prefer-const",
  "prefer-rest-params",
  "prefer-spread",
  "require-yield",
  "use-isnan",
  "valid-typeof",
];

// Feature-Sliced Design (src/): app → pages → widgets → features → entities →
// shared. A layer imports only from the layers below it, never from another
// slice of its own layer, and only through a slice's public API (index.ts).
// Relative imports stay inside a slice (or a shared segment); everything
// else goes through the `@/` alias. app/ is split by segment too (providers,
// router, styles; main.tsx is its own entry), so its segments meet through
// their index.ts as well. The test helpers (shared/test) are for tests only,
// and the whole-app harness (app/test) only for the app's own tests.
const sliced = ["pages", "widgets", "features", "entities"];
const below = {
  app: ["pages", "widgets", "features", "entities", "shared"],
  pages: ["widgets", "features", "entities", "shared"],
  widgets: ["features", "entities", "shared"],
  features: ["entities", "shared"],
  entities: ["shared"],
  shared: [],
};
const publicApi = "index.{ts,tsx}";
const viaAlias = { source: "@/**" };
const testHelpers = { type: "shared", captured: { slice: "test" }, fileInternalPath: publicApi };

const fsdElements = [
  // Before "app", so the harness is not an ordinary app segment.
  { type: "app-test", pattern: "src/app/test" },
  { type: "app", pattern: "src/app/*", capture: ["slice"] },
  // What is left directly in src/app: main.tsx.
  { type: "app", pattern: "src/app" },
  ...sliced.map((layer) => ({ type: layer, pattern: `src/${layer}/*`, capture: ["slice"] })),
  { type: "shared", pattern: "src/shared/*", capture: ["slice"] },
];

const fsdPolicies = [
  // Anything inside its own slice or segment.
  ...["app", "app-test", ...sliced, "shared"].map((layer) => ({
    from: { element: { type: layer } },
    allow: { to: { element: { type: layer, captured: { slice: "{{ from.element.captured.slice }}" } } } },
  })),
  // Other app segments, through their index (styles/index.css included).
  {
    from: { element: { types: { anyOf: ["app", "app-test"] } } },
    allow: { to: { element: { type: "app", fileInternalPath: "index.{ts,tsx,css}" } } },
  },
  // Lower layers (and other shared segments), through their public API and the alias.
  ...Object.entries(below).map(([layer, targets]) => ({
    from: { element: { types: { anyOf: layer === "app" ? ["app", "app-test"] : [layer] } } },
    allow: {
      to: {
        element: {
          types: { anyOf: layer === "shared" ? ["shared"] : targets },
          captured: { slice: "!test" },
          fileInternalPath: publicApi,
        },
      },
      dependency: viaAlias,
    },
  })),
  // Tests (and the app harness) use the test helpers.
  {
    from: { file: { categories: "test" } },
    allow: { to: { element: testHelpers }, dependency: viaAlias },
  },
  {
    from: { element: { type: "app-test" } },
    allow: { to: { element: testHelpers }, dependency: viaAlias },
  },
  // Only the app's own tests drive the whole app through its harness.
  {
    from: { element: { type: "app" }, file: { categories: "test" } },
    allow: { to: { element: { type: "app-test", fileInternalPath: publicApi } }, dependency: viaAlias },
  },
];

export default defineConfig([
  globalIgnores(["dist", "src/shared/api/schema.gen.ts"]),
  {
    files: ["**/*.{ts,tsx}"],
    extends: [js.configs.recommended, tseslint.configs.strictTypeChecked, reactHooks.configs.flat.recommended],
    plugins: { react },
    languageOptions: {
      globals: globals.browser,
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    settings: { react: { version: "19.3" } },
    rules: {
      "react/no-danger": "error",
      "react/no-danger-with-children": "error",
      "react/jsx-no-script-url": "error",
      "react/jsx-no-target-blank": "error",
      "no-restricted-properties": [
        "error",
        ...htmlSinks,
        { object: "document", property: "write", message: "document.write is banned." },
        { object: "document", property: "writeln", message: "document.writeln is banned." },
      ],
      "no-restricted-syntax": ["error", ...htmlProps],
      "no-eval": "error",
      "no-new-func": "error",
      "@typescript-eslint/no-implied-eval": "error",
      // Numbers in template strings are fine (`${n} участников`).
      // TanStack Router redirects by throwing redirect(...) from beforeLoad.
      "@typescript-eslint/only-throw-error": [
        "error",
        { allow: [{ from: "package", package: "@tanstack/router-core", name: "Redirect" }] },
      ],
      "@typescript-eslint/restrict-template-expressions": ["error", { allowNumber: true }],
    },
  },
  {
    // Components, helpers and callbacks in .tsx are const arrow functions
    // (the owner's call, 2026-10-26). Class members and generators are exempt
    // by nature; .ts files keep their function declarations.
    files: ["**/*.tsx"],
    rules: {
      "react/function-component-definition": [
        "error",
        { namedComponents: "arrow-function", unnamedComponents: "arrow-function" },
      ],
      "func-style": ["error", "expression"],
    },
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { boundaries },
    settings: {
      "import/resolver": { typescript: { project: "tsconfig.json" } },
      "boundaries/elements": fsdElements,
      "boundaries/files": [{ category: "test", pattern: "**/*.test.{ts,tsx}" }],
    },
    rules: {
      "boundaries/dependencies": ["error", { default: "disallow", policies: fsdPolicies }],
      "boundaries/no-unknown-files": "error",
    },
  },
  {
    files: ["**/*.js"],
    ignores: ["public/**"],
    extends: [js.configs.recommended],
    languageOptions: { globals: globals.node },
  },
  {
    // Classic scripts served as-is from public/ (theme-boot.js runs before the app).
    files: ["public/**/*.js"],
    extends: [js.configs.recommended],
    languageOptions: { globals: globals.browser, sourceType: "script" },
  },
  {
    rules: Object.fromEntries(biomeOwns.map((rule) => [rule, "off"])),
  },
]);
