import { useRouterState } from "@tanstack/react-router";
import { Menu, Users } from "lucide-react";
import { type CSSProperties, createContext, type ReactNode, use, useState } from "react";
import { useMediaQuery } from "@/shared/lib";
import { cn, IconButton, Sheet, SheetContent } from "@/shared/ui";

/** From this width the member list sits beside the chat; below it, it slides over it. */
const WIDE = "(min-width: 1100px)";
/** From this width the server rail and the channel list stay on screen; below it, they slide in. */
const ROOMY = "(min-width: 768px)";

interface ShellControls {
  /** Whether the member list is on screen (beside the chat or over it). */
  membersShown: boolean;
  toggleMembers: () => void;
  /** Opens the servers and channels on a narrow window. */
  openNav: () => void;
}

const ShellContext = createContext<ShellControls | null>(null);

const useShell = (): ShellControls => {
  const ctx = use(ShellContext);
  if (!ctx) throw new Error("useShell must be used inside <AppShell>");
  return ctx;
};

interface AppShellProps {
  /** The server rail. */
  rail: ReactNode;
  /** The channel list (or home's list), above the user panel. */
  sidebar: ReactNode;
  userPanel: ReactNode;
  /** The member list, when the screen has one. */
  members?: ReactNode;
  /** The main area: its own header, then its content. */
  children: ReactNode;
}

/**
 * Where focus starts in the slid-in navigation: the open channel, else the
 * open server (or home) in the rail. The channel first: a rail icon shows
 * its tooltip on focus, which would cover the channel list.
 */
const openPlace = (panel: HTMLElement): HTMLElement | null =>
  panel.querySelector<HTMLElement>('nav[aria-label="Каналы"] [aria-current="page"]') ??
  panel.querySelector<HTMLElement>('[aria-current="page"]');

const navColumns = (rail: ReactNode, sidebar: ReactNode, userPanel: ReactNode) => (
  <>
    <div className="flex w-(--rail-width) shrink-0 flex-col bg-rail">{rail}</div>
    <div
      className="flex w-(--sidebar-width) min-w-0 shrink-0 flex-col bg-sidebar"
      style={{ "--avatar-cutout": "var(--bg-sidebar)" } as CSSProperties}
    >
      <div className="flex min-h-0 flex-1 flex-col">{sidebar}</div>
      {userPanel}
    </div>
  </>
);

/**
 * The main screen in Discord's layout: server rail, channel list with the
 * user panel under it, the main area and the member list. Narrower than
 * 1100px the member list slides over the chat from the right instead of
 * taking a column; narrower than 768px (a phone) the rail and the channel
 * list slide in from the left, opened from the main area's header. The
 * pages put NavButton and MembersButton into their header.
 *
 * Each part is mounted once: the nav columns only when there is room for
 * them (else inside the open sheet), the member column only on a wide window
 * (else inside the open sheet). The app renders the shell in a layout route,
 * so it stays mounted, toggles and scroll positions included, while the
 * server and channel change under it.
 */
export const AppShell = ({ rail, sidebar, userPanel, members, children }: AppShellProps) => {
  const wide = useMediaQuery(WIDE);
  const roomy = useMediaQuery(ROOMY);
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  // Beside the chat the list stays until hidden; over it, it waits to be opened.
  const [membersBeside, setMembersBeside] = useState(true);
  const [membersOver, setMembersOver] = useState(false);
  // The slide-in navigation belongs to the page it was opened on: following a
  // link in it closes it, with no effect to reset it.
  const [navOpenAt, setNavOpenAt] = useState<string | null>(null);
  const navOpen = !roomy && navOpenAt === pathname;

  const hasMembers = members !== undefined;
  const membersShown = hasMembers && (wide ? membersBeside : membersOver);
  const controls: ShellControls = {
    membersShown,
    toggleMembers: () => {
      if (wide) setMembersBeside((v) => !v);
      else setMembersOver((v) => !v);
    },
    openNav: () => {
      setNavOpenAt(pathname);
    },
  };

  return (
    <ShellContext value={controls}>
      <div className="flex h-full min-h-0 overflow-hidden">
        {roomy && navColumns(rail, sidebar, userPanel)}
        <main
          className="flex min-h-0 min-w-0 flex-1 flex-col bg-chat"
          style={{ "--avatar-cutout": "var(--bg-chat)" } as CSSProperties}
        >
          {children}
        </main>
        {hasMembers && wide && membersBeside && (
          <div
            className="flex w-(--members-width) shrink-0 flex-col bg-sidebar"
            style={{ "--avatar-cutout": "var(--bg-sidebar)" } as CSSProperties}
          >
            {members}
          </div>
        )}
      </div>
      {!roomy && (
        <Sheet
          open={navOpen}
          onOpenChange={(open) => {
            setNavOpenAt(open ? pathname : null);
          }}
        >
          <SheetContent side="left" title="Серверы и каналы" initialFocus={openPlace}>
            {navColumns(rail, sidebar, userPanel)}
          </SheetContent>
        </Sheet>
      )}
      {hasMembers && !wide && (
        <Sheet open={membersOver} onOpenChange={setMembersOver}>
          <SheetContent side="right" title="Участники" className="w-(--members-width) flex-col bg-sidebar">
            <div
              className="flex min-h-0 flex-1 flex-col"
              style={{ "--avatar-cutout": "var(--bg-sidebar)" } as CSSProperties}
            >
              {members}
            </div>
          </SheetContent>
        </Sheet>
      )}
    </ShellContext>
  );
};

/** The header of the main area: a row the height of the channel list's header. */
export const ShellHeader = ({ children, className }: { children: ReactNode; className?: string }) => (
  <header
    className={cn(
      "flex h-(--header-height) shrink-0 items-center gap-2 px-4 shadow-[0_1px_0_var(--border)] max-md:pl-2",
      className,
    )}
  >
    {children}
  </header>
);

/** Opens the servers and channels; only on a phone-wide window, where they are hidden. */
export const NavButton = () => {
  const { openNav } = useShell();
  return (
    <IconButton label="Серверы и каналы" icon={Menu} onClick={openNav} tooltipSide="bottom" className="md:hidden" />
  );
};

/** Shows or hides the member list; pressed while it is on screen. */
export const MembersButton = () => {
  const { membersShown, toggleMembers } = useShell();
  return (
    <IconButton
      label={membersShown ? "Скрыть участников" : "Показать участников"}
      icon={Users}
      pressed={membersShown}
      onClick={toggleMembers}
      tooltipSide="bottom"
    />
  );
};
