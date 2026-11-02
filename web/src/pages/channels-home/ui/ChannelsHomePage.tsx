import { Compass, Plus } from "lucide-react";
import { useState } from "react";
import { CreateGuildDialog } from "@/features/create-guild";
import { JoinGuildDialog } from "@/features/join-guild";
import { Button } from "@/shared/ui";
import { NavButton, ShellHeader } from "@/widgets/app-shell";

/** Home's column in the shell. Direct messages come in stage 2; until then it says so quietly. */
export const HomeSidebar = () => (
  <>
    <div className="flex h-(--header-height) shrink-0 items-center px-4 text-base font-semibold shadow-[0_1px_0_var(--border)]">
      Главная
    </div>
    <p className="px-4 pt-4 text-sm text-fg-faint">Личные сообщения появятся в одном из следующих обновлений.</p>
  </>
);

/**
 * The main area of /channels/@me (HomeView): no server is open. An empty
 * screen points to the rail and offers to create a server or to join one.
 * The header only holds the menu button on a phone; the sidebar names the place.
 */
export const ChannelsHomePage = () => {
  const [dialog, setDialog] = useState<"create" | "join" | null>(null);
  const close = (open: boolean) => {
    if (!open) setDialog(null);
  };
  return (
    <>
      <ShellHeader className="md:hidden">
        <NavButton />
      </ShellHeader>
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center overflow-y-auto px-6 py-10 text-center">
        <h1 className="max-w-md text-xl font-semibold text-balance">Выберите сервер слева или создайте свой</h1>
        <p className="mt-2 max-w-md text-base text-fg-muted">
          Сервер — это место для вас и друзей: текстовые каналы, роли, а скоро и голос.
        </p>
        <div className="mt-8 flex flex-wrap justify-center gap-3">
          <Button
            size="lg"
            onClick={() => {
              setDialog("create");
            }}
          >
            <Plus aria-hidden className="size-5" />
            Создать сервер
          </Button>
          <Button
            size="lg"
            variant="secondary"
            onClick={() => {
              setDialog("join");
            }}
          >
            <Compass aria-hidden className="size-5" />
            Присоединиться к серверу
          </Button>
        </div>
      </div>
      <CreateGuildDialog open={dialog === "create"} onOpenChange={close} />
      <JoinGuildDialog open={dialog === "join"} onOpenChange={close} />
    </>
  );
};
