import { Button } from "@/shared/ui";

export const UNSAVED = "Есть несохранённые изменения";

/** The bar at the bottom of a settings tab while it holds unsaved changes. */
export const SaveBar = ({
  pending,
  onReset,
  onSave,
}: {
  pending: boolean;
  onReset: () => void;
  onSave: () => void;
}) => (
  <section
    aria-label={UNSAVED}
    className="sticky bottom-4 mt-8 flex items-center gap-3 rounded-lg bg-rail px-4 py-2.5 shadow-dialog"
  >
    <p className="min-w-0 flex-1 text-sm font-medium">{UNSAVED}</p>
    <Button variant="link" size="sm" onClick={onReset} disabled={pending}>
      Сбросить
    </Button>
    <Button size="sm" loading={pending} onClick={onSave}>
      Сохранить изменения
    </Button>
  </section>
);
