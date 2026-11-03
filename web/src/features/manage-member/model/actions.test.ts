import { applyReady, formatMask, PERM } from "@/entities/guild";
import { makeEveryone, makeGuild, makeGuildState, makeMember, makeRole, makeUser } from "@/shared/test";
import { assignableRoles, memberActions } from "./actions";

const owner = makeUser("1");
const me = makeUser("2");
const peer = makeUser("3");
const junior = makeUser("4");
const G = "10";
const low = makeRole("51", { name: "Новичок", position: 1 });
const mid = makeRole("52", { name: "Модератор", position: 2 });

/** Server 10 owned by `owner`: `me` and `peer` hold Модератор with `perms`, `junior` holds Новичок. */
const entry = (perms: bigint) => {
  const role = { ...mid, permissions: formatMask(perms) };
  const mine = makeMember(me, { roles: [mid.id] });
  const state = makeGuildState(G, me, {
    guild: makeGuild(G, owner.id),
    roles: [makeEveryone(G, "0"), low, role],
    members: [mine, makeMember(owner), makeMember(peer, { roles: [mid.id] }), makeMember(junior, { roles: [low.id] })],
    my_member: mine,
  });
  const e = applyReady({ user: me, guilds: [state], presences: [] }).guilds[G];
  if (!e) throw new Error("no entry");
  return e;
};

const ALL_MOD = PERM.CHANGE_NICKNAME | PERM.MANAGE_NICKNAMES | PERM.KICK_MEMBERS | PERM.BAN_MEMBERS | PERM.MANAGE_ROLES;

describe("memberActions", () => {
  it("offers everything on someone ranked below", () => {
    expect(memberActions(entry(ALL_MOD), me.id, junior.id)).toEqual({ nick: true, roles: true, kick: true, ban: true });
  });

  it("offers nothing but roles on an equal rank or the owner", () => {
    for (const target of [peer.id, owner.id]) {
      expect(memberActions(entry(ALL_MOD), me.id, target)).toEqual({
        nick: false,
        roles: true,
        kick: false,
        ban: false,
      });
    }
  });

  it("lets a member rename themself with CHANGE_NICKNAME, never kick or ban themself", () => {
    expect(memberActions(entry(PERM.CHANGE_NICKNAME), me.id, me.id)).toEqual({
      nick: true,
      roles: false,
      kick: false,
      ban: false,
    });
    expect(memberActions(entry(0n), me.id, me.id).nick).toBe(false);
  });

  it("follows each permission on its own", () => {
    expect(memberActions(entry(PERM.KICK_MEMBERS), me.id, junior.id)).toEqual({
      nick: false,
      roles: false,
      kick: true,
      ban: false,
    });
  });

  it("assigns only the roles below the actor's highest", () => {
    expect(assignableRoles(entry(PERM.MANAGE_ROLES), me.id).map((r) => r.name)).toEqual(["Новичок"]);
    expect(assignableRoles(entry(PERM.MANAGE_ROLES), owner.id).map((r) => r.name)).toEqual(["Модератор", "Новичок"]);
  });
});
