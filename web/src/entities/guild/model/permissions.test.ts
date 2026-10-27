import type { components } from "@/shared/api";
import { makeEveryone } from "@/shared/test";
import {
  ALL,
  canActOn,
  canGrant,
  canManageRole,
  computeBase,
  computeChannel,
  DEFAULT_EVERYONE,
  formatMask,
  has,
  highestPosition,
  PERM,
  parseMask,
} from "./permissions";
import type { GuildEntry } from "./reducer";

type Role = components["schemas"]["Role"];
type Overwrite = components["schemas"]["Overwrite"];

// The cases of server/internal/permissions/*_test.go, ported 1:1: the same
// IDs, the same inputs and the same expected masks. A Go Subject (a member
// with their roles in one server) becomes a GuildEntry holding that member.

const guildID = "1000";
const ownerID = "1";
const userID = "2";
const otherID = "3";
const roleA = "11";
const roleB = "12";
const roleFar = "13";
const channelID = "500";

interface SubjectRole {
  id: string;
  position: number;
  permissions: bigint;
}

interface Subject {
  userId: string;
  ownerId: string;
  guildId: string;
  everyone: bigint;
  roles: SubjectRole[];
}

function subject(everyone: bigint, ...roles: SubjectRole[]): Subject {
  return { userId: userID, ownerId: ownerID, guildId: guildID, everyone, roles };
}

function withUser(s: Subject, userId: string): Subject {
  return { ...s, userId };
}

function role(id: string, position: number, permissions: bigint): SubjectRole {
  return { id, position, permissions };
}

function apiRole(r: SubjectRole): Role {
  return {
    id: r.id,
    name: r.id,
    color: 0,
    position: r.position,
    permissions: r.permissions.toString(),
    hoist: false,
    mentionable: false,
  };
}

function roleOW(id: string, allow: bigint, deny: bigint): Overwrite {
  return { id, type: 0, allow: allow.toString(), deny: deny.toString() };
}

function memberOW(id: string, allow: bigint, deny: bigint): Overwrite {
  return { id, type: 1, allow: allow.toString(), deny: deny.toString() };
}

function user(id: string) {
  return { id, username: `u${id}`, display_name: `U${id}`, avatar_url: null, created_at: "2026-10-21T12:00:00.000Z" };
}

/**
 * One server holding every given subject as a member (the first one is the
 * receiver, `myMember`), the @everyone role of the first subject, every role
 * of every subject and one text channel with the given overwrites.
 */
function entryOf(subjects: Subject[], overwrites: Overwrite[] = []): GuildEntry {
  const [first] = subjects;
  if (!first) throw new Error("entryOf needs a subject");
  const roles: Record<string, Role> = {
    [first.guildId]: apiRole({ id: first.guildId, position: 0, permissions: first.everyone }),
  };
  const members: GuildEntry["members"] = {};
  for (const s of subjects) {
    for (const r of s.roles) roles[r.id] = apiRole(r);
    members[s.userId] = {
      user: user(s.userId),
      nick: null,
      roles: s.roles.map((r) => r.id),
      joined_at: "2026-10-21T12:00:00.000Z",
    };
  }
  const myMember = members[first.userId];
  if (!myMember) throw new Error("unreachable");
  return {
    guild: {
      id: first.guildId,
      name: "Сервер",
      icon_url: null,
      owner_id: first.ownerId,
      created_at: "2026-10-21T12:00:00.000Z",
    },
    roles,
    channels: {
      [channelID]: {
        id: channelID,
        guild_id: first.guildId,
        type: 0,
        name: "general",
        topic: null,
        parent_id: null,
        position: 0,
        permission_synced: false,
        permission_overwrites: overwrites,
      },
    },
    members,
    myMember,
  };
}

const base = (s: Subject) => computeBase(entryOf([s]), s.userId);
const inChannel = (s: Subject, ow: Overwrite[]) => computeChannel(entryOf([s], ow), channelID, s.userId);
const actOn = (actor: Subject, target: Subject) => canActOn(entryOf([actor, target]), actor.userId, target.userId);

