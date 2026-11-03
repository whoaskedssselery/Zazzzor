import { makeChannel, makeGuildState, makeMember, makeReady, makeUser, makeVoiceState } from "@/shared/test";
import { applyEvent, applyReady, type GuildsState } from "./reducer";
import { myVoice, voiceMembers } from "./voice";

const me = makeUser("100");
const petya = makeUser("200");
const masha = makeUser("300");
const G = "10";
const LOUNGE = "102";
const GAMES = "103";

/** Server 10 with two voice channels; Petya is in the lounge. */
function ready(): GuildsState {
  return applyReady(
    makeReady(me, [
      makeGuildState(G, me, {
        channels: [
          makeChannel("101", G),
          makeChannel(LOUNGE, G, { type: 2, name: "Лаунж" }),
          makeChannel(GAMES, G, { type: 2, name: "Игры" }),
        ],
        members: [makeMember(me), makeMember(petya), makeMember(masha)],
        voice_states: [makeVoiceState(G, LOUNGE, petya.id)],
      }),
    ]),
  );
}

const update = (s: GuildsState, d: unknown) => applyEvent(s, "VOICE_STATE_UPDATE", d);
const entry = (s: GuildsState) => {
  const e = s.guilds[G];
  if (!e) throw new Error("no server 10");
  return e;
};
const ids = (s: GuildsState, channelId: string) => voiceMembers(entry(s), channelId).map((v) => v.user_id);

describe("voice states", () => {
  it("come with READY, GUILD_CREATE and GUILD_SYNC", () => {
    expect(ids(ready(), LOUNGE)).toEqual([petya.id]);
    const synced = applyEvent(
      ready(),
      "GUILD_SYNC",
      makeGuildState(G, me, {
        channels: [makeChannel(LOUNGE, G, { type: 2 })],
        voice_states: [makeVoiceState(G, LOUNGE, masha.id)],
      }),
    );
    expect(ids(synced, LOUNGE)).toEqual([masha.id]);
  });

  it("VOICE_STATE_UPDATE puts a user in a channel, or moves them, without left", () => {
    let s = update(ready(), { ...makeVoiceState(G, GAMES, masha.id), left: false });
    expect(ids(s, GAMES)).toEqual([masha.id]);
    s = update(s, { ...makeVoiceState(G, LOUNGE, masha.id, { self_mute: true }), left: false });
    expect(ids(s, GAMES)).toEqual([]);
    expect(voiceMembers(entry(s), LOUNGE).find((v) => v.user_id === masha.id)?.self_mute).toBe(true);
    expect(voiceMembers(entry(s), LOUNGE)[0]).not.toHaveProperty("left");
  });

  it("left removes the user only from the channel it names", () => {
    // Petya moved to Games, and the leave of the lounge arrives after.
    let s = update(ready(), { ...makeVoiceState(G, GAMES, petya.id), left: false });
    s = update(s, { ...makeVoiceState(G, LOUNGE, petya.id), left: true });
    expect(ids(s, GAMES)).toEqual([petya.id]);
    s = update(s, { ...makeVoiceState(G, GAMES, petya.id), left: true });
    expect(ids(s, GAMES)).toEqual([]);
  });

  it("go with a deleted channel and with a member who left the server", () => {
    let s = applyEvent(ready(), "CHANNEL_DELETE", makeChannel(LOUNGE, G, { type: 2 }));
    expect(entry(s).voice).toEqual({});
    s = update(ready(), { ...makeVoiceState(G, GAMES, masha.id), left: false });
    s = applyEvent(s, "GUILD_MEMBER_REMOVE", { guild_id: G, user: masha });
    expect(ids(s, GAMES)).toEqual([]);
  });

  it("voiceMembers lists by join time, then by user ID", () => {
    let s = update(ready(), {
      ...makeVoiceState(G, LOUNGE, masha.id, { joined_at: "2026-10-23T17:00:00.000Z" }),
      left: false,
    });
    s = update(s, { ...makeVoiceState(G, LOUNGE, me.id), left: false });
    expect(ids(s, LOUNGE)).toEqual([masha.id, me.id, petya.id]);
  });

  it("myVoice finds where the user is, in any server", () => {
    expect(myVoice(ready())).toBeNull();
    const s = update(ready(), { ...makeVoiceState(G, GAMES, me.id, { self_deaf: true }), left: false });
    expect(myVoice(s)).toMatchObject({ channel_id: GAMES, self_deaf: true });
  });
});
