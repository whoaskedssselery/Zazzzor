import { PlaceholderScreen } from "@/widgets/placeholder-screen";

/** /channels/$guildId/$channelId until the chat (Task 6) replaces it. */
export function ChannelPage({ channelId }: { channelId: string }) {
  return <PlaceholderScreen title="Канал">Канал {channelId} откроется здесь в следующем обновлении.</PlaceholderScreen>;
}
