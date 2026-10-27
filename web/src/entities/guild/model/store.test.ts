import { act, renderHook } from "@testing-library/react";
import { makeGuildState, makeReady, makeUser } from "@/shared/test";
import { EMPTY_GUILDS } from "./reducer";
import {
  applyGatewayDispatch,
  getGuildsState,
  resetGuilds,
  setConnectionStatus,
  useConnectionStatus,
  useGuilds,
} from "./store";

const me = makeUser("100");

afterEach(() => {
  resetGuilds();
});

describe("guild store", () => {
  it("is filled by READY and changed by the events after it", () => {
    const { result } = renderHook(() => useGuilds((s) => s.order));
    expect(result.current).toEqual([]);
    act(() => {
      applyGatewayDispatch("READY", makeReady(me, [makeGuildState("10", me)]));
    });
    expect(result.current).toEqual(["10"]);
    act(() => {
      applyGatewayDispatch("GUILD_CREATE", makeGuildState("20", me));
    });
    expect(result.current).toEqual(["10", "20"]);
    expect(getGuildsState().me).toEqual(me);
  });

  it("a later READY replaces the store wholesale", () => {
    applyGatewayDispatch("READY", makeReady(me, [makeGuildState("10", me)]));
    applyGatewayDispatch("READY", makeReady(me, [makeGuildState("20", me)]));
    expect(getGuildsState().order).toEqual(["20"]);
    expect(getGuildsState().guilds["10"]).toBeUndefined();
  });

  it("drops a malformed READY and keeps what it had", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    applyGatewayDispatch("READY", makeReady(me, [makeGuildState("10", me)]));
    const before = getGuildsState();
    applyGatewayDispatch("READY", { user: me, guilds: "nope", presences: [] });
    expect(getGuildsState()).toBe(before);
    expect(warn).toHaveBeenCalledTimes(1);
    warn.mockRestore();
  });

  it("leaves messages and typing to the gateway's subscribers", () => {
    applyGatewayDispatch("READY", makeReady(me, [makeGuildState("10", me)]));
    const before = getGuildsState();
    applyGatewayDispatch("MESSAGE_CREATE", { id: "1" });
    applyGatewayDispatch("TYPING_START", { channel_id: "101" });
    expect(getGuildsState()).toBe(before);
  });

  it("follows the connection status", () => {
    const { result } = renderHook(() => useConnectionStatus());
    expect(result.current).toBe("connecting");
    act(() => {
      setConnectionStatus("ready");
    });
    expect(result.current).toBe("ready");
    act(() => {
      setConnectionStatus("reconnecting");
    });
    expect(result.current).toBe("reconnecting");
  });

  it("resetGuilds empties the store and starts over at connecting", () => {
    applyGatewayDispatch("READY", makeReady(me, [makeGuildState("10", me)]));
    setConnectionStatus("ready");
    resetGuilds();
    expect(getGuildsState()).toEqual(EMPTY_GUILDS);
    expect(renderHook(() => useConnectionStatus()).result.current).toBe("connecting");
  });
});
