import { AppWindow, Monitor } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { nativeShare, type ShareSource, type Thumbnail } from "@/entities/call";
import {
  Button,
  cn,
  Dialog,
  DialogClose,
  DialogContent,
  Spinner,
  Switch,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/shared/ui";
import { setNativeShare, useNativeShare } from "../model/native-share";
import { shareNatively } from "../model/session";

/** Thumbnails taken at once: each holds a capture of its own for a moment. */
const THUMBNAILS_AT_ONCE = 3;

type Thumbs = Record<string, Thumbnail | null>;

/** Takes the thumbnails of `sources` a few at a time, reporting each as it comes (null: none, e.g. minimised). */
const loadThumbnails = (sources: ShareSource[], got: (id: string, thumb: Thumbnail | null) => void) => {
  let cancelled = false;
  // A function, so the check after an await reads the flag again.
  const live = () => !cancelled;
  const queue = [...sources];
  const worker = async () => {
    for (let next = queue.shift(); next !== undefined && live(); next = queue.shift()) {
      const id = next.id;
      const thumb = await nativeShare.thumbnail(id).catch(() => null);
      if (live()) got(id, thumb);
    }
  };
  for (let i = 0; i < THUMBNAILS_AT_ONCE; i++) void worker();
  return () => {
    cancelled = true;
  };
};

const ThumbnailCanvas = ({ thumb }: { thumb: Thumbnail }) => {
  const canvas = useRef<HTMLCanvasElement>(null);
  useEffect(() => {
    canvas.current?.getContext("2d")?.putImageData(new ImageData(thumb.rgba, thumb.width, thumb.height), 0, 0);
  }, [thumb]);
  return <canvas ref={canvas} width={thumb.width} height={thumb.height} className="size-full object-contain" />;
};

const SourceCard = ({
  source,
  thumb,
  selected,
  onSelect,
  onPick,
}: {
  source: ShareSource;
  thumb: Thumbnail | null | undefined;
  selected: boolean;
  onSelect: () => void;
  onPick: () => void;
}) => {
  const Icon = source.kind === "screen" ? Monitor : AppWindow;
  return (
    <button
      type="button"
      aria-pressed={selected}
      onClick={onSelect}
      onDoubleClick={onPick}
      className={cn(
        "flex min-w-0 cursor-pointer flex-col gap-2 rounded-lg p-2 text-left transition-colors duration-(--duration-fast) hover:bg-hover",
        selected && "bg-selected outline-2 outline-accent",
      )}
    >
      <span className="flex aspect-video items-center justify-center overflow-hidden rounded-md bg-[#000]">
        {thumb === undefined && <Spinner className="text-fg-muted" />}
        {thumb === null && <Icon aria-hidden className="size-8 text-fg-muted" strokeWidth={1.5} />}
        {thumb && <ThumbnailCanvas thumb={thumb} />}
      </span>
      <span className="min-w-0">
        <span className="block truncate text-sm font-medium">{source.name}</span>
        <span className="block truncate text-xs text-fg-muted">
          {source.kind === "screen" ? `${String(source.width)}×${String(source.height)}` : (source.app ?? " ")}
        </span>
      </span>
    </button>
  );
};

/**
 * The desktop app's picker: its windows and screens with what each shows,
 * and whether the sound goes with them. A window brings its application's
 * sound, a screen the whole system's but the app's (so the call is not
 * sent back). Opens from «Демонстрация экрана» in the desktop app.
 */
export const NativeSharePicker = () => {
  const open = useNativeShare((s) => s.picking);
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) setNativeShare({ picking: false });
      }}
    >
      {/* Mounted per opening: each starts from a fresh list. */}
      {open && <Picker />}
    </Dialog>
  );
};

const Picker = () => {
  const [sources, setSources] = useState<ShareSource[] | null>(null);
  const [failed, setFailed] = useState(false);
  const [thumbs, setThumbs] = useState<Thumbs>({});
  const [selected, setSelected] = useState<string | null>(null);
  const [sound, setSound] = useState(true);
  const [tab, setTab] = useState<"window" | "screen">("window");
  const soundId = useId();

  useEffect(() => {
    let stop: () => void = () => undefined;
    let gone = false;
    nativeShare
      .sources()
      .then((list) => {
        if (gone) return;
        setSources(list);
        if (!list.some((s) => s.kind === "window")) setTab("screen");
        stop = loadThumbnails(list, (id, thumb) => {
          setThumbs((t) => ({ ...t, [id]: thumb }));
        });
      })
      .catch(() => {
        if (!gone) setFailed(true);
      });
    return () => {
      gone = true;
      stop();
    };
  }, []);

  const chosen = sources?.find((s) => s.id === selected) ?? null;
  const start = (source: ShareSource | null) => {
    if (source) void shareNatively(source, sound);
  };
  const grid = (kind: ShareSource["kind"]) => {
    const list = sources?.filter((s) => s.kind === kind) ?? [];
    if (list.length === 0) {
      return (
        <p className="py-10 text-center text-sm text-fg-muted">
          {kind === "window" ? "Нет открытых окон" : "Нет экранов"}
        </p>
      );
    }
    return (
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
        {list.map((s) => (
          <SourceCard
            key={s.id}
            source={s}
            thumb={thumbs[s.id]}
            selected={s.id === selected}
            onSelect={() => {
              setSelected(s.id);
            }}
            onPick={() => {
              start(s);
            }}
          />
        ))}
      </div>
    );
  };

  return (
    <DialogContent
      title="Демонстрация экрана"
      size="lg"
      footer={
        <>
          <label htmlFor={soundId} className="mr-auto flex cursor-pointer items-center gap-3 text-sm">
            <Switch id={soundId} checked={sound} onCheckedChange={setSound} />
            {chosen?.kind === "screen" ? "Звук системы (без звонка)" : "Звук приложения"}
          </label>
          <DialogClose asChild>
            <Button variant="ghost">Отмена</Button>
          </DialogClose>
          <Button
            disabled={chosen === null}
            onClick={() => {
              start(chosen);
            }}
          >
            Начать демонстрацию
          </Button>
        </>
      }
    >
      {failed && <p className="py-10 text-center text-sm text-fg-danger">Не удалось получить список окон</p>}
      {!failed && sources === null && (
        <div className="flex justify-center py-10">
          <Spinner label="Загрузка" size={24} className="text-fg-muted" />
        </div>
      )}
      {!failed && sources !== null && (
        <Tabs
          value={tab}
          onValueChange={(v) => {
            setTab(v === "screen" ? "screen" : "window");
          }}
        >
          <TabsList className="mb-3">
            <TabsTrigger value="window">Приложения</TabsTrigger>
            <TabsTrigger value="screen">Экраны</TabsTrigger>
          </TabsList>
          <TabsContent value="window">{grid("window")}</TabsContent>
          <TabsContent value="screen">{grid("screen")}</TabsContent>
        </Tabs>
      )}
    </DialogContent>
  );
};
