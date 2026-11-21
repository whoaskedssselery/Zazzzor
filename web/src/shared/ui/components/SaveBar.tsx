import { t } from "@/shared/i18n";
import { Button } from "./Button";

export const unsavedText = (): string => t("common:unsavedChanges");

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
    aria-label={unsavedText()}
    className="sticky bottom-4 mt-8 flex animate-rise-in items-center gap-3 rounded-lg bg-rail px-4 py-2.5 shadow-dialog"
  >
    <p className="min-w-0 flex-1 truncate text-sm font-medium">{unsavedText()}</p>
    <Button variant="link" size="sm" onClick={onReset} disabled={pending}>
      {t("common:reset")}
    </Button>
    <Button size="sm" loading={pending} onClick={onSave}>
      {t("common:saveChanges")}
    </Button>
  </section>
);
