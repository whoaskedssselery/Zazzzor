import { compareIds } from "@/shared/lib";
import type { GuildEntry, GuildsState, VoiceState } from "./reducer";

/** Who is in voice channel `channelId`, in the order they joined (then by user ID). */
export const voiceMembers = (entry: GuildEntry, channelId: string): VoiceState[] =>
  Object.values(entry.voice)
    .filter((v) => v.channel_id === channelId)
    .sort((a, b) => a.joined_at.localeCompare(b.joined_at) || compareIds(a.user_id, b.user_id));

/** Where the signed-in user is in voice, in any server or call; null when nowhere. */
export const myVoice = (s: GuildsState): VoiceState | null => {
  const id = s.me?.id;
  if (id === undefined) return null;
  for (const holder of [...Object.values(s.guilds), ...Object.values(s.privates)]) {
    const v = holder.voice[id];
    if (v) return v;
  }
  return null;
};

/** Who is in the call of private channel `channelId`, in the order they joined. */
export const callMembers = (s: Pick<GuildsState, "privates">, channelId: string): VoiceState[] =>
  Object.values(s.privates[channelId]?.voice ?? {}).sort(
    (a, b) => a.joined_at.localeCompare(b.joined_at) || compareIds(a.user_id, b.user_id),
  );

/** Whether the call of `channelId` rings the signed-in user. */
export const ringsMe = (s: GuildsState, channelId: string): boolean =>
  s.me !== null && (s.calls[channelId]?.ringing.includes(s.me.id) ?? false);
