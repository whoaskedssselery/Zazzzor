import {
  asMe,
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildEmoji,
  makeGuildState,
  makeMember,
  makeReady,
  makeRelationship,
  makeRole,
  makeUser,
} from "@/shared/test";
import { applyEvent, applyReady, EMPTY_GUILDS, type GuildsState } from "./reducer";

const me = makeUser("100");
const petya = makeUser("200");
const masha = makeUser("300");

const G1 = "10";
const G2 = "20";

/** READY with two servers: G1 has me and Petya, G2 has me only. */
function ready(): GuildsState {
  const g1 = makeGuildState(G1, me, {
    roles: [makeEveryone(G1), makeRole("11", { position: 1, permissions: "8" })],
    channels: [makeChannel("101", G1), makeChannel("102", G1, { position: 1 })],
    members: [makeMember(me), makeMember(petya, { roles: ["11"] })],
    presences: [
      { user_id: me.id, status: "online", custom_status: null },
      { user_id: petya.id, status: "online", custom_status: null },
    ],
  });
  const g2 = makeGuildState(G2, me);
  return applyReady(makeReady(me, [g1, g2]));
}

/** Applies the event twice: the second time must change nothing. */
function applyTwice(s: GuildsState, t: string, d: unknown): GuildsState {
  const once = applyEvent(s, t, d);
  const twice = applyEvent(once, t, d);
  expect(twice).toEqual(once);
  return once;
}

describe("applyReady", () => {
  it("fills the store from READY", () => {
    const s = ready();
    expect(s.me).toEqual(asMe(me));
    expect(s.order).toEqual([G1, G2]);
    const g1 = s.guilds[G1];
    expect(g1?.guild.id).toBe(G1);
    expect(Object.keys(g1?.roles ?? {}).sort()).toEqual(["10", "11"]);
    expect(Object.keys(g1?.channels ?? {}).sort()).toEqual(["101", "102"]);
    expect(Object.keys(g1?.members ?? {}).sort()).toEqual([me.id, petya.id]);
    expect(g1?.myMember.user.id).toBe(me.id);
    expect(s.presences).toEqual({ [me.id]: "online", [petya.id]: "online" });
  });

  it("replaces everything held before (a READY after a reconnect)", () => {
    const before = applyEvent(ready(), "GUILD_CREATE", makeGuildState("30", me));
    const after = applyReady(makeReady(me, [makeGuildState(G2, me)]));
    expect(before.order).toContain("30");
    expect(after.order).toEqual([G2]);
    expect(after.guilds[G1]).toBeUndefined();
    expect(after.presences).toEqual({ [me.id]: "online" });
  });

  it("holds myMember among the members even past the first 1000", () => {
    const s = applyReady(makeReady(me, [makeGuildState(G1, me, { members: [makeMember(petya)] })]));
    expect(s.guilds[G1]?.members[me.id]?.user).toEqual(me);
  });
});

