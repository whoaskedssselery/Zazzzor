// Generated from the JSON files beside it: every namespace, in every language.

import enAdmin from "./en/admin.json";
import enAuth from "./en/auth.json";
import enChat from "./en/chat.json";
import enCommon from "./en/common.json";
import enErrors from "./en/errors.json";
import enSettings from "./en/settings.json";
import ruAdmin from "./ru/admin.json";
import ruAuth from "./ru/auth.json";
import ruChat from "./ru/chat.json";
import ruCommon from "./ru/common.json";
import ruErrors from "./ru/errors.json";
import ruSettings from "./ru/settings.json";

export const locales = {
  en: {
    admin: enAdmin,
    auth: enAuth,
    chat: enChat,
    common: enCommon,
    errors: enErrors,
    settings: enSettings,
  },
  ru: {
    admin: ruAdmin,
    auth: ruAuth,
    chat: ruChat,
    common: ruCommon,
    errors: ruErrors,
    settings: ruSettings,
  },
} as const;
