import { act, renderHook } from "@testing-library/react";
import { applyGatewayDispatch, DEFAULT_EVERYONE, getGuildsState, resetGuilds } from "@/entities/guild/@x/channel";
import {
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeReady,
  makeRole,
  makeUser,
} from "@/shared/test";
import { useVisibleChannels } from "./useVisibleChannels";
import { fallbackPath, visibleChannelGroups } from "./visible";

const owner = makeUser("1");
const me = makeUser("100");
const G = "10";
/** VIEW_CHANNEL as an overwrite mask. */
const VIEW = String(1 << 10);

/** Server 10 as `me` (a plain member) sees it, with the given channels. */
const guildWith = (channels: ReturnType<typeof makeChannel>[], extra: { roles?: string[] } = {}) => {
  const member = makeMember(me, { roles: extra.roles ?? [] });
  return makeGuildState(G, me, {
    guild: makeGuild(G, owner.id),
    roles: [makeEveryone(G, String(DEFAULT_EVERYONE)), makeRole("50", { position: 1 })],
    channels,
    members: [member, makeMember(owner)],
    my_member: member,
  });
};

const names = (groups: ReturnType<typeof visibleChannelGroups>) =>
  groups.map((g) => [g.category?.name ?? null, g.channels.map((c) => c.name)]);

afterEach(() => {
  resetGuilds();
});

describe("visibleChannelGroups", () => {
  it("puts channels without a category first, then categories, each sorted by position, then by ID", () => {
    applyGatewayDispatch(
      "READY",
      makeReady(me, [
        guildWith([
          makeChannel("31", G, { name: "Голос", type: 4, position: 1 }),
          makeChannel("30", G, { name: "Текст", type: 4, position: 0 }),
          makeChannel("41", G, { name: "б", parent_id: "30", position: 1 }),
          makeChannel("40", G, { name: "а", parent_id: "30", position: 1 }),
          makeChannel("42", G, { name: "в", parent_id: "30", position: 0 }),
          makeChannel("43", G, { name: "Кинозал", type: 2, parent_id: "31", position: 0 }),
          makeChannel("44", G, { name: "правила", position: 2 }),
          makeChannel("9", G, { name: "общий", position: 2 }),
        ]),
      ]),
    );
    const entry = getGuildsState().guilds[G];
    expect(entry).toBeDefined();
    if (!entry) return;
    expect(names(visibleChannelGroups(entry, me.id))).toEqual([
      [null, ["общий", "правила"]],
      ["Текст", ["в", "а", "б"]],
      ["Голос", ["Кинозал"]],
    ]);
  });

  it("leaves out the channels the user cannot view, and a category with nothing visible", () => {
    const hidden = { id: G, type: 0 as const, allow: "0", deny: VIEW };
    applyGatewayDispatch(
      "READY",
      makeReady(me, [
        guildWith([
          makeChannel("30", G, { name: "Секретно", type: 4, permission_overwrites: [hidden] }),
          makeChannel("40", G, { name: "тайный", parent_id: "30", permission_overwrites: [hidden] }),
          makeChannel("41", G, { name: "открытый" }),
          makeChannel("42", G, { name: "для роли", permission_overwrites: [hidden] }),
        ]),
      ]),
    );
    const entry = getGuildsState().guilds[G];
    if (!entry) throw new Error("no server");
    expect(names(visibleChannelGroups(entry, me.id))).toEqual([[null, ["открытый"]]]);
  });

  it("shows a hidden category that holds a channel the user may view", () => {
    const hidden = { id: G, type: 0 as const, allow: "0", deny: VIEW };
    const shown = { id: me.id, type: 1 as const, allow: VIEW, deny: "0" };
    applyGatewayDispatch(
      "READY",
      makeReady(me, [
        guildWith([
          makeChannel("30", G, { name: "Секретно", type: 4, permission_overwrites: [hidden] }),
          makeChannel("40", G, { name: "мой", parent_id: "30", permission_overwrites: [hidden, shown] }),
        ]),
      ]),
    );
    const entry = getGuildsState().guilds[G];
    if (!entry) throw new Error("no server");
    expect(names(visibleChannelGroups(entry, me.id))).toEqual([["Секретно", ["мой"]]]);
  });
});

describe("fallbackPath", () => {
  const ready = () => {
    applyGatewayDispatch(
      "READY",
      makeReady(me, [
        guildWith([
          makeChannel("30", G, { name: "Категория", type: 4, position: 0 }),
          makeChannel("43", G, { name: "голос", type: 2, position: 0 }),
          makeChannel("41", G, { name: "второй", parent_id: "30", position: 1 }),
          makeChannel("40", G, { name: "первый", parent_id: "30", position: 0 }),
        ]),
      ]),
    );
  };

  it("is the first visible text channel of the server (voice channels and categories are skipped)", () => {
    ready();
    expect(fallbackPath(getGuildsState(), G)).toBe(`/channels/${G}/40`);
  });

  it("prefers the channel asked for, then the last one opened, when the user may view them", () => {
    ready();
    expect(fallbackPath(getGuildsState(), G, { prefer: "41" })).toBe(`/channels/${G}/41`);
    expect(fallbackPath(getGuildsState(), G, { last: "41" })).toBe(`/channels/${G}/41`);
    // A voice channel, a category or a channel that is gone is not a place to land.
    expect(fallbackPath(getGuildsState(), G, { prefer: "43", last: "999" })).toBe(`/channels/${G}/40`);
  });

  it("is home for a server the user is not in or without a visible text channel", () => {
    ready();
    expect(fallbackPath(getGuildsState(), "77")).toBe("/channels/@me");
    expect(fallbackPath(getGuildsState(), null)).toBe("/channels/@me");
    applyGatewayDispatch("READY", makeReady(me, [guildWith([makeChannel("43", G, { type: 2 })])]));
    expect(fallbackPath(getGuildsState(), G)).toBe("/channels/@me");
  });
});

describe("useVisibleChannels", () => {
  it("follows the store: a channel hidden by a GUILD_SYNC leaves the list", () => {
    applyGatewayDispatch(
      "READY",
      makeReady(me, [guildWith([makeChannel("40", G, { name: "а" }), makeChannel("41", G, { name: "б" })])]),
    );
    const { result } = renderHook(() => useVisibleChannels(G));
    expect(names(result.current.categories)).toEqual([[null, ["а", "б"]]]);

    const hidden = { id: G, type: 0 as const, allow: "0", deny: VIEW };
    act(() => {
      applyGatewayDispatch(
        "GUILD_SYNC",
        guildWith([
          makeChannel("40", G, { name: "а" }),
          makeChannel("41", G, { name: "б", permission_overwrites: [hidden] }),
        ]),
      );
    });
    expect(names(result.current.categories)).toEqual([[null, ["а"]]]);
  });

  it("is empty for a server the store does not hold", () => {
    const { result } = renderHook(() => useVisibleChannels("77"));
    expect(result.current.categories).toEqual([]);
  });
});
