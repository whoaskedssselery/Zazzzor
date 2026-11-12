import { afterEach, describe, expect, it, vi } from "vitest";
import { makeGuildState, makeReady, makeUser } from "@/shared/test";
import type { components } from "../schema.gen";
import { parseEvent } from "./protocol";

type VoiceState = components["schemas"]["VoiceState"];

const me = makeUser("100");

const voiceState = (over: Partial<VoiceState> = {}): VoiceState => ({
  guild_id: "1",
  channel_id: "12",
  user_id: me.id,
  self_mute: false,
  self_deaf: false,
  self_stream: true,
  self_video: false,
  mute: false,
  deaf: false,
  joined_at: "2026-11-02T10:00:00Z",
  ...over,
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("guild state voice_states", () => {
  for (const t of ["GUILD_CREATE", "GUILD_SYNC"] as const) {
    it(`${t} keeps the voice states of the snapshot`, () => {
      const state = makeGuildState("1", me, { voice_states: [voiceState()] });
      expect(parseEvent(t, state)?.voice_states).toEqual([voiceState()]);
    });

    it(`${t} drops a snapshot whose voice state is malformed`, () => {
      vi.spyOn(console, "warn").mockImplementation(() => {});
      const bad = { ...voiceState(), self_mute: "yes" };
      expect(parseEvent(t, { ...makeGuildState("1", me), voice_states: [bad] })).toBeNull();
    });

    it(`${t} drops a snapshot without voice_states`, () => {
      vi.spyOn(console, "warn").mockImplementation(() => {});
      const { voice_states: _, ...withoutVoice } = makeGuildState("1", me);
      expect(parseEvent(t, withoutVoice)).toBeNull();
    });
  }

  it("READY keeps the voice states of every guild", () => {
    const ready = makeReady(me, [makeGuildState("1", me, { voice_states: [voiceState()] })]);
    expect(parseEvent("READY", ready)?.guilds[0]?.voice_states).toEqual([voiceState()]);
  });
});
