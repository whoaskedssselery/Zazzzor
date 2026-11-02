import { GuildSettings } from "@/widgets/guild-settings";

/** /guilds/$guildId/settings/$tab: a server's settings over the whole window. */
export const GuildSettingsPage = ({ guildId, tab }: { guildId: string; tab: string }) => (
  <GuildSettings guildId={guildId} tab={tab} />
);
