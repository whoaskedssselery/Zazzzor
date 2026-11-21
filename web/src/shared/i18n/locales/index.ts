// Generated from the JSON files beside it: every namespace, in every language.

import enAdmin from "./en/admin.json";
import enAudit from "./en/audit.json";
import enAuth from "./en/auth.json";
import enChat from "./en/chat.json";
import enCommon from "./en/common.json";
import enErrors from "./en/errors.json";
import enFriends from "./en/friends.json";
import enGuild from "./en/guild.json";
import enMember from "./en/member.json";
import enNotify from "./en/notify.json";
import enSettings from "./en/settings.json";
import enVoice from "./en/voice.json";
import ruAdmin from "./ru/admin.json";
import ruAudit from "./ru/audit.json";
import ruAuth from "./ru/auth.json";
import ruChat from "./ru/chat.json";
import ruCommon from "./ru/common.json";
import ruErrors from "./ru/errors.json";
import ruFriends from "./ru/friends.json";
import ruGuild from "./ru/guild.json";
import ruMember from "./ru/member.json";
import ruNotify from "./ru/notify.json";
import ruSettings from "./ru/settings.json";
import ruVoice from "./ru/voice.json";

export const locales = {
  en: {
    admin: enAdmin,
    audit: enAudit,
    auth: enAuth,
    chat: enChat,
    common: enCommon,
    errors: enErrors,
    friends: enFriends,
    guild: enGuild,
    member: enMember,
    notify: enNotify,
    settings: enSettings,
    voice: enVoice,
  },
  ru: {
    admin: ruAdmin,
    audit: ruAudit,
    auth: ruAuth,
    chat: ruChat,
    common: ruCommon,
    errors: ruErrors,
    friends: ruFriends,
    guild: ruGuild,
    member: ruMember,
    notify: ruNotify,
    settings: ruSettings,
    voice: ruVoice,
  },
} as const;
