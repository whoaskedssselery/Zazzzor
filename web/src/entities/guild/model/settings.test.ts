import { makeEveryone, makeGuildState, makeMember, makeRole, makeUser } from "@/shared/test";
import { formatMask, PERM } from "./permissions";
import { applyReady } from "./reducer";
import { guildSettingsTabs } from "./settings";

const owner = makeUser("1");
const me = makeUser("2");
const G = "10";

/** Server 10 owned by `owner`, where `me` has a role with `perms`. */
const entryWith = (perms: bigint, userId = me.id) => {
  const role = makeRole("50", { permissions: formatMask(perms) });
  const mine = makeMember(me, { roles: perms === 0n ? [] : [role.id] });
  const state = makeGuildState(G, me, {
    guild: { id: G, name: "Сервер", icon_url: null, owner_id: owner.id, created_at: "2026-10-01T00:00:00Z" },
    roles: [makeEveryone(G, "0"), role],
    members: [mine, makeMember(owner)],
    my_member: mine,
  });
  const entry = applyReady({ user: me, guilds: [state], presences: [], read_states: [], relationships: [] }).guilds[G];
  if (!entry) throw new Error("no entry");
  return { entry, userId };
};

describe("guildSettingsTabs", () => {
  it.each([
    ["nothing", 0n, []],
    ["MANAGE_GUILD", PERM.MANAGE_GUILD, ["overview", "invites"]],
    ["MANAGE_ROLES", PERM.MANAGE_ROLES, ["roles", "members"]],
    ["BAN_MEMBERS", PERM.BAN_MEMBERS, ["members", "bans"]],
    ["KICK_MEMBERS", PERM.KICK_MEMBERS, ["members"]],
    ["MANAGE_NICKNAMES", PERM.MANAGE_NICKNAMES, ["members"]],
    ["MANAGE_EXPRESSIONS", PERM.MANAGE_EXPRESSIONS, ["emoji"]],
    ["ADMINISTRATOR", PERM.ADMINISTRATOR, ["overview", "roles", "emoji", "members", "bans", "invites"]],
  ])("opens the sections %s allows", (_, perms, tabs) => {
    const { entry, userId } = entryWith(perms);
    expect(guildSettingsTabs(entry, userId)).toEqual(tabs);
  });

  it("opens everything to the owner", () => {
    const { entry } = entryWith(0n);
    expect(guildSettingsTabs(entry, owner.id)).toHaveLength(6);
  });
});
