import { PlaceholderScreen } from "@/widgets/placeholder-screen";

/** /channels/$guildId/$channelId until the chat (Task 6) replaces it. */
export const ChannelPage = ({ channelId }: { channelId: string }) => (
  <PlaceholderScreen title="Канал">Канал {channelId} откроется здесь в следующем обновлении.</PlaceholderScreen>
);
