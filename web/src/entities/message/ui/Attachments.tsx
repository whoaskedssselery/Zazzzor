import { ChevronLeft, ChevronRight, Download, ExternalLink, FileText, X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { type CSSProperties, type ReactNode, useState } from "react";
import { cn, IconButton, overlayClass } from "@/shared/ui";
import { type Attachment, attachmentKind, formatSize } from "../model/attachment";

/** The largest box an image takes in the feed; it keeps its shape inside. */
const IMAGE_MAX_W = 520;
const IMAGE_MAX_H = 350;

/**
 * The box of an image in the feed, from its size known before it loads, so
 * the feed does not jump when it arrives. Small images keep their size.
 */
const imageBox = (a: Attachment): CSSProperties => {
  const w = a.width ?? IMAGE_MAX_W;
  const h = a.height ?? IMAGE_MAX_H;
  const scale = Math.min(1, IMAGE_MAX_W / w, IMAGE_MAX_H / h);
  return { width: Math.round(w * scale), aspectRatio: `${String(w)} / ${String(h)}` };
};

const downloadUrl = (a: Attachment) => a.url;

/** A file the browser does not show in place: its name, size and a download. */
const FileCard = ({ attachment: a, children }: { attachment: Attachment; children?: ReactNode }) => (
  <div className="flex w-full max-w-[432px] flex-col gap-2 rounded-lg border border-border bg-sidebar p-3">
    <div className="flex min-w-0 items-center gap-3">
      <FileText aria-hidden className="size-8 shrink-0 text-fg-muted" strokeWidth={1.5} />
      <div className="min-w-0 flex-1">
        <a href={downloadUrl(a)} download={a.filename} className="block truncate text-base text-link hover:underline">
          {a.filename}
        </a>
        <p className="text-xs text-fg-faint">{formatSize(a.size)}</p>
      </div>
      <a
        href={downloadUrl(a)}
        download={a.filename}
        aria-label={`Скачать ${a.filename}`}
        className="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-fg-muted transition-colors hover:bg-hover hover:text-fg"
      >
        <Download aria-hidden className="size-5" />
      </a>
    </div>
    {children}
  </div>
);

/**
 * The images of a message over the whole window, one at a time: arrows (and
 * ←/→) move between them, Esc closes, and the original opens in a new tab.
 */
const ImageViewer = ({
  images,
  index,
  onIndex,
  onClose,
}: {
  images: Attachment[];
  index: number | null;
  onIndex: (i: number) => void;
  onClose: () => void;
}) => {
  const current = index === null ? undefined : images[index];
  const many = images.length > 1;
  const step = (d: number) => {
    if (index !== null) onIndex((index + d + images.length) % images.length);
  };
  return (
    <DialogPrimitive.Root
      open={current !== undefined}
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className={cn(overlayClass, "bg-[rgb(0_0_0/0.85)]")} />
        <DialogPrimitive.Content
          aria-describedby={undefined}
          className="animate-overlay fixed inset-0 z-(--z-dialog) flex flex-col focus:outline-none"
          onKeyDown={(e) => {
            if (!many) return;
            if (e.key === "ArrowLeft") step(-1);
            if (e.key === "ArrowRight") step(1);
          }}
        >
          {current && (
            <>
              <div className="flex shrink-0 items-center gap-2 px-4 py-3 text-on-avatar">
                <DialogPrimitive.Title className="min-w-0 flex-1 truncate text-sm font-medium">
                  {current.filename}
                  <span className="ml-2 text-xs opacity-70">{formatSize(current.size)}</span>
                </DialogPrimitive.Title>
                <a
                  href={current.url}
                  target="_blank"
                  rel="noreferrer"
                  aria-label="Открыть оригинал"
                  className="inline-flex size-9 items-center justify-center rounded-md hover:bg-[rgb(255_255_255/0.1)]"
                >
                  <ExternalLink aria-hidden className="size-5" />
                </a>
                <a
                  href={current.url}
                  download={current.filename}
                  aria-label="Скачать"
                  className="inline-flex size-9 items-center justify-center rounded-md hover:bg-[rgb(255_255_255/0.1)]"
                >
                  <Download aria-hidden className="size-5" />
                </a>
                <DialogPrimitive.Close
                  aria-label="Закрыть"
                  className="inline-flex size-9 cursor-pointer items-center justify-center rounded-md hover:bg-[rgb(255_255_255/0.1)]"
                >
                  <X aria-hidden className="size-5" />
                </DialogPrimitive.Close>
              </div>
              <div className="relative flex min-h-0 flex-1 items-center justify-center px-4 pb-6">
                <img
                  key={current.id}
                  src={current.url}
                  alt={current.filename}
                  className="animate-pop-in max-h-full max-w-full object-contain"
                />
                {many && (
                  <>
                    <IconButton
                      label="Предыдущее изображение"
                      icon={ChevronLeft}
                      className="absolute left-4 size-11 rounded-full bg-[rgb(0_0_0/0.5)] text-on-avatar hover:bg-[rgb(0_0_0/0.7)]"
                      onClick={() => {
                        step(-1);
                      }}
                    />
                    <IconButton
                      label="Следующее изображение"
                      icon={ChevronRight}
                      className="absolute right-4 size-11 rounded-full bg-[rgb(0_0_0/0.5)] text-on-avatar hover:bg-[rgb(0_0_0/0.7)]"
                      onClick={() => {
                        step(1);
                      }}
                    />
                  </>
                )}
              </div>
            </>
          )}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
};

/**
 * The files of a message under its text, as Discord shows them: images in
 * place at their own shape (a click opens them over the window), videos and
 * sounds with players, anything else as a card to download. Which way a file
 * shows follows the type the server sniffed from its bytes.
 */
export const Attachments = ({ attachments }: { attachments: Attachment[] }) => {
  const [open, setOpen] = useState<number | null>(null);
  if (attachments.length === 0) return null;
  const images = attachments.filter((a) => attachmentKind(a.content_type) === "image");
  return (
    <div className="mt-1 flex flex-col items-start gap-1">
      {images.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {images.map((a, i) => (
            <button
              key={a.id}
              type="button"
              aria-label={`Открыть ${a.filename}`}
              className="block max-w-full cursor-zoom-in overflow-hidden rounded-lg bg-skeleton"
              style={imageBox(a)}
              onClick={() => {
                setOpen(i);
              }}
            >
              <img src={a.url} alt={a.filename} loading="lazy" className="size-full object-cover" />
            </button>
          ))}
        </div>
      )}
      {attachments.map((a) => {
        switch (attachmentKind(a.content_type)) {
          case "image":
            // Shown in the row of images above.
            return null;
          case "video":
            return (
              // biome-ignore lint/a11y/useMediaCaption: user uploads come without captions.
              <video
                key={a.id}
                src={a.url}
                controls
                preload="metadata"
                aria-label={a.filename}
                className="max-h-[350px] w-full max-w-[520px] rounded-lg bg-[#000]"
              />
            );
          case "audio":
            return (
              <FileCard key={a.id} attachment={a}>
                {/* biome-ignore lint/a11y/useMediaCaption: user uploads come without captions. */}
                <audio src={a.url} controls preload="metadata" aria-label={a.filename} className="w-full" />
              </FileCard>
            );
          default:
            return <FileCard key={a.id} attachment={a} />;
        }
      })}
      <ImageViewer
        images={images}
        index={open}
        onIndex={setOpen}
        onClose={() => {
          setOpen(null);
        }}
      />
    </div>
  );
};
