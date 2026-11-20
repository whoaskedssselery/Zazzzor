import { act, renderHook } from "@testing-library/react";
import { setConnectionStatus } from "@/entities/guild";
import { SPLASH_TIMEOUT_MS, useSplash } from "./splash";

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  setConnectionStatus("connecting");
});

describe("useSplash", () => {
  it("shows while connecting, fades out once ready, then goes", () => {
    const { result } = renderHook(() => useSplash());
    expect(result.current).toBe("showing");
    act(() => {
      setConnectionStatus("ready");
    });
    expect(result.current).toBe("leaving");
    act(() => {
      vi.advanceTimersByTime(260);
    });
    expect(result.current).toBe("gone");
    // A lost connection later is the banner's job, not the loading screen's.
    act(() => {
      setConnectionStatus("reconnecting");
    });
    expect(result.current).toBe("gone");
  });

  it("gives way to the connection banner when the gateway takes too long", () => {
    const { result } = renderHook(() => useSplash());
    act(() => {
      vi.advanceTimersByTime(SPLASH_TIMEOUT_MS - 1);
    });
    expect(result.current).toBe("showing");
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current).toBe("leaving");
  });
});
