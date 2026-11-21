import { Play } from "lucide-react";
import { type CSSProperties, type ReactNode, useState } from "react";
import type { components } from "@/shared/api";
import { t } from "@/shared/i18n";

export type Embed = components["schemas"]["Embed"];
type Media = components["schemas"]["EmbedMedia"];

/** A card is as wide as Discord's at most. */
const CARD_MAX_W = 432;
/** An image this wide or wider, and not tall, runs under the text; a smaller one sits beside it. */
const LARGE_IMAGE_W = 300;

/** The box of `media` scaled into `maxW`×`maxH`, known before it loads so the feed does not jump. */
const box = (media: Media, maxW: number, maxH: number): CSSProperties => {
  const scale = Math.min(1, maxW / media.width, maxH / media.height);
  return { width: Math.round(media.width * scale), aspectRatio: `${String(media.width)} / ${String(media.height)}` };
};

/** The page's host, for a card that has no site name. */
const hostOf = (url: string): string => {
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return url;
  }
};

/** A link to the page itself: out of the app, without telling it where it came from. */
const External = ({ href, className, children }: { href: string; className?: string; children: ReactNode }) => (
  <a href={href} target="_blank" rel="noopener noreferrer" className={className}>
    {children}
  </a>
);

/** The image of a link straight in the feed. */
const ImageEmbed = ({ embed, image }: { embed: Embed; image: Media }) => (
  <External href={embed.url} className="block w-fit max-w-full overflow-hidden rounded-lg">
    <img
      src={image.url}
      alt={embed.title ?? t("chat:embed.image")}
      loading="lazy"
      className="block h-auto max-w-full bg-control object-contain"
      style={box(image, 520, 350)}
    />
  </External>
);

/**
 * A YouTube video: its cover with a play button; the player (privacy mode)
 * loads only on the click, so nothing is asked of YouTube before.
 */
const VideoPlayer = ({ embed, video }: { embed: Embed; video: Media }) => {
  const [playing, setPlaying] = useState(false);
  const style = { ...box(video, CARD_MAX_W - 32, 400), width: "100%" };
  if (playing) {
    return (
      <iframe
        src={`${video.url}?autoplay=1`}
        title={embed.title ?? t("chat:embed.youtube")}
        allow="autoplay; encrypted-media; picture-in-picture; fullscreen"
        allowFullScreen
        sandbox="allow-scripts allow-same-origin allow-presentation allow-popups allow-popups-to-escape-sandbox"
        referrerPolicy="strict-origin-when-cross-origin"
        className="mt-3 block rounded-md border-0 bg-[#000]"
        style={style}
      />
    );
  }
  return (
    <button
      type="button"
      aria-label={t("chat:embed.watch", { title: embed.title ?? t("chat:embed.video") })}
      className="group/play relative mt-3 block cursor-pointer overflow-hidden rounded-md bg-[#000]"
      style={style}
      onClick={() => {
        setPlaying(true);
      }}
    >
      {embed.image && (
        <img src={embed.image.url} alt="" loading="lazy" className="absolute inset-0 size-full object-cover" />
      )}
      <span className="absolute inset-0 m-auto flex size-14 items-center justify-center rounded-full bg-[rgb(0_0_0/0.65)] text-[#fff] transition-transform duration-(--duration-fast) group-hover/play:scale-110">
        <Play aria-hidden className="ml-1 size-7 fill-current" />
      </span>
    </button>
  );
};

/** A page's card: site, title (the link), description, and its picture or video. */
const LinkCard = ({ embed }: { embed: Embed }) => {
  const image = embed.type === "video" ? null : embed.image;
  const large = image !== null && image.width >= LARGE_IMAGE_W && image.width >= image.height;
  return (
    <article
      aria-label={embed.title ?? hostOf(embed.url)}
      className="w-full rounded-md border-l-4 border-l-accent bg-sidebar p-3 pr-4"
      style={{ maxWidth: CARD_MAX_W }}
    >
      <div className="flex gap-4">
        <div className="min-w-0 flex-1">
          <p className="truncate text-xs text-fg-muted">{embed.site_name ?? hostOf(embed.url)}</p>
          {embed.title && (
            <External
              href={embed.url}
              className="mt-1 line-clamp-2 block text-base font-semibold text-link hover:underline"
            >
              {embed.title}
            </External>
          )}
          {embed.description && (
            <p className="mt-1 line-clamp-3 text-sm whitespace-pre-line text-fg-muted">{embed.description}</p>
          )}
        </div>
        {image && !large && (
          <External href={embed.url} className="shrink-0">
            <img src={image.url} alt="" loading="lazy" className="size-20 rounded-md bg-control object-cover" />
          </External>
        )}
      </div>
      {image && large && (
        <External href={embed.url} className="mt-3 block w-fit max-w-full overflow-hidden rounded-md">
          <img
            src={image.url}
            alt=""
            loading="lazy"
            className="block h-auto max-w-full bg-control"
            style={box(image, CARD_MAX_W - 32, 300)}
          />
        </External>
      )}
      {embed.type === "video" && embed.video && <VideoPlayer embed={embed} video={embed.video} />}
    </article>
  );
};

/** The link previews of a message, under its text, as the server made them. */
export const Embeds = ({ embeds }: { embeds: readonly Embed[] }) => {
  if (embeds.length === 0) return null;
  return (
    <ul aria-label={t("chat:embed.list")} className="mt-1 flex flex-col gap-2">
      {embeds.map((e, i) => (
        // The same URL never comes twice, but its order is the key that survives an edit's refetch.
        <li key={`${String(i)}:${e.url}`}>
          {e.type === "image" && e.image ? <ImageEmbed embed={e} image={e.image} /> : <LinkCard embed={e} />}
        </li>
      ))}
    </ul>
  );
};
