import { useMutation } from "@tanstack/react-query";
import { Trash2, Upload } from "lucide-react";
import { type ChangeEvent, useRef, useState } from "react";
import { type GuildEmoji, type GuildEntry, userDisplay } from "@/entities/guild";
import { api, errorText, unwrap } from "@/shared/api";
import { onLanguage, t } from "@/shared/i18n";
import { IMAGE_ACCEPT, wrongImageText } from "@/shared/lib";
import { Button, ConfirmDialog, IconButton, Input, useToast } from "@/shared/ui";

/** The server's limits (C11): per kind, and the file size. */
export const MAX_PER_KIND = 50;
export const MAX_EMOJI_BYTES = 256 * 1024;
export let EMOJI_TOO_LARGE = "";
export let EMOJI_NAME_RULE = "";
onLanguage(() => {
  EMOJI_TOO_LARGE = t("guild:emoji.tooLarge");
  EMOJI_NAME_RULE = t("guild:emoji.nameRule");
});

const NAME = /^[A-Za-z0-9_]{2,32}$/;
const TYPES = new Set(IMAGE_ACCEPT.split(","));

/** Why the server would refuse `file` as an emoji, before sending it; null when it would not. */
export const emojiFileProblem = (file: File): string | null => {
  if (!TYPES.has(file.type)) return wrongImageText();
  if (file.size > MAX_EMOJI_BYTES) return EMOJI_TOO_LARGE;
  return null;
};

/**
 * A name for an emoji from its file's name, as Discord makes one: the
 * name without its extension, with every character the server does not
 * take as `_`, cut to 32; "emoji" when too little is left.
 */
export const nameFromFile = (filename: string): string => {
  const base = filename
    .replace(/\.[^.]*$/, "")
    .replace(/[^A-Za-z0-9_]/g, "_")
    .slice(0, 32);
  return /[A-Za-z0-9]/.test(base) && base.length >= 2 ? base : "emoji";
};

/** A name not in `used` (lower-cased names): `name`, else `name_2`, `name_3`… */
export const freeName = (name: string, used: ReadonlySet<string>): string => {
  if (!used.has(name.toLowerCase())) return name;
  for (let i = 2; ; i++) {
    const tail = `_${String(i)}`;
    const next = name.slice(0, 32 - tail.length) + tail;
    if (!used.has(next.toLowerCase())) return next;
  }
};

/** One emoji: its picture, its name to edit in place, who added it, and a way to delete it. */
const Row = ({
  emoji,
  author,
  onRename,
  onDelete,
}: {
  emoji: GuildEmoji;
  author: string | null;
  onRename: (name: string) => void;
  onDelete: () => void;
}) => {
  const [name, setName] = useState(emoji.name);
  const [held, setHeld] = useState(emoji.name);
  // A rename from elsewhere (or a refused one put back) shows at once.
  if (held !== emoji.name) {
    setHeld(emoji.name);
    setName(emoji.name);
  }
  const commit = () => {
    if (name === emoji.name) return;
    if (!NAME.test(name)) {
      setName(emoji.name);
      onRename("");
      return;
    }
    onRename(name);
  };
  return (
    <li className="flex items-center gap-4 py-2">
      <img src={emoji.url} alt={`:${emoji.name}:`} className="size-8 shrink-0 object-contain" />
      <div className="min-w-0 flex-1">
        <Input
          value={name}
          aria-label={t("guild:emoji.nameOf", { name: emoji.name })}
          maxLength={32}
          autoComplete="off"
          spellCheck={false}
          className="h-9 max-w-64"
          onChange={(e) => {
            setName(e.currentTarget.value);
          }}
          onBlur={commit}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commit();
            } else if (e.key === "Escape") setName(emoji.name);
          }}
        />
      </div>
      <span className="w-40 truncate text-sm text-fg-muted max-sm:hidden">{author ?? "—"}</span>
      <IconButton
        label={t("guild:emoji.deleteOf", { name: emoji.name })}
        icon={Trash2}
        variant="danger"
        onClick={onDelete}
      />
    </li>
  );
};

const Group = ({
  title,
  list,
  entry,
  onRename,
  onDelete,
}: {
  title: string;
  list: GuildEmoji[];
  entry: GuildEntry;
  onRename: (emoji: GuildEmoji, name: string) => void;
  onDelete: (emoji: GuildEmoji) => void;
}) => (
  <section aria-label={title} className="mt-6">
    <h3 className="flex items-center justify-between border-b border-border pb-2 text-xs font-semibold text-fg-faint uppercase">
      {title}
      <span className="tabular-nums">{t("guild:emoji.count", { count: list.length, max: MAX_PER_KIND })}</span>
    </h3>
    {list.length === 0 ? (
      <p className="py-3 text-sm text-fg-muted">{t("guild:emoji.noneYet")}</p>
    ) : (
      <ul className="divide-y divide-border">
        {list.map((e) => {
          const member = e.creator_id === null ? undefined : entry.members[e.creator_id];
          return (
            <Row
              key={e.id}
              emoji={e}
              author={member ? userDisplay(entry, member.user).name : null}
              onRename={(name) => {
                onRename(e, name);
              }}
              onDelete={() => {
                onDelete(e);
              }}
            />
          );
        })}
      </ul>
    )}
  </section>
);

