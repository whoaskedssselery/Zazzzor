import { render, screen, waitFor } from "@testing-library/react";
import { renderUi } from "@/shared/test";
import { Button } from "./Button";
import { useToast } from "./Toast";

function ShowButton({ tone }: { tone?: "info" | "error" | "success" }) {
  const toast = useToast();
  return (
    <Button
      onClick={() => {
        toast.show({ text: "Инвайт скопирован", ...(tone ? { tone } : {}) });
      }}
    >
      Показать
    </Button>
  );
}

describe("Toast", () => {
  it("announces the text through role=status", async () => {
    const { user } = renderUi(<ShowButton />);
    await user.click(screen.getByRole("button", { name: "Показать" }));
    // Radix fills the live region a frame after mounting, so it is read out
    // once, and clears it a second later.
    await waitFor(() => {
      const statuses = screen.getAllByRole("status");
      expect(statuses.map((s) => s.textContent)).toContain("Уведомление Инвайт скопирован");
    });
    expect(screen.getByRole("region", { name: "Уведомления (F8)" })).toBeTruthy();
    // The visible toast stays until its timer runs out.
    expect(screen.getByText("Инвайт скопирован", { selector: "[data-toast-text]" })).toBeTruthy();
  });

  it("shows error toasts too, and can be dismissed", async () => {
    const { user } = renderUi(<ShowButton tone="error" />);
    await user.click(screen.getByRole("button", { name: "Показать" }));
    await screen.findAllByRole("status");
    await user.click(screen.getByRole("button", { name: "Скрыть уведомление" }));
    expect(screen.queryByText("Инвайт скопирован", { selector: "[data-toast-text]" })).toBeNull();
  });

  it("throws a clear error outside the provider", () => {
    function Bare() {
      useToast();
      return null;
    }
    // React reports the render error on console.error; keep the output clean.
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
    expect(() => render(<Bare />)).toThrow("useToast must be used inside <ToastProvider>");
    consoleError.mockRestore();
  });
});
