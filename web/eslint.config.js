import js from "@eslint/js";
import { defineConfig, globalIgnores } from "eslint/config";
import react from "eslint-plugin-react";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

// HTML from strings is how markup injection happens. Messages are rendered as
// React elements from our own markdown AST, so none of these is ever needed.
const htmlSinks = ["innerHTML", "outerHTML", "insertAdjacentHTML", "createContextualFragment"].map((property) => ({
  property,
  message: "Build DOM with React elements; HTML strings are banned in web/.",
}));

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
      "no-eval": "error",
      "no-new-func": "error",
      "@typescript-eslint/no-implied-eval": "error",
      // Numbers in template strings are fine (`${n} участников`).
      "@typescript-eslint/restrict-template-expressions": ["error", { allowNumber: true }],
    },
  },
  {
    files: ["**/*.js"],
    extends: [js.configs.recommended],
    languageOptions: { globals: globals.node },
  },
]);