describe("Base (TestBase)", () => {
  const view = PERM.VIEW_CHANNEL;
  const send = PERM.SEND_MESSAGES;
  const adm = PERM.ADMINISTRATOR;
  const owner = withUser(subject(0n), ownerID);

  it.each<[string, Subject, bigint]>([
    ["owner without roles gets all", owner, ALL],
    ["no roles gives everyone", subject(view | send), view | send],
    [
      "roles are unioned with everyone",
      subject(view, role(roleA, 1, send), role(roleB, 2, PERM.ATTACH_FILES)),
      view | send | PERM.ATTACH_FILES,
    ],
    ["administrator role gives all", subject(view, role(roleA, 1, adm)), ALL],
    ["administrator on everyone gives all", subject(adm), ALL],
    ["empty subject has nothing", subject(0n), 0n],
    ["unknown bits are preserved", subject(1n << 40n), 1n << 40n],
  ])("%s", (_name, s, want) => {
    expect(base(s)).toBe(want);
  });
});

describe("InChannel (TestInChannel)", () => {
  const view = PERM.VIEW_CHANNEL;
  const send = PERM.SEND_MESSAGES;
  const hist = PERM.READ_MESSAGE_HISTORY;
  const att = PERM.ATTACH_FILES;
  const adm = PERM.ADMINISTRATOR;
  const owner = withUser(subject(0n), ownerID);
  const member = subject(view | send | hist, role(roleA, 1, 0n), role(roleB, 2, 0n));

  it.each<[string, Subject, Overwrite[], bigint]>([
    ["nil overwrites give base", member, [], view | send | hist],
    ["empty overwrites give base", member, [], view | send | hist],
    ["owner ignores deny", owner, [roleOW(guildID, 0n, ALL)], ALL],
    [
      "administrator role ignores deny",
      subject(view, role(roleA, 1, adm)),
      [roleOW(guildID, 0n, view), memberOW(userID, 0n, view)],
      ALL,
    ],
    ["everyone deny removes send", member, [roleOW(guildID, 0n, send)], view | hist],
    ["everyone allow adds attach", member, [roleOW(guildID, att, 0n)], view | send | hist | att],
    ["everyone: deny applied before allow", member, [roleOW(guildID, send, send)], view | send | hist],
    [
      "deny on everyone, allow on role wins",
      member,
      [roleOW(guildID, 0n, send), roleOW(roleA, send, 0n)],
      view | send | hist,
    ],
    [
      "deny on one role, allow on another role of member: allow wins",
      member,
      [roleOW(roleA, 0n, send), roleOW(roleB, send, 0n)],
      view | send | hist,
    ],
    ["role deny alone removes", member, [roleOW(roleA, 0n, send)], view | hist],
    [
      "allow on role, deny on member: denied",
      member,
      [roleOW(roleA, att, 0n), memberOW(userID, 0n, att)],
      view | send | hist,
    ],
    ["member allow beats role deny", member, [roleOW(roleA, 0n, send), memberOW(userID, send, 0n)], view | send | hist],
    ["member: deny applied before allow", member, [memberOW(userID, send, send)], view | send | hist],
    ["deny view removes everything", member, [roleOW(guildID, 0n, view)], 0n],
    ["member deny view removes everything", member, [memberOW(userID, att, view)], 0n],
    ["allow cannot resurrect when view is missing", subject(send), [roleOW(guildID, att, 0n)], 0n],
    ["foreign role overwrite is ignored", member, [roleOW(roleFar, 0n, view)], view | send | hist],
    ["foreign member overwrite is ignored", member, [memberOW(otherID, 0n, view)], view | send | hist],
    ["role-typed overwrite with the user id is ignored", member, [roleOW(userID, 0n, view)], view | send | hist],
    ["member-typed overwrite with a role id is ignored", member, [memberOW(roleA, 0n, view)], view | send | hist],
    ["member-typed overwrite with the guild id is ignored", member, [memberOW(guildID, 0n, view)], view | send | hist],
    ["two roles deny unioned", member, [roleOW(roleA, 0n, send), roleOW(roleB, 0n, hist)], view],
    [
      "order of overwrites does not matter",
      member,
      [memberOW(userID, att, 0n), roleOW(roleB, 0n, send), roleOW(guildID, 0n, hist)],
      view | att,
    ],
    ["view granted by overwrite on role", subject(send, role(roleA, 1, 0n)), [roleOW(roleA, view, 0n)], send | view],
    ["view granted by member overwrite", subject(0n), [memberOW(userID, view | send, 0n)], view | send],
    [
      "unknown bits survive",
      subject(view | (1n << 40n)),
      [roleOW(guildID, 1n << 41n, 0n)],
      view | (1n << 40n) | (1n << 41n),
    ],
  ])("%s", (_name, s, ow, want) => {
    expect(inChannel(s, ow)).toBe(want);
  });

  it("gives nothing in a channel the server does not have", () => {
    expect(computeChannel(entryOf([member]), "999", userID)).toBe(0n);
  });
});

