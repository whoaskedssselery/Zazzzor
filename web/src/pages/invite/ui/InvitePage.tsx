import { PlaceholderScreen } from "@/widgets/placeholder-screen";

/** /invite/$code until the invite page (Task 5) replaces it. */
export function InvitePage({ code }: { code: string }) {
  return (
    <PlaceholderScreen title="Приглашение">
      Код приглашения: <code>{code}</code>. Принять его можно будет в следующем обновлении.
    </PlaceholderScreen>
  );
}