describe("applyEvent", () => {
  it("leaves the state as it was for an unknown event", () => {
    const s = ready();
    expect(applyEvent(s, "SOMETHING_NEW", { id: "1" })).toBe(s);
  });

  it("leaves the state as it was for MESSAGE_* and TYPING_START", () => {
    const s = ready();
    expect(applyEvent(s, "MESSAGE_CREATE", { id: "1" })).toBe(s);
    expect(applyEvent(s, "TYPING_START", { channel_id: "101" })).toBe(s);
  });

  it("drops a malformed payload of a known event", () => {
    const s = ready();
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    expect(applyEvent(s, "CHANNEL_DELETE", { id: 101 })).toBe(s);
    expect(applyEvent(s, "GUILD_SYNC", null)).toBe(s);
    expect(warn).toHaveBeenCalledTimes(2);
    warn.mockRestore();
  });

  it("GUILD_SYNC replaces the server whole: a channel missing from it is gone", () => {
    const s = ready();
    const sync = makeGuildState(G1, me, {
      guild: makeGuild(G1, me.id, { name: "Новое имя" }),
      roles: [makeEveryone(G1)],
      channels: [makeChannel("102", G1)],
      members: [makeMember(me), makeMember(petya), makeMember(masha)],
      presences: [
        { user_id: me.id, status: "online", custom_status: null },
        { user_id: masha.id, status: "online", custom_status: null },
      ],
    });
    const next = applyTwice(s, "GUILD_SYNC", sync);
    const g1 = next.guilds[G1];
    expect(g1?.guild.name).toBe("Новое имя");
    expect(Object.keys(g1?.channels ?? {})).toEqual(["102"]);
    expect(Object.keys(g1?.roles ?? {})).toEqual([G1]);
    expect(Object.keys(g1?.members ?? {}).sort()).toEqual([me.id, petya.id, masha.id]);
    expect(g1?.members[petya.id]?.roles).toEqual([]);
    expect(next.order).toEqual([G1, G2]);
    expect(next.presences[masha.id]).toBe("online");
    // The sync lists every online member of the server: Petya is a member and not among them.
    expect(next.presences[petya.id]).toBe("offline");
  });

  it("GUILD_SYNC leaves the presence of someone who is no longer a member to PRESENCE_UPDATE", () => {
    const sync = makeGuildState(G1, me, { members: [makeMember(me)] });
    expect(applyEvent(ready(), "GUILD_SYNC", sync).presences[petya.id]).toBe("online");
  });

  it("GUILD_SYNC of a server not held yet adds it", () => {
    const next = applyTwice(ready(), "GUILD_SYNC", makeGuildState("30", me));
    expect(next.order).toEqual([G1, G2, "30"]);
  });

  it("GUILD_CREATE adds a server at the end", () => {
    const next = applyTwice(ready(), "GUILD_CREATE", makeGuildState("30", me));
    expect(next.order).toEqual([G1, G2, "30"]);
    expect(next.guilds["30"]?.guild.id).toBe("30");
  });

  it("GUILD_CREATE of a server already held is an upsert in place", () => {
    const next = applyTwice(ready(), "GUILD_CREATE", makeGuildState(G1, me, { channels: [makeChannel("109", G1)] }));
    expect(next.order).toEqual([G1, G2]);
    expect(Object.keys(next.guilds[G1]?.channels ?? {})).toEqual(["109"]);
  });

  it("GUILD_UPDATE changes the server's fields", () => {
    const next = applyTwice(ready(), "GUILD_UPDATE", makeGuild(G2, me.id, { name: "Переименован" }));
    expect(next.guilds[G2]?.guild.name).toBe("Переименован");
    expect(next.guilds[G2]?.channels).toEqual(ready().guilds[G2]?.channels);
  });

  it("GUILD_DELETE removes the server", () => {
    const next = applyTwice(ready(), "GUILD_DELETE", { id: G1 });
    expect(next.guilds[G1]).toBeUndefined();
    expect(next.order).toEqual([G2]);
  });

  it("READY and RELATIONSHIP_ADD hold the user's ties by the other user's ID", () => {
    const s0 = applyReady({ ...makeReady(me), relationships: [makeRelationship(petya, 3)] });
    expect(s0.relationships[petya.id]?.type).toBe(3);
    const s1 = applyTwice(s0, "RELATIONSHIP_ADD", makeRelationship(petya, 1));
    expect(s1.relationships[petya.id]?.type).toBe(1);
  });

  it("RELATIONSHIP_REMOVE drops only the tie it names", () => {
    const s0 = applyReady({ ...makeReady(me), relationships: [makeRelationship(petya, 1)] });
    // A late removal of the request that became this friendship changes nothing.
    expect(applyEvent(s0, "RELATIONSHIP_REMOVE", { id: petya.id, type: 3 })).toBe(s0);
    const s1 = applyTwice(s0, "RELATIONSHIP_REMOVE", { id: petya.id, type: 1 });
    expect(s1.relationships).toEqual({});
  });

  it("USER_UPDATE renames a friend outside any shared server", () => {
    const s0 = applyReady({ ...makeReady(me), relationships: [makeRelationship(masha, 1)] });
    const s1 = applyTwice(s0, "USER_UPDATE", { ...masha, display_name: "Мария" });
    expect(s1.relationships[masha.id]?.user.display_name).toBe("Мария");
  });

  it("GUILD_EMOJIS_UPDATE replaces the server's emoji with the list it carries", () => {
    const smile = makeGuildEmoji("501", G1, "smile");
    const cat = makeGuildEmoji("502", G1, "cat", { animated: true });
    let s = applyTwice(ready(), "GUILD_EMOJIS_UPDATE", { guild_id: G1, emojis: [smile, cat] });
    expect(Object.values(s.guilds[G1]?.emojis ?? {})).toEqual([smile, cat]);
    s = applyTwice(s, "GUILD_EMOJIS_UPDATE", { guild_id: G1, emojis: [{ ...cat, name: "kitty" }] });
    expect(s.guilds[G1]?.emojis).toEqual({ "502": { ...cat, name: "kitty" } });
    expect(s.guilds[G2]?.emojis).toEqual({});
  });

  it("CHANNEL_CREATE and CHANNEL_UPDATE upsert, CHANNEL_DELETE removes", () => {
    let s = applyTwice(ready(), "CHANNEL_CREATE", makeChannel("103", G1, { name: "новый" }));
    expect(s.guilds[G1]?.channels["103"]?.name).toBe("новый");
    s = applyTwice(s, "CHANNEL_UPDATE", makeChannel("103", G1, { name: "переименован" }));
    expect(s.guilds[G1]?.channels["103"]?.name).toBe("переименован");
    s = applyTwice(s, "CHANNEL_DELETE", makeChannel("101", G1));
    expect(Object.keys(s.guilds[G1]?.channels ?? {}).sort()).toEqual(["102", "103"]);
  });

  it("ignores a channel of a server it does not hold", () => {
    const s = ready();
    expect(applyEvent(s, "CHANNEL_CREATE", makeChannel("901", "90"))).toEqual(s);
  });

  it("GUILD_ROLE_CREATE and _UPDATE upsert a role", () => {
    let s = applyTwice(ready(), "GUILD_ROLE_CREATE", { guild_id: G1, role: makeRole("12", { name: "модер" }) });
    expect(s.guilds[G1]?.roles["12"]?.name).toBe("модер");
    s = applyTwice(s, "GUILD_ROLE_UPDATE", { guild_id: G1, role: makeRole("12", { name: "модератор", position: 2 }) });
    expect(s.guilds[G1]?.roles["12"]).toMatchObject({ name: "модератор", position: 2 });
  });

  it("GUILD_ROLE_DELETE removes the role and takes it off the members", () => {
    const s = applyTwice(ready(), "GUILD_ROLE_DELETE", { guild_id: G1, role_id: "11" });
    expect(s.guilds[G1]?.roles["11"]).toBeUndefined();
    expect(s.guilds[G1]?.members[petya.id]?.roles).toEqual([]);
  });

  it("GUILD_MEMBER_ADD and _UPDATE upsert a member", () => {
    let s = applyTwice(ready(), "GUILD_MEMBER_ADD", { guild_id: G2, member: makeMember(masha) });
    expect(s.guilds[G2]?.members[masha.id]?.user).toEqual(masha);
    s = applyTwice(s, "GUILD_MEMBER_UPDATE", { guild_id: G2, member: makeMember(masha, { nick: "Маша" }) });
    expect(s.guilds[G2]?.members[masha.id]?.nick).toBe("Маша");
  });

  it("GUILD_MEMBER_UPDATE of me also updates myMember", () => {
    const s = applyTwice(ready(), "GUILD_MEMBER_UPDATE", { guild_id: G1, member: makeMember(me, { roles: ["11"] }) });
    expect(s.guilds[G1]?.myMember.roles).toEqual(["11"]);
    expect(s.guilds[G1]?.members[me.id]?.roles).toEqual(["11"]);
  });

  it("GUILD_MEMBER_REMOVE removes a member", () => {
    const s = applyTwice(ready(), "GUILD_MEMBER_REMOVE", { guild_id: G1, user: petya });
    expect(s.guilds[G1]?.members[petya.id]).toBeUndefined();
  });

  it("GUILD_MEMBER_REMOVE of me drops the server", () => {
    const s = applyTwice(ready(), "GUILD_MEMBER_REMOVE", { guild_id: G1, user: me });
    expect(s.guilds[G1]).toBeUndefined();
    expect(s.order).toEqual([G2]);
  });

  it("PRESENCE_UPDATE sets a user's status", () => {
    let s = applyTwice(ready(), "PRESENCE_UPDATE", { user_id: petya.id, status: "offline", custom_status: null });
    expect(s.presences[petya.id]).toBe("offline");
    s = applyTwice(s, "PRESENCE_UPDATE", { user_id: masha.id, status: "online", custom_status: null });
    expect(s.presences[masha.id]).toBe("online");
  });

  it("PRESENCE_UPDATE keeps the status line of who shows online, and drops it offline", () => {
    let s = applyTwice(ready(), "PRESENCE_UPDATE", { user_id: petya.id, status: "dnd", custom_status: "Сплю" });
    expect(s.presences[petya.id]).toBe("dnd");
    expect(s.statusLines[petya.id]).toBe("Сплю");
    s = applyTwice(s, "PRESENCE_UPDATE", { user_id: petya.id, status: "idle", custom_status: null });
    expect(s.statusLines[petya.id]).toBeUndefined();
    s = applyTwice(s, "PRESENCE_UPDATE", { user_id: petya.id, status: "online", custom_status: "Тут" });
    s = applyTwice(s, "PRESENCE_UPDATE", { user_id: petya.id, status: "offline", custom_status: null });
    expect(s.statusLines[petya.id]).toBeUndefined();
  });

  it("USER_UPDATE of me changes me and me in every server", () => {
    const renamed = { ...me, display_name: "Новое имя" };
    const s = applyTwice(ready(), "USER_UPDATE", asMe(renamed, { bio: "о себе" }));
    expect(s.me).toEqual(asMe(renamed, { bio: "о себе" }));
    expect(s.guilds[G1]?.members[me.id]?.user).toEqual(renamed);
    expect(s.guilds[G2]?.members[me.id]?.user).toEqual(renamed);
    expect(s.guilds[G1]?.myMember.user).toEqual(renamed);
  });

  it("USER_UPDATE of someone else changes them in every server and leaves me", () => {
    const renamed = { ...petya, display_name: "Пётр" };
    const s = applyTwice(ready(), "USER_UPDATE", renamed);
    expect(s.me).toEqual(asMe(me));
    expect(s.guilds[G1]?.members[petya.id]?.user).toEqual(renamed);
    expect(s.guilds[G2]?.members[petya.id]).toBeUndefined();
  });

  it("applies an event to the empty store before READY without failing", () => {
    expect(applyEvent(EMPTY_GUILDS, "GUILD_DELETE", { id: G1 })).toEqual(EMPTY_GUILDS);
  });
});
