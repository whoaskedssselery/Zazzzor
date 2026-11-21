import "i18next";
import type { resources } from "./resources";

declare module "i18next" {
  // Keys are checked against the Russian texts, the source of every other language.
  interface CustomTypeOptions {
    defaultNS: "common";
    resources: (typeof resources)["ru"];
  }
}
