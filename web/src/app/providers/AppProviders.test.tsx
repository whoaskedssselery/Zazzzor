import { render } from "@testing-library/react";
import { act } from "react";
import { createQueryClient } from "@/shared/api";
import { setLanguage } from "@/shared/i18n";
import { AppProviders } from "./AppProviders";

describe("AppProviders in the desktop app", () => {
  const invoke = vi.fn(() => Promise.resolve());

  beforeEach(() => {
    Object.assign(globalThis, { __TAURI_INTERNALS__: { invoke } });
  });

  afterEach(() => {
    delete (globalThis as { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__;
    invoke.mockClear();
  });

  it("tells the app the interface's language, at start and on every switch", () => {
    render(
      <AppProviders queryClient={createQueryClient()}>
        <p>app</p>
      </AppProviders>,
    );
    expect(invoke).toHaveBeenLastCalledWith("set_language", { lang: "ru" });
    act(() => {
      setLanguage("en");
    });
    expect(invoke).toHaveBeenLastCalledWith("set_language", { lang: "en" });
  });
});