describe("InChannel with duplicate overwrites (TestInChannelDuplicateOverwritesFirstWins)", () => {
  const view = PERM.VIEW_CHANNEL;
  const send = PERM.SEND_MESSAGES;
  const s = subject(view | send, role(roleA, 1, 0n));

  it.each<[string, Overwrite[]]>([
    ["everyone", [roleOW(guildID, 0n, send), roleOW(guildID, 0n, view)]],
    ["role", [roleOW(roleA, 0n, send), roleOW(roleA, 0n, view)]],
    ["member", [memberOW(userID, 0n, send), memberOW(userID, 0n, view)]],
  ])("%s: only the first one counts", (_name, ow) => {
    const first = inChannel(s, ow.slice(0, 1));
    expect(first).toBe(view);
    expect(inChannel(s, ow)).toBe(first);
  });
});

describe("hierarchy (TestHierarchy)", () => {
  const owner = withUser(subject(0n), ownerID);
  const admin = subject(0n, role(roleA, 5, PERM.ADMINISTRATOR));
  const manager = subject(0n, role(roleA, 5, PERM.MANAGE_ROLES));
  const plain = subject(PERM.VIEW_CHANNEL, role(roleB, 7, 0n));
  const lowPlain = subject(0n);

  it("HighestPosition", () => {
    expect(highestPosition(entryOf([owner]), ownerID)).toBeGreaterThan(2 ** 30);
    expect(highestPosition(entryOf([lowPlain]), userID)).toBe(0);
    const s = subject(0n, role(roleA, 3, 0n), role(roleB, 9, 0n));
    expect(highestPosition(entryOf([s]), userID)).toBe(9);
  });

  it.each<[string, Subject, SubjectRole, boolean]>([
    ["owner manages any", owner, role(roleFar, 1000, 0n), true],
    ["manager manages lower role", manager, role(roleFar, 4, 0n), true],
    ["manager cannot manage role at own position", manager, role(roleFar, 5, 0n), false],
    ["manager cannot manage higher role", manager, role(roleFar, 6, 0n), false],
    ["admin manages lower role", admin, role(roleFar, 4, 0n), true],
    ["admin cannot manage own-position role", admin, role(roleFar, 5, 0n), false],
    ["no MANAGE_ROLES cannot manage", plain, role(roleFar, 1, 0n), false],
    ["everyone role (position 0) is editable by manager", manager, role(guildID, 0, 0n), true],
  ])("CanManageRole: %s", (_name, actor, target, want) => {
    expect(canManageRole(entryOf([actor]), actor.userId, apiRole(target))).toBe(want);
  });

  it.each<[string, Subject, bigint, boolean]>([
    ["owner grants Administrator", owner, PERM.ADMINISTRATOR, true],
    ["owner cannot grant unknown bits", owner, 1n << 50n, false],
    ["admin cannot grant unknown bits", admin, ALL | (1n << 50n), false],
    ["admin grants anything", admin, ALL, true],
    ["subset is allowed", plain, PERM.VIEW_CHANNEL, true],
    ["empty request is allowed", plain, 0n, true],
    ["denies administrator the actor lacks", manager, PERM.ADMINISTRATOR, false],
    ["denies a mix with one missing bit", plain, PERM.VIEW_CHANNEL | PERM.SEND_MESSAGES, false],
    ["manager cannot grant what only others have", manager, PERM.KICK_MEMBERS, false],
    ["manager can grant MANAGE_ROLES it has", manager, PERM.MANAGE_ROLES, true],
  ])("CanGrant: %s", (_name, actor, req, want) => {
    expect(canGrant(entryOf([actor]), actor.userId, req)).toBe(want);
  });

  describe("CanActOn", () => {
    const ownerTarget = withUser(subject(0n), ownerID);
    const higher = subject(0n, role(roleA, 9, 0n));
    const lower = withUser(subject(0n, role(roleB, 2, 0n)), otherID);
    const roleLess = withUser(subject(0n), String(Number(otherID) + 1));

    it.each<[string, Subject, Subject, boolean]>([
      ["owner acts on anyone", owner, higher, true],
      ["nobody acts on owner", higher, ownerTarget, false],
      ["owner cannot act on owner either", owner, ownerTarget, false],
      ["higher acts on lower", higher, lower, true],
      ["lower cannot act on higher", lower, higher, false],
      [
        "equal positions are denied",
        subject(0n, role(roleA, 4, 0n)),
        withUser(subject(0n, role(roleB, 4, 0n)), otherID),
        false,
      ],
      ["role-holder acts on role-less", lower, roleLess, true],
    ])("%s", (_name, actor, target, want) => {
      expect(actOn(actor, target)).toBe(want);
    });
  });
});

