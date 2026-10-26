import { act, render, screen } from "@testing-library/react";
import { setConnectionStatus } from "@/entities/guild";
import { ConnectionBanner } from "./ConnectionBanner";

afterEach(() => {
  setConnectionStatus("connecting");
});

describe("ConnectionBanner", () => {
  it("says «Подключение…» while connecting for the first time", () => {
    render(<ConnectionBanner />);
    expect(screen.getByRole("status").textContent).toBe("Подключение…");
  });

  it("says «Переподключение…» after the connection was lost and nothing once ready", () => {
    render(<ConnectionBanner />);
    act(() => {
      setConnectionStatus("reconnecting");
    });
    expect(screen.getByRole("status").textContent).toBe("Переподключение…");
    act(() => {
      setConnectionStatus("ready");
    });
    // The live region stays in the page (empty), so the next change is announced.
    expect(screen.getByRole("status").textContent).toBe("");
  });
});
