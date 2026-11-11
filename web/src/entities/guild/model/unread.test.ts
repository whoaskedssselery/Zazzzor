import {
  makeChannel,
  makeEveryone,
  makeGuild,
  makeGuildState,
  makeMember,
  makeMessage,
  makeReady,
  makeRole,
  makeUser,
} from "@/shared/test";
import { advanceRead, applyEvent, applyReady, type GuildEntry, type GuildsState } from "./reducer";
import { guildMentionCount, isGuildUnread, isUnread, mentionCount } from "./unread";

const me = makeUser("100");
const petya = makeUser("200");
const G = "10";
const GENERAL = "101";
const NEWS = "102";
const SECRET = "103";
const DUTY = "11";

/** One server: #general read to 500, #news never read, #secret without history for me. */
function ready(): GuildsState {
  const noHistory = { id: G, type: 0 as const, allow: "0", deny: String(1 << 16) };
  const state = makeGuildState(G, me, {
    guild: makeGuild(G, petya.id),
    roles: [makeEveryone(G), makeRole(DUTY, { mentionable: true })],
    channels: [
      makeChannel(GENERAL, G, { last_message_id: "500" }),
      makeChannel(NEWS, G, { last_message_id: "400" }),
      makeChannel(SECRET, G, { last_message_id: "300", permission_overwrites: [noHistory] }),
    ],
    members: [makeMember(me, { roles: [DUTY] }), makeMember(petya)],
    my_member: makeMember(me, { roles: [DUTY] }),
    read_states: [{ channel_id: GENERAL, last_read_id: "500", mention_count: 0 }],
  });
  return applyReady(makeReady(me, [state]));
}

const entry = (s: GuildsState): GuildEntry => {
  const e = s.guilds[G];
  if (!e) throw new Error("no server");
  return e;
};

const create = (s: GuildsState, id: string, channelId: string, author = petya, over = {}) =>
  applyEvent(s, "MESSAGE_CREATE", { ...makeMessage(id, channelId, author), guild_id: G, ...over });

describe("unread channels", () => {
  it("counts a channel with messages past the mark, or never read, but not one without history", () => {
    const e = entry(ready());
    expect(isUnread(e, GENERAL)).toBe(false);
    expect(isUnread(e, NEWS)).toBe(true);
    expect(isUnread(e, SECRET)).toBe(false);
    expect(isGuildUnread(e)).toBe(true);
  });

  it("makes a channel unread on someone's message, and keeps it read on the user's own", () => {
    let s = create(ready(), "600", GENERAL);
    expect(isUnread(entry(s), GENERAL)).toBe(true);
    s = create(s, "700", GENERAL, me);
    expect(isUnread(entry(s), GENERAL)).toBe(false);
    expect(entry(s).channels[GENERAL]?.last_message_id).toBe("700");
  });

  it("counts mentions by ID, by role and @everyone, once per message", () => {
    let s = create(ready(), "600", GENERAL, petya, { mention_user_ids: [me.id] });
    s = create(s, "601", GENERAL, petya, { mention_role_ids: [DUTY] });
    s = create(s, "602", GENERAL, petya, { mention_everyone: true });
    s = create(s, "602", GENERAL, petya, { mention_everyone: true });
    s = create(s, "603", GENERAL, petya, { mention_user_ids: [petya.id] });
    expect(mentionCount(entry(s), GENERAL)).toBe(3);
    expect(guildMentionCount(entry(s))).toBe(3);
  });

  it("reads to an ack and never moves the mark back", () => {
    let s = create(ready(), "600", GENERAL, petya, { mention_user_ids: [me.id] });
    s = applyEvent(s, "MESSAGE_ACK", { channel_id: GENERAL, last_read_id: "600", mention_count: 0 });
    expect(isUnread(entry(s), GENERAL)).toBe(false);
    expect(mentionCount(entry(s), GENERAL)).toBe(0);
    const back = advanceRead(s, GENERAL, { lastReadId: "550", mentions: 2 });
    expect(back).toBe(s);
  });

  it("follows the newest message when it is deleted", () => {
    let s = create(ready(), "600", GENERAL);
    s = applyEvent(s, "MESSAGE_DELETE", { id: "600", channel_id: GENERAL, guild_id: G, last_message_id: "500" });
    expect(isUnread(entry(s), GENERAL)).toBe(false);
  });
});
