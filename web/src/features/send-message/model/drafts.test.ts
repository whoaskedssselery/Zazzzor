import { renderHook } from "@testing-library/react";
import { act } from "react";
import { resetDrafts, setDraft, useDraft, useDrafts } from "./drafts";

afterEach(() => {
  resetDrafts();
});

describe("drafts", () => {
  it("keeps a draft per channel and forgets an empty one", () => {
    setDraft("101", "привет");
    setDraft("102", "пока");
    expect(useDrafts.getState().drafts).toEqual({ "101": "привет", "102": "пока" });
    setDraft("101", "");
    expect(useDrafts.getState().drafts).toEqual({ "102": "пока" });
  });

  it("gives a component the draft of its channel", () => {
    const { result } = renderHook(() => useDraft("101"));
    expect(result.current).toBe("");
    act(() => {
      setDraft("101", "текст");
    });
    expect(result.current).toBe("текст");
  });

  it("never writes a draft to storage", () => {
    const setItem = vi.spyOn(Storage.prototype, "setItem");
    setDraft("101", "секрет");
    expect(setItem).not.toHaveBeenCalled();
    setItem.mockRestore();
  });

  it("resetDrafts forgets every draft", () => {
    setDraft("101", "текст");
    resetDrafts();
    expect(useDrafts.getState().drafts).toEqual({});
  });
});
