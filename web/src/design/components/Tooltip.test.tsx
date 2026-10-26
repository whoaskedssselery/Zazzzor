import { screen, waitFor } from "@testing-library/react";
import { renderUi } from "../../test/render";
import { IconButton } from "./IconButton";
import { Tooltip } from "./Tooltip";
import { Settings } from "lucide-react";

describe("Tooltip", () => {
  it("opens on keyboard focus, describes the trigger and closes on Escape", async () => {
    const { user } = renderUi(
      <>
        <button type="button">до</button>
        <Tooltip content="Настройки пользователя">
          <button type="button">Шестерёнка</button>
        </Tooltip>
      </>,
    );

    await user.tab();
    await user.tab();
    const trigger = screen.getByRole("button", { name: "Шестерёнка" });
    expect(document.activeElement).toBe(trigger);

    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip.textContent).toBe("Настройки пользователя");
    expect(trigger.getAttribute("aria-describedby")).toBe(tooltip.id);

    await user.keyboard("{Escape}");
    await waitFor(() => {
      expect(screen.queryByRole("tooltip")).toBeNull();
    });
  });

  it("gives an IconButton its accessible name and a tooltip with the same text", async () => {
    const { user } = renderUi(<IconButton label="Настройки" icon={Settings} />);
    await user.tab();
    const button = screen.getByRole("button", { name: "Настройки" });
    expect(document.activeElement).toBe(button);
    expect((await screen.findByRole("tooltip")).textContent).toBe("Настройки");
  });
});
