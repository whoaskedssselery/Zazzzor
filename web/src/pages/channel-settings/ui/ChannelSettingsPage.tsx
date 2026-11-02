import { ChannelSettings } from "@/widgets/channel-settings";

/** /guilds/$guildId/channels/$channelId/settings/$tab: a channel's settings over the whole window. */
export const ChannelSettingsPage = ({
  guildId,
  channelId,
  tab,
}: {
  guildId: string;
  channelId: string;
  tab: string;
}) => <ChannelSettings guildId={guildId} channelId={channelId} tab={tab} />;
