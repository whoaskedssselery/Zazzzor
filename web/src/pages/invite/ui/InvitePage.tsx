import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { useNavigateToFallback } from "@/entities/channel";
import { useGuilds } from "@/entities/guild";
import {
  INVALID_INVITE,
  type Invite,
  inviteErrorText,
  JOINED_LATE,
  previewInvite,
  useJoinInvite,
} from "@/features/join-guild";
import { ApiError } from "@/shared/api";
import { t } from "@/shared/i18n";
import { Avatar, Button, FormAlert, GuildIcon, Skeleton } from "@/shared/ui";

/** The invite cannot lead anywhere any more (unknown, expired or used up). */
const isDead = (e: unknown): boolean => e instanceof ApiError && (e.status === 404 || e.status === 410);

const Card = ({ children, labelledBy }: { children: ReactNode; labelledBy?: string }) => (
  <main className="flex min-h-full flex-col items-center justify-center bg-rail px-4 py-10">
    <p className="mb-6 text-lg font-bold tracking-tight text-fg" translate="no">
      Zazzzor
    </p>
    <section
      aria-labelledby={labelledBy}
      aria-busy={labelledBy ? undefined : true}
      className="flex w-full max-w-[30rem] flex-col items-center rounded-lg bg-chat p-8 text-center shadow-dialog max-sm:px-5 max-sm:py-6"
    >
      {children}
    </section>
  </main>
);

const Loading = () => (
  <Card>
    <Skeleton className="size-20 rounded-xl" />
    <Skeleton className="mt-4 h-4 w-40 rounded-sm" />
    <Skeleton className="mt-3 h-7 w-56 rounded-sm" />
    <Skeleton className="mt-8 h-11 w-full rounded-md" />
    <span className="sr-only" role="status">
      {t("guild:invitePage.loading")}
    </span>
  </Card>
);

const Dead = ({ text, title }: { text: string; title: string }) => (
  <Card labelledBy="invite-title">
    <h1 id="invite-title" className="text-xl font-semibold text-balance">
      {title}
    </h1>
    <p className="mt-2 text-base text-fg-muted">{text}</p>
    <Button asChild size="lg" className="mt-8 w-full">
      <Link to="/channels/@me">{t("guild:invitePage.home")}</Link>
    </Button>
  </Card>
);

const Preview = ({ invite }: { invite: Invite }) => {
  const join = useJoinInvite();
  const openGuild = useNavigateToFallback();
  // A member opening an invite to their own server just goes there (no use is taken).
  const member = useGuilds((s) => invite.guild.id in s.guilds) && !join.pending;
  const message = join.error ? inviteErrorText(join.error) : join.late ? JOINED_LATE : null;
  return (
    <Card labelledBy="invite-title">
      <GuildIcon name={invite.guild.name} src={invite.guild.icon_url} size={80} active />
      <p className="mt-5 flex items-center gap-2 text-sm text-fg-muted">
        <Avatar name={invite.inviter.display_name} src={invite.inviter.avatar_url} size={20} />
        <span>
          <span className="font-semibold text-fg">{invite.inviter.display_name}</span> {t("guild:invitePage.invites")}
        </span>
      </p>
      <h1 id="invite-title" className="mt-2 text-xl font-semibold break-words text-balance">
        {invite.guild.name}
      </h1>
      <p className="mt-1 text-base text-fg-muted">
        <span className="sr-only">{`${t("guild:invitePage.channel")} `}</span>
        <span>{`#${invite.channel.name}`}</span>
      </p>
      {message !== null && <FormAlert className="mt-6 mb-0 w-full text-left">{message}</FormAlert>}
      {member ? (
        <>
          <p className="mt-6 text-base font-medium text-fg">{t("guild:invitePage.already")}</p>
          <Button
            size="lg"
            className="mt-4 w-full"
            onClick={() => {
              openGuild(invite.guild.id, { prefer: invite.channel.id });
            }}
          >
            {t("guild:invitePage.open")}
          </Button>
        </>
      ) : (
        <Button
          size="lg"
          className="mt-8 w-full"
          loading={join.pending}
          onClick={() => {
            if (!join.pending) join.join({ code: invite.code, channelId: invite.channel.id });
          }}
        >
          {t("guild:invitePage.accept")}
        </Button>
      )}
    </Card>
  );
};

/**
 * /invite/$code: where the invite leads (the server, its channel, who
 * invited) and the button to join. Joining opens the invite's channel. A
 * dead invite (unknown, expired, used up) and a ban are said plainly, with
 * the way home.
 */
export const InvitePage = ({ code }: { code: string }) => {
  const preview = useQuery({ queryKey: ["invite", code], queryFn: () => previewInvite(code) });
  if (preview.isPending) return <Loading />;
  if (preview.isError) {
    return isDead(preview.error) ? (
      <Dead title={INVALID_INVITE} text={t("guild:invitePage.deadText")} />
    ) : (
      <Dead title={t("guild:invitePage.failed")} text={inviteErrorText(preview.error)} />
    );
  }
  return <Preview invite={preview.data} />;
};
