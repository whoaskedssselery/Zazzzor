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
]);
