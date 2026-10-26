import { screen, waitFor } from "@testing-library/react";
import { renderUi } from "../../test/render";
import { Button } from "./Button";
import { Dialog, DialogContent, DialogTrigger } from "./Dialog";

describe("Dialog", () => {
  it("closes on Escape and returns focus to the trigger", async () => {
    const { user } = renderUi(
      <Dialog>
        <DialogTrigger asChild>
          <Button>Создать канал</Button>
        </DialogTrigger>
        <DialogContent title="Новый канал" description="Канал появится в выбранной категории.">
          <input aria-label="Название канала" />
        </DialogContent>
      </Dialog>,
    );
    const trigger = screen.getByRole("button", { name: "Создать канал" });

    await user.click(trigger);
    const dialog = await screen.findByRole("dialog", { name: "Новый канал" });
    expect(dialog.contains(document.activeElement)).toBe(true);

    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
    expect(document.activeElement).toBe(trigger);
  });

  it("has a labelled close button", async () => {
    const { user } = renderUi(
      <Dialog defaultOpen>
        <DialogContent title="Новый канал">текст</DialogContent>
      </Dialog>,
    );
    await user.click(await screen.findByRole("button", { name: "Закрыть" }));
    await waitFor(() => {
      expect(screen.queryByRole("dialog")).toBeNull();
    });
  });
});
