import { screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { renderUi } from "../../test/render";
import { ConfirmDialog } from "./ConfirmDialog";

function Harness({ onConfirm }: { onConfirm: () => void }) {
  const [open, setOpen] = useState(true);
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={setOpen}
      title="Удалить канал #мемы?"
      description="Сообщения канала удалятся навсегда."
      confirmLabel="Удалить"
      tone="danger"
      onConfirm={onConfirm}
    />
  );
}

describe("ConfirmDialog", () => {
  it("calls onConfirm only from the confirm button", async () => {
    const onConfirm = vi.fn();
    const { user } = renderUi(<Harness onConfirm={onConfirm} />);
    const dialog = await screen.findByRole("alertdialog", { name: "Удалить канал #мемы?" });
    expect(dialog.textContent).toContain("Сообщения канала удалятся навсегда.");

    await user.click(screen.getByRole("button", { name: "Удалить" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("does not confirm on cancel or Escape", async () => {
    const onConfirm = vi.fn();
    const { user, unmount } = renderUi(<Harness onConfirm={onConfirm} />);

    await user.click(await screen.findByRole("button", { name: "Отмена" }));
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).toBeNull();
    });
    unmount();

    const second = renderUi(<Harness onConfirm={onConfirm} />);
    await screen.findByRole("alertdialog");
    await second.user.keyboard("{Escape}");
    await waitFor(() => {
      expect(screen.queryByRole("alertdialog")).toBeNull();
    });
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("focuses cancel first, so Enter on open does not confirm a destructive action", async () => {
    const onConfirm = vi.fn();
    const { user } = renderUi(<Harness onConfirm={onConfirm} />);
    await screen.findByRole("alertdialog");
    expect(document.activeElement).toBe(screen.getByRole("button", { name: "Отмена" }));
    await user.keyboard("{Enter}");
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
