import { Link, useNavigate } from "@tanstack/react-router";
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
import { t } from "@/shared/i18n";
import { SettingsFrame, settingsNavItemClass, useLeaveGuard } from "@/shared/ui";
import { AuditTab } from "./AuditTab";
import { BansTab } from "./BansTab";
import { EmojiTab } from "./EmojiTab";
import { InvitesTab } from "./InvitesTab";
import { MembersTab } from "./MembersTab";
import { OverviewTab } from "./OverviewTab";
import { RolesTab } from "./RolesTab";

/** The content of one section. */
const TabContent = ({
  tab,
  entry,
  meId,
  onDirtyChange,
}: {
  tab: GuildSettingsTab;
  entry: GuildEntry;
  meId: string;
  onDirtyChange: (dirty: boolean) => void;
}): ReactNode => {
  switch (tab) {
    case "overview":
      return <OverviewTab key={entry.guild.id} guild={entry.guild} onDirtyChange={onDirtyChange} />;
    case "roles":
      return <RolesTab entry={entry} meId={meId} onDirtyChange={onDirtyChange} />;
    case "emoji":
      return <EmojiTab entry={entry} />;
    case "members":
      return <MembersTab entry={entry} />;
    case "bans":
      return <BansTab guildId={entry.guild.id} />;
    case "invites":
      return <InvitesTab guildId={entry.guild.id} />;
    case "audit":
      return <AuditTab entry={entry} />;
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
  const { guard, dialog } = useLeaveGuard(dirty, () => {
    setDirty(false);
  });

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

  const isOwner = entry !== undefined && entry.guild.owner_id === meId;
  return (
    <>
      <SettingsFrame
        navLabel={t("guild:settingsNav")}
        title={entry?.guild.name ?? null}
        onClose={() => {
          guard(close);
        }}
        nav={
          <>
            {tabs.map((t) => (
              <Link
                key={t}
                to="/guilds/$guildId/settings/$tab"
                params={{ guildId, tab: t }}
                aria-current={t === current ? "page" : undefined}
                className={settingsNavItemClass(t === current)}
                onClick={(e) => {
                  if (!dirty || t === current) return;
                  e.preventDefault();
                  guard(() => {
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
          </>
        }
      >
        {entry && current && meId !== null ? (
          <TabContent tab={current} entry={entry} meId={meId} onDirtyChange={setDirty} />
        ) : null}
      </SettingsFrame>
      {dialog}
    </>
  );
};
