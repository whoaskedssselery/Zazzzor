import { useSession } from "@/entities/session";
import { PlaceholderScreen } from "@/widgets/placeholder-screen";

/** /channels/@me until the app shell (Task 5) replaces it. */
export const ChannelsHomePage = () => {
  const { user } = useSession();
  return (
    <PlaceholderScreen title="Личные сообщения">
      Вы вошли как {user?.display_name}. Серверы и каналы появятся здесь совсем скоро.
    </PlaceholderScreen>
  );
};
