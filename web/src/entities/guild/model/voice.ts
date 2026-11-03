import { compareIds } from "@/shared/lib";
import type { GuildEntry, GuildsState, VoiceState } from "./reducer";

/** Who is in voice channel `channelId`, in the order they joined (then by user ID). */
export const voiceMembers = (entry: GuildEntry, channelId: string): VoiceState[] =>
  Object.values(entry.voice)
    .filter((v) => v.channel_id === channelId)
    .sort((a, b) => a.joined_at.localeCompare(b.joined_at) || compareIds(a.user_id, b.user_id));

/** Where the signed-in user is in voice, in any server; null when nowhere. */
export const myVoice = (s: GuildsState): VoiceState | null => {
  const id = s.me?.id;
  if (id === undefined) return null;
  for (const entry of Object.values(s.guilds)) {
    const v = entry.voice[id];
    if (v) return v;
  }
  return null;
};
