import { create } from "zustand";

/**
 * How loud the user hears each person and each screen share, in percent:
 * 0 is silent, 100 as sent, up to 200. Kept on this device, as Discord
 * keeps it; a person or a share never set is at 100.
 */
export type VolumeKind = "voice" | "stream";

export const VOLUMES_KEY = "zazzzor.volumes";
export const VOLUME_MAX = 200;
export const VOLUME_DEFAULT = 100;

interface Volumes {
  voice: Record<string, number>;
  stream: Record<string, number>;
}

const EMPTY: Volumes = { voice: {}, stream: {} };

const percent = (v: unknown): number | null =>
  typeof v === "number" && Number.isFinite(v) ? Math.min(VOLUME_MAX, Math.max(0, Math.round(v))) : null;

const valid = (raw: unknown): Record<string, number> => {
  if (typeof raw !== "object" || raw === null) return {};
  const out: Record<string, number> = {};
  for (const [id, v] of Object.entries(raw)) {
    const p = percent(v);
    if (p !== null && p !== VOLUME_DEFAULT) out[id] = p;
  }
  return out;
};

const read = (): Volumes => {
  try {
    const raw = JSON.parse(localStorage.getItem(VOLUMES_KEY) ?? "null") as unknown;
    if (typeof raw !== "object" || raw === null) return EMPTY;
    const { voice, stream } = raw as Record<string, unknown>;
    return { voice: valid(voice), stream: valid(stream) };
  } catch {
    return EMPTY;
  }
};

const store = create<Volumes>()(() => read());

/** The volume of `userId`'s voice or screen share, in percent. */
export const useVolume = (kind: VolumeKind, userId: string): number => store((s) => s[kind][userId] ?? VOLUME_DEFAULT);

/** Every volume that is not 100, outside React. */
export const getVolumes = (): Volumes => store.getState();

/** Sets the volume of `userId`'s voice or screen share (0–200 %) and keeps it. */
export const setVolume = (kind: VolumeKind, userId: string, value: number): void => {
  const p = percent(value) ?? VOLUME_DEFAULT;
  const { [userId]: _old, ...rest } = store.getState()[kind];
  store.setState({ [kind]: p === VOLUME_DEFAULT ? rest : { ...rest, [userId]: p } });
  try {
    localStorage.setItem(VOLUMES_KEY, JSON.stringify(store.getState()));
  } catch {
    // Storage blocked (private mode): the volumes last for this tab only.
  }
};

/** Calls `fn` after every change of a volume; returns the unsubscribe. */
export const onVolumes = (fn: (volumes: Volumes) => void): (() => void) => store.subscribe(fn);

/** Reads the volumes from storage again (tests). */
export const reloadVolumes = (): void => {
  store.setState(read(), true);
};