describe("invalid subjects fail closed (TestInvalidSubjectFailsClosed)", () => {
  const member = subject(PERM.VIEW_CHANNEL);
  // The Go zero Subject is a user the server does not hold as a member.
  const entry = entryOf([member], [roleOW("0", ALL, 0n)]);
  const stranger = "77";

  it("gives a non-member no rights at all", () => {
    expect(computeBase(entry, stranger)).toBe(0n);
    expect(computeChannel(entry, channelID, stranger)).toBe(0n);
    expect(highestPosition(entry, stranger)).toBe(-1);
    expect(canActOn(entryOf([member, withUser(subject(0n), otherID)]), stranger, otherID)).toBe(false);
    expect(canManageRole(entry, stranger, apiRole(role(roleA, 1, 0n)))).toBe(false);
    expect(canGrant(entry, stranger, PERM.VIEW_CHANNEL)).toBe(false);
  });

  it("never takes a zero user and owner for the owner", () => {
    const noOwner = { ...subject(PERM.VIEW_CHANNEL), userId: "0", ownerId: "0" };
    expect(base(noOwner)).toBe(0n);
  });

  it("gives a member of a server without an owner ID nothing", () => {
    expect(base({ ...subject(PERM.VIEW_CHANNEL), ownerId: "0" })).toBe(0n);
  });

  it("gives a member of a server without an ID nothing", () => {
    expect(base({ ...subject(PERM.VIEW_CHANNEL), guildId: "0" })).toBe(0n);
  });
});

describe("CanActOn on oneself (TestCanActOnCrossGuildAndSelf)", () => {
  // Cross-guild does not exist here: a GuildEntry is one server.
  it("refuses acting on oneself", () => {
    const hi = subject(0n, role(roleA, 9, 0n));
    expect(canActOn(entryOf([hi]), userID, userID)).toBe(false);
  });
});

describe("ADMINISTRATOR in overwrites (TestInChannelMasksAdministratorInOverwrites)", () => {
  const view = PERM.VIEW_CHANNEL;
  const s = subject(view, role(roleA, 1, 0n));

  it.each<[string, Overwrite]>([
    ["everyone", roleOW(guildID, PERM.ADMINISTRATOR, 0n)],
    ["role", roleOW(roleA, PERM.ADMINISTRATOR, 0n)],
    ["member", memberOW(userID, PERM.ADMINISTRATOR, 0n)],
  ])("an allow on %s does not grant it", (_name, ow) => {
    const got = inChannel(s, [ow]);
    expect(has(got, PERM.ADMINISTRATOR)).toBe(false);
    expect(got).toBe(view);
  });

  it("a deny of it on a real administrator does not matter either", () => {
    const adm = subject(0n, role(roleA, 1, PERM.ADMINISTRATOR));
    expect(inChannel(adm, [roleOW(guildID, 0n, PERM.ADMINISTRATOR)])).toBe(ALL);
  });

  it("ADMINISTRATOR bypasses every overwrite of a channel", () => {
    const adm = subject(0n, role(roleA, 1, PERM.ADMINISTRATOR));
    const ow = [roleOW(guildID, 0n, ALL), roleOW(roleA, 0n, ALL), memberOW(userID, 0n, ALL)];
    expect(inChannel(adm, ow)).toBe(ALL);
  });
});

describe("CanGrant of changed bits (TestCanGrantChangedBitsOnly)", () => {
  const manager = subject(0n, role(roleA, 5, PERM.MANAGE_ROLES));
  const entry = entryOf([manager]);

  it("allows a changed bit the actor has, refuses one it lacks, allows no change", () => {
    expect(canGrant(entry, userID, PERM.MANAGE_ROLES)).toBe(true);
    expect(canGrant(entry, userID, PERM.MANAGE_ROLES | PERM.MANAGE_GUILD)).toBe(false);
    expect(canGrant(entry, userID, 0n)).toBe(true);
  });
});

