import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
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

export default defineConfig([
  globalIgnores(["dist", "src/api/schema.gen.ts"]),
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
