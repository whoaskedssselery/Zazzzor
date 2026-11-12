import { applyReady, NOTIFY_LEVEL } from "@/entities/guild";
import {
  makeChannel,
  makeDMChannel,
  makeGuildState,
  makeMember,
  makeMessage,
  makeReady,
  makeUser,
} from "@/shared/test";
import { alertFor, notificationOf, openChannelOf, privateNotificationOf } from "./alerts";

const base = {
  level: NOTIFY_LEVEL.MENTIONS,
  mentioned: true,
  own: false,
  windowActive: true,
  openChannelId: "1",
  channelId: "2",
} as const;

describe("alertFor", () => {
  it("at «only mentions» is silent unless someone else mentions the user", () => {
    expect(alertFor({ ...base, mentioned: false })).toBeNull();
    expect(alertFor({ ...base, own: true })).toBeNull();
  });

  it("counts @everyone unless the user hid it", () => {
    const everyone = { ...base, mentioned: false, everyone: true };
    expect(alertFor(everyone)).not.toBeNull();
    expect(alertFor({ ...everyone, suppressEveryone: true })).toBeNull();
  });

  it("at «all» alerts to any message by someone else, at «nothing» to none", () => {
    expect(alertFor({ ...base, level: NOTIFY_LEVEL.ALL, mentioned: false })).not.toBeNull();
    expect(alertFor({ ...base, level: NOTIFY_LEVEL.ALL, own: true })).toBeNull();
    expect(alertFor({ ...base, level: NOTIFY_LEVEL.NOTHING })).toBeNull();
  });

  it("is silent while the user looks at that channel, chimes in another, notifies in the background", () => {
    expect(alertFor({ ...base, openChannelId: "2" })).toBeNull();
    expect(alertFor(base)).toEqual({ sound: true, desktop: false });
    expect(alertFor({ ...base, windowActive: false, openChannelId: "2" })).toEqual({ sound: true, desktop: true });
  });
});

describe("openChannelOf", () => {
  it("reads the channel from the address", () => {
    expect(openChannelOf("/channels/10/101")).toBe("101");
    expect(openChannelOf("/channels/10/101/settings")).toBe("101");
    expect(openChannelOf("/channels/@me")).toBeNull();
    expect(openChannelOf("/channels/@me/900")).toBe("900");
    expect(openChannelOf("/settings/profile")).toBeNull();
  });
});

describe("notificationOf", () => {
  const me = makeUser("100");
  const anna = makeUser("200", { display_name: "Аня" });
  const entry = applyReady(
    makeReady(me, [
      makeGuildState("10", me, {
        channels: [makeChannel("101", "10", { name: "общий" })],
        members: [makeMember(me), makeMember(anna, { nick: "Анька" })],
      }),
    ]),
  ).guilds["10"];
  if (!entry) throw new Error("no server");

  it("names the author as the server knows them, the channel and the server, with the text in plain words", () => {
    const n = notificationOf(entry, makeMessage("1", "101", anna, { content: "**<@100>** идёшь?" }));
    expect(n.title).toBe(`Анька (#общий, ${entry.guild.name})`);
    expect(n.body).toBe(`@${entry.members["100"]?.user.display_name ?? ""} идёшь?`);
    expect(n.icon).toMatch(/^http/);
  });

  it("says «Вложение» for a message of files alone and cuts a long text", () => {
    const file = {
      id: "5",
      filename: "a.png",
      content_type: "image/png",
      size: 1,
      width: 1,
      height: 1,
      url: "/a",
      pending: false,
    };
    expect(notificationOf(entry, makeMessage("2", "101", anna, { content: "", attachments: [file] })).body).toBe(
      "📎 Вложение",
    );
    const long = notificationOf(entry, makeMessage("3", "101", anna, { content: "а".repeat(500) })).body;
    expect(long).toHaveLength(200);
    expect(long.endsWith("…")).toBe(true);
  });
});

describe("privateNotificationOf", () => {
  const anya = makeUser("200", { display_name: "Аня" });
  const petya = makeUser("300", { display_name: "Петя" });

  it("names the author alone in a DM, and the group after the author", () => {
    const m = makeMessage("5", "900", anya, { guild_id: null, content: "привет, <@300>" });
    const dm = { channel: makeDMChannel("900", [anya]), read: null };
    expect(privateNotificationOf(dm, { ...m, content: "привет" })).toMatchObject({ title: "Аня", body: "привет" });
    const group = { channel: makeDMChannel("901", [anya, petya], { name: "Пятница" }), read: null };
    expect(privateNotificationOf(group, m)).toMatchObject({ title: "Аня (Пятница)", body: "привет, @Петя" });
  });
});
