import { CircleAlert, CircleCheck, Info, type LucideIcon, X } from "lucide-react";
import { Toast as ToastPrimitive } from "radix-ui";
import { createContext, type ReactNode, use, useCallback, useMemo, useRef, useState } from "react";
import { cn } from "../cn";

export type ToastTone = "info" | "error" | "success";

export interface ToastOptions {
  text: string;
  tone?: ToastTone;
}

interface ToastApi {
  show(opts: ToastOptions): void;
}

interface ToastItem extends Required<ToastOptions> {
  id: number;
  open: boolean;
}

const ToastContext = createContext<ToastApi | null>(null);

const tones: Record<ToastTone, { icon: LucideIcon; className: string }> = {
  info: { icon: Info, className: "text-fg-muted" },
  success: { icon: CircleCheck, className: "text-fg-success" },
  error: { icon: CircleAlert, className: "text-fg-danger" },
};

/** At most this many toasts at once; older ones drop off. */
const MAX_TOASTS = 3;

/**
 * Short notices at the bottom of the window (Radix Toast: role="status",
 * paused on hover and focus, swipe or F8 to reach them). Errors stay twice
 * as long as other tones.
 */
export const ToastProvider = ({ children }: { children: ReactNode }) => {
  const [items, setItems] = useState<ToastItem[]>([]);
  const nextId = useRef(1);

  const show = useCallback((opts: ToastOptions) => {
    const item: ToastItem = { id: nextId.current++, text: opts.text, tone: opts.tone ?? "info", open: true };
    setItems((prev) => [...prev.filter((t) => t.open).slice(-(MAX_TOASTS - 1)), item]);
  }, []);

  const close = useCallback((id: number) => {
    setItems((prev) => prev.map((t) => (t.id === id ? { ...t, open: false } : t)));
  }, []);

  const api = useMemo(() => ({ show }), [show]);

  return (
    <ToastContext value={api}>
      <ToastPrimitive.Provider swipeDirection="down" label="Уведомление">
        {children}
        {items.map((t) => {
          const { icon: Icon, className } = tones[t.tone];
          return (
            <ToastPrimitive.Root
              key={t.id}
              open={t.open}
              duration={t.tone === "error" ? 8000 : 4000}
              type={t.tone === "error" ? "foreground" : "background"}
              onOpenChange={(open) => {
                if (!open) close(t.id);
              }}
              className="flex w-full animate-rise-in items-center gap-3 rounded-lg bg-overlay py-2.5 pr-2 pl-3.5 text-fg shadow-float ring-1 ring-border data-[state=closed]:animate-[zz-fade-out_var(--duration-fast)_ease-in_forwards] data-[swipe=move]:translate-y-(--radix-toast-swipe-move-y)"
            >
              <Icon aria-hidden className={cn("size-5 shrink-0", className)} strokeWidth={2} />
              <ToastPrimitive.Description data-toast-text className="min-w-0 flex-1 text-sm font-medium">
                {t.text}
              </ToastPrimitive.Description>
              <ToastPrimitive.Close
                aria-label="Скрыть уведомление"
                className="inline-flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-md text-fg-muted transition-colors hover:bg-hover hover:text-fg"
              >
                <X aria-hidden className="size-4" />
              </ToastPrimitive.Close>
            </ToastPrimitive.Root>
          );
        })}
        <ToastPrimitive.Viewport
          label="Уведомления ({hotkey})"
          className="fixed bottom-6 left-1/2 z-(--z-toast) flex w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 flex-col gap-2 outline-none"
        />
      </ToastPrimitive.Provider>
    </ToastContext>
  );
};

/** Shows a toast: `useToast().show({ text: "Инвайт скопирован", tone: "success" })`. */
export const useToast = (): ToastApi => {
  const ctx = use(ToastContext);
  if (!ctx) throw new Error("useToast must be used inside <ToastProvider>");
  return ctx;
};