describe("masks as decimal strings (TestPermissionJSON)", () => {
  it("formats a mask", () => {
    expect(formatMask(PERM.MANAGE_ROLES)).toBe("268435456");
    expect(formatMask(0n)).toBe("0");
  });

  it.each<[string, bigint]>([
    ["0", 0n],
    ["268435456", PERM.MANAGE_ROLES],
    ["18446744073709551615", (1n << 64n) - 1n],
    ["1099511627776", 1n << 40n],
  ])("parses %s", (input, want) => {
    expect(parseMask(input)).toBe(want);
  });

  it.each<[unknown]>([
    [268435456],
    [null],
    [true],
    [{}],
    [[]],
    [""],
    ["-1"],
    ["+1"],
    ["01"],
    ["00"],
    [" 1"],
    ["1 "],
    ["0x10"],
    ["1e3"],
    ["1.5"],
    ["abc"],
    [String.fromCodePoint(0x661, 0x662)],
    ["18446744073709551616"],
    ["100000000000000000000"],
    ["1".repeat(21)],
    ["0".repeat(25)],
  ])("refuses %j", (input) => {
    expect(parseMask(input)).toBeNull();
  });

  it("keeps every bit of a mask above 2^53", () => {
    const mask = parseMask("2305843009213693951");
    expect(mask).toBe((1n << 61n) - 1n);
    expect(formatMask(mask ?? 0n)).toBe("2305843009213693951");
    expect(has(mask ?? 0n, 1n << 60n)).toBe(true);
    // An overwrite allow above 2^53 reaches the channel mask intact (only
    // ADMINISTRATOR is masked out of overwrites).
    const got = inChannel(subject(PERM.VIEW_CHANNEL), [
      { id: guildID, type: 0, allow: "2305843009213693951", deny: "0" },
    ]);
    expect(got).toBe(((1n << 61n) - 1n) & ~PERM.ADMINISTRATOR);
  });
});

describe("permission bits (TestPermissionBits)", () => {
  const bits: Record<keyof typeof PERM, bigint> = {
    CREATE_INVITE: 1n << 0n,
    KICK_MEMBERS: 1n << 1n,
    BAN_MEMBERS: 1n << 2n,
    ADMINISTRATOR: 1n << 3n,
    MANAGE_CHANNELS: 1n << 4n,
    MANAGE_GUILD: 1n << 5n,
    STREAM: 1n << 9n,
    VIEW_CHANNEL: 1n << 10n,
    SEND_MESSAGES: 1n << 11n,
    MANAGE_MESSAGES: 1n << 13n,
    ATTACH_FILES: 1n << 15n,
    READ_MESSAGE_HISTORY: 1n << 16n,
    MENTION_EVERYONE: 1n << 17n,
    CONNECT: 1n << 20n,
    SPEAK: 1n << 21n,
    MUTE_MEMBERS: 1n << 22n,
    DEAFEN_MEMBERS: 1n << 23n,
    MOVE_MEMBERS: 1n << 24n,
    USE_VOICE_ACTIVITY: 1n << 25n,
    CHANGE_NICKNAME: 1n << 26n,
    MANAGE_NICKNAMES: 1n << 27n,
    MANAGE_ROLES: 1n << 28n,
  };

  it("has the bits of 1.2a", () => {
    expect(PERM).toEqual(bits);
  });

  it("ALL is the union of the bits", () => {
    expect(ALL).toBe(Object.values(bits).reduce((a, b) => a | b, 0n));
  });

  it("the @everyone test fixture has the mask of a new server", () => {
    expect(makeEveryone("10").permissions).toBe(formatMask(DEFAULT_EVERYONE));
  });

  it("DEFAULT_EVERYONE is the mask of a new server", () => {
    expect(DEFAULT_EVERYONE).toBe(
      PERM.VIEW_CHANNEL |
        PERM.SEND_MESSAGES |
        PERM.READ_MESSAGE_HISTORY |
        PERM.ATTACH_FILES |
        PERM.MENTION_EVERYONE |
        PERM.CREATE_INVITE |
        PERM.CHANGE_NICKNAME |
        PERM.CONNECT |
        PERM.SPEAK |
        PERM.STREAM |
        PERM.USE_VOICE_ACTIVITY,
    );
  });

  it("has() requires every bit", () => {
    expect(has(ALL, PERM.VIEW_CHANNEL | PERM.SPEAK)).toBe(true);
    expect(has(PERM.VIEW_CHANNEL, PERM.VIEW_CHANNEL | PERM.SPEAK)).toBe(false);
    expect(has(0n, 0n)).toBe(true);
  });
});