/**
 * The server's own emoji (MANAGE_EXPRESSIONS): upload a picture and it
 * becomes `:name:` (named after its file, renamed in place), 50 still and
 * 50 animated. The list is the store's: the server announces every change
 * (GUILD_EMOJIS_UPDATE) and the tab shows it.
 */
export const EmojiTab = ({ entry }: { entry: GuildEntry }) => {
  const guildId = entry.guild.id;
  const toast = useToast();
  const picker = useRef<HTMLInputElement>(null);
  const [deleting, setDeleting] = useState<GuildEmoji | null>(null);
  const all = Object.values(entry.emojis);
  const still = all.filter((e) => !e.animated);
  const animated = all.filter((e) => e.animated);
  const fail = (e: unknown) => {
    toast.show({ text: errorText(e), tone: "error" });
  };

  const upload = useMutation({
    mutationFn: async ({ file, name }: { file: File; name: string }) =>
      unwrap(
        await api.POST("/guilds/{guild_id}/emojis", {
          params: { path: { guild_id: guildId } },
          body: { name, file: "" },
          bodySerializer: () => {
            const form = new FormData();
            form.append("name", name);
            form.append("file", file);
            return form;
          },
        }),
      ),
    onSuccess: (e) => {
      toast.show({ text: t("guild:emoji.added", { name: e.name }), tone: "success" });
    },
    onError: fail,
  });
  const rename = useMutation({
    mutationFn: async ({ emoji, name }: { emoji: GuildEmoji; name: string }) =>
      unwrap(
        await api.PATCH("/guilds/{guild_id}/emojis/{emoji_id}", {
          params: { path: { guild_id: guildId, emoji_id: emoji.id } },
          body: { name },
        }),
      ),
    onError: fail,
  });
  const remove = useMutation({
    mutationFn: async (emoji: GuildEmoji) => {
      const r = await api.DELETE("/guilds/{guild_id}/emojis/{emoji_id}", {
        params: { path: { guild_id: guildId, emoji_id: emoji.id } },
      });
      if (r.response.status !== 404) unwrap(r);
    },
    onSuccess: () => {
      setDeleting(null);
    },
    onError: fail,
  });

  const pick = (e: ChangeEvent<HTMLInputElement>) => {
    const files = [...(e.currentTarget.files ?? [])];
    e.currentTarget.value = "";
    const used = new Set(all.map((x) => x.name.toLowerCase()));
    for (const file of files) {
      const problem = emojiFileProblem(file);
      if (problem !== null) {
        toast.show({ text: `${file.name}: ${problem}`, tone: "error" });
        continue;
      }
      const name = freeName(nameFromFile(file.name), used);
      used.add(name.toLowerCase());
      upload.mutate({ file, name });
    }
  };

  return (
    <section aria-labelledby="emoji-title">
      <h2 id="emoji-title" className="mb-2 text-xl font-semibold">
        {t("guild:emoji.title")}
      </h2>
      <p className="max-w-prose text-base text-fg-muted">
        {t("guild:emoji.introBefore")} <code className="text-fg">{t("guild:emoji.introCode")}</code>{" "}
        {t("guild:emoji.introAfter")}
      </p>
      <input
        ref={picker}
        type="file"
        multiple
        accept={IMAGE_ACCEPT}
        className="sr-only"
        tabIndex={-1}
        aria-hidden
        onChange={pick}
      />
      <Button
        className="mt-4"
        disabled={upload.isPending}
        onClick={() => {
          picker.current?.click();
        }}
      >
        <Upload aria-hidden className="size-4" />
        {t("guild:emoji.upload")}
      </Button>
      <Group
        title={t("guild:emoji.static")}
        list={still}
        entry={entry}
        onRename={(emoji, name) => {
          if (name === "") toast.show({ text: EMOJI_NAME_RULE, tone: "error" });
          else rename.mutate({ emoji, name });
        }}
        onDelete={setDeleting}
      />
      <Group
        title={t("guild:emoji.animated")}
        list={animated}
        entry={entry}
        onRename={(emoji, name) => {
          if (name === "") toast.show({ text: EMOJI_NAME_RULE, tone: "error" });
          else rename.mutate({ emoji, name });
        }}
        onDelete={setDeleting}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        title={t("guild:emoji.deleteTitle", { name: deleting?.name ?? "" })}
        description={t("guild:emoji.deleteText")}
        confirmLabel={t("guild:emoji.delete")}
        tone="danger"
        pending={remove.isPending}
        onConfirm={() => {
          if (deleting) remove.mutate(deleting);
        }}
      />
    </section>
  );
};
