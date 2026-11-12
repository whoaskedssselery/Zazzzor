import { makeChannel, makeDMChannel, makeGuildState, makeMessage, makeReady, makeUser } from "@/shared/test";
import { homeUnreadCount, isPrivateUnread, privateList, privateName, privateUnreadCount } from "./private";
import { advanceRead, applyEvent, applyReady, type GuildsState } from "./reducer";
import { isChannelUnread, lastMessageOf, unreadFrom } from "./unread";

const me = makeUser("100", { display_name: "Я" });
const anya = makeUser("200", { display_name: "Аня" });
const petya = makeUser("300", { display_name: "Петя" });

const DM = "900";
const GROUP = "901";

/** READY with a DM with Аня (read through 5, newest 7) and a group with Аня and Петя, never read. */
const ready = (): GuildsState =>
  applyReady({
    ...makeReady(me, [makeGuildState("10", me, { channels: [makeChannel("101", "10")] })]),
    private_channels: [
      makeDMChannel(DM, [anya], { last_message_id: "7" }),
      makeDMChannel(GROUP, [anya, petya], { owner_id: me.id, last_message_id: "3" }),
    ],
    read_states: [{ channel_id: DM, last_read_id: "5", mention_count: 2 }],
  });

/** A message of someone in a private channel, as MESSAGE_CREATE carries it. */
const privateMessage = (id: string, channelId: string, author = anya) =>
  makeMessage(id, channelId, author, { guild_id: null });

describe("private channels in the store", () => {
  it("READY keeps the DMs and groups with their own read marks", () => {
    const s = ready();
    expect(Object.keys(s.privates).sort()).toEqual([DM, GROUP]);
    expect(s.privates[DM]?.read).toEqual({ lastReadId: "5", mentions: 2 });
    expect(s.privates[GROUP]?.read).toBeNull();
  });

  it("DM_CHANNEL_CREATE and _UPDATE upsert and keep the read mark; _DELETE drops", () => {
    let s = applyEvent(ready(), "DM_CHANNEL_UPDATE", makeDMChannel(DM, [anya], { last_message_id: "7", name: null }));
    expect(s.privates[DM]?.read?.lastReadId).toBe("5");
    s = applyEvent(s, "DM_CHANNEL_CREATE", makeDMChannel("902", [petya]));
    expect(s.privates["902"]?.channel.recipients).toEqual([petya]);
    s = applyEvent(s, "DM_CHANNEL_DELETE", { id: GROUP });
    expect(s.privates[GROUP]).toBeUndefined();
  });

  it("counts every message from someone else, and the user's own reads the channel", () => {
    let s = applyEvent(ready(), "MESSAGE_CREATE", privateMessage("8", DM));
    s = applyEvent(s, "MESSAGE_CREATE", privateMessage("8", DM));
    expect(s.privates[DM]?.read).toEqual({ lastReadId: "5", mentions: 3 });
    expect(s.privates[DM]?.channel.last_message_id).toBe("8");
    s = applyEvent(s, "MESSAGE_CREATE", privateMessage("9", DM, me));
    expect(s.privates[DM]?.read).toEqual({ lastReadId: "9", mentions: 0 });
  });

  it("follows a deletion of the newest message and an ack", () => {
    let s = applyEvent(ready(), "MESSAGE_DELETE", { id: "7", channel_id: DM, guild_id: null, last_message_id: "6" });
    expect(s.privates[DM]?.channel.last_message_id).toBe("6");
    s = applyEvent(s, "MESSAGE_ACK", { channel_id: DM, last_read_id: "6", mention_count: 0 });
    expect(s.privates[DM]?.read).toEqual({ lastReadId: "6", mentions: 0 });
    // An older ack crossing a newer one changes nothing.
    expect(advanceRead(s, DM, { lastReadId: "5", mentions: 1 })).toBe(s);
  });

  it("USER_UPDATE renames a recipient", () => {
    const s = applyEvent(ready(), "USER_UPDATE", { ...anya, display_name: "Анна" });
    expect(s.privates[DM]?.channel.recipients[0]?.display_name).toBe("Анна");
    expect(s.privates[GROUP]?.channel.recipients.map((u) => u.display_name)).toEqual(["Анна", "Петя"]);
  });
});

describe("private channel helpers", () => {
  it("names a group by its name, else its people; a DM by the other person", () => {
    const s = ready();
    expect(privateName(s.privates[DM]?.channel ?? makeDMChannel("1", []))).toBe("Аня");
    expect(privateName(makeDMChannel(GROUP, [anya, petya]))).toBe("Аня, Петя");
    expect(privateName(makeDMChannel(GROUP, [anya, petya], { name: "Пятница" }))).toBe("Пятница");
    expect(privateName(makeDMChannel(GROUP, [], { type: 3 }))).toBe("Только вы");
  });

  it("lists the most recently active first", () => {
    let s = ready();
    expect(privateList(s).map((p) => p.channel.id)).toEqual([DM, GROUP]);
    s = applyEvent(s, "MESSAGE_CREATE", privateMessage("20", GROUP));
    expect(privateList(s).map((p) => p.channel.id)).toEqual([GROUP, DM]);
  });

  it("tells unread and its count, for one channel and for home", () => {
    const s = ready();
    const dm = s.privates[DM];
    if (!dm) throw new Error("DM expected");
    expect(isPrivateUnread(dm)).toBe(true);
    expect(privateUnreadCount(dm)).toBe(2);
    expect(homeUnreadCount(s)).toBe(2);
    expect(isChannelUnread(s, DM)).toBe(true);
    expect(lastMessageOf(s, DM)).toBe("7");
    expect(unreadFrom(s, DM)).toBe("5");
    expect(unreadFrom(s, GROUP)).toBe("0");
  });
});
