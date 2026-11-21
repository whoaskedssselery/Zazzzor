import "i18next";
import type { Texts } from "./resources";

declare module "i18next" {
  // Keys are checked against the English texts, the source of every other language.
  interface CustomTypeOptions {
    defaultNS: "common";
    resources: Texts;
  }
}
