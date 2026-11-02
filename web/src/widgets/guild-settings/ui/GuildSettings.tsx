import { Link, useNavigate } from "@tanstack/react-router";
import { X } from "lucide-react";
import { type ReactNode, useCallback, useEffect, useState } from "react";
import {
  GUILD_SETTINGS_TITLES,
  type GuildEntry,
  type GuildSettingsTab,
  guildSettingsTabs,
  useConnectionStatus,
  useGuilds,
} from "@/entities/guild";
import { DeleteGuild } from "@/features/delete-guild";
import { ConfirmDialog, cn, IconButton, Skeleton } from "@/shared/ui";
import { OverviewTab } from "./OverviewTab";

export const LEAVE_UNSAVED = "Уйти без сохранения?";

/** The content of one section; sections still to come say so. */
const TabContent = ({
  tab,
  entry,
  onDirtyChange,
}: {
  tab: GuildSettingsTab;
  entry: GuildEntry;
  onDirtyChange: (dirty: boolean) => void;
}): ReactNode => {
  switch (tab) {
    case "overview":
      return <OverviewTab key={entry.guild.id} guild={entry.guild} onDirtyChange={onDirtyChange} />;
    default:
      return <p className="text-base text-fg-muted">{`Раздел «${GUILD_SETTINGS_TITLES[tab]}» появится позже.`}</p>;
  }
};

/**
 * A server's settings, over the whole window as in Discord: its sections
 * on the left (only those the user's permissions open), the open one on
 * the right, and a way out (the button or Esc) back to the server. A
 * section with unsaved changes asks before the user leaves it. When the
 * server goes, or the user loses the section, they are taken to what they
 * may still see.
 */
export const GuildSettings = ({ guildId, tab }: { guildId: string; tab: string }) => {
  const entry = useGuilds((s) => s.guilds[guildId]);
  const meId = useGuilds((s) => s.me?.id ?? null);
  const status = useConnectionStatus();
  const navigate = useNavigate();
  const tabs = entry && meId !== null ? guildSettingsTabs(entry, meId) : [];
  const current = tabs.find((t) => t === tab);
  const [dirty, setDirty] = useState(false);
  const [pendingLeave, setPendingLeave] = useState<(() => void) | null>(null);

  const guard = (go: () => void) => {
    if (dirty) setPendingLeave(() => go);
    else go();
  };
  const close = useCallback(() => {
    void navigate({ to: "/channels/$guildId", params: { guildId } });
  }, [navigate, guildId]);

  // Lost the server or every section: back to it, or home if it is gone.
  const known = entry !== undefined;
  const first = tabs[0];
  useEffect(() => {
    if (status !== "ready") return;
    if (!known) void navigate({ to: "/channels/@me", replace: true });
    else if (first === undefined) close();
    else if (current === undefined)
      void navigate({ to: "/guilds/$guildId/settings/$tab", params: { guildId, tab: first }, replace: true });
  }, [status, known, first, current, guildId, navigate, close]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // A dialog over the settings closes itself first.
      if (e.key !== "Escape" || e.defaultPrevented || document.querySelector("[role=dialog]")) return;
      if (dirty) setPendingLeave(() => close);
      else close();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [dirty, close]);

  const isOwner = entry !== undefined && entry.guild.owner_id === meId;
  return (
    <div className="flex h-full min-h-0 bg-chat max-md:flex-col">
      <nav
        aria-label="Настройки сервера"
        className="flex shrink-0 justify-end bg-sidebar md:w-[max(14rem,calc((100%-46rem)/2))] max-md:overflow-x-auto"
      >
        <div className="flex w-full flex-col gap-0.5 px-4 py-6 md:w-56 md:py-14 max-md:flex-row max-md:items-center">
          {entry ? (
            <h2 className="truncate px-2.5 pb-1.5 text-xs font-semibold text-fg-faint uppercase max-md:hidden">
              {entry.guild.name}
            </h2>
          ) : (
            <Skeleton className="mx-2.5 mb-2 h-3 w-24 rounded-sm" />
          )}
          {tabs.map((t) => (
            <Link
              key={t}
              to="/guilds/$guildId/settings/$tab"
              params={{ guildId, tab: t }}
              aria-current={t === current ? "page" : undefined}
              className={cn(
                "shrink-0 rounded-md px-2.5 py-1.5 text-base text-fg-muted transition-colors hover:bg-hover hover:text-fg",
                t === current && "bg-selected text-fg",
              )}
              onClick={(e) => {
                if (!dirty || t === current) return;
                e.preventDefault();
                guard(() => {
                  setDirty(false);
                  void navigate({ to: "/guilds/$guildId/settings/$tab", params: { guildId, tab: t } });
                });
              }}
            >
              {GUILD_SETTINGS_TITLES[t]}
            </Link>
          ))}
          {isOwner && (
            <>
              <span aria-hidden className="mx-2.5 my-2 h-px bg-border max-md:hidden" />
              <DeleteGuild guildId={guildId} guildName={entry.guild.name} />
            </>
          )}
        </div>
      </nav>
      <main className="min-h-0 min-w-0 flex-1 overflow-y-auto">
        <div className="flex max-w-[46rem] gap-6 px-10 py-14 max-md:px-4 max-md:py-6">
          <div className="min-w-0 flex-1">
            {entry && current ? <TabContent tab={current} entry={entry} onDirtyChange={setDirty} /> : null}
          </div>
          <div className="flex shrink-0 flex-col items-center gap-1 max-md:fixed max-md:top-2 max-md:right-2">
            <IconButton
              label="Закрыть настройки"
              icon={X}
              className="size-9 rounded-full border-2 border-border-strong text-fg-muted"
              onClick={() => {
                guard(close);
              }}
            />
            <span aria-hidden className="text-xs font-semibold text-fg-faint max-md:hidden">
              ESC
            </span>
          </div>
        </div>
      </main>
      <ConfirmDialog
        open={pendingLeave !== null}
        onOpenChange={(open) => {
          if (!open) setPendingLeave(null);
        }}
        title={LEAVE_UNSAVED}
        description="Несохранённые изменения пропадут."
        confirmLabel="Уйти"
        tone="danger"
        onConfirm={() => {
          const go = pendingLeave;
          setPendingLeave(null);
          setDirty(false);
          go?.();
        }}
      />
    </div>
  );
};
