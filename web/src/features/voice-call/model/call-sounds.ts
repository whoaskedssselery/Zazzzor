import { useEffect, useRef } from "react";
import { useCall } from "@/entities/call";
import { type GuildsState, useGuilds } from "@/entities/guild";
import { playSound } from "@/shared/lib";

/** The others in the call the user is in, as sorted IDs joined by commas; "" for none. */
const othersIn = (s: GuildsState, guildId: string | null, channelId: string): string => {
  const voice = guildId === null ? s.privates[channelId]?.voice : s.guilds[guildId]?.voice;
  return Object.values(voice ?? {})
    .filter((v) => v.channel_id === channelId && v.user_id !== s.me?.id)
    .map((v) => v.user_id)
    .sort()
    .join(",");
};

/**
 * Plays a sound when someone comes into the user's voice channel or call,
 * and another when someone leaves it, as Discord does. Who was already
 * there when the user joined (or moved, or reconnected) makes no sound.
 */
export const useCallSounds = (): void => {
  const where = useCall((s) =>
    s.status === "connected" && s.channelId !== null ? `${s.guildId ?? ""}/${s.channelId}` : null,
  );
  const others = useGuilds((s) => {
    if (where === null) return "";
    const [guild = "", channel = ""] = where.split("/");
    return othersIn(s, guild === "" ? null : guild, channel);
  });
  const last = useRef<{ where: string | null; ids: Set<string> }>({ where: null, ids: new Set() });
  useEffect(() => {
    const ids = new Set(others === "" ? [] : others.split(","));
    const before = last.current;
    last.current = { where, ids };
    if (where === null || before.where !== where) return;
    if ([...ids].some((id) => !before.ids.has(id))) playSound("userJoin");
    else if ([...before.ids].some((id) => !ids.has(id))) playSound("userLeave");
  }, [where, others]);
};
