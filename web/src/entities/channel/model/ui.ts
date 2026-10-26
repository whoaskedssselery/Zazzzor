import { z } from "zod/mini";
import { create } from "zustand";

/** Where the channel list's own state lives between visits (localStorage). */
const STORAGE_KEY = "zazzzor.ui";

const savedSchema = z.object({
  lastChannel: z.record(z.string(), z.string()),
  collapsed: z.record(z.string(), z.literal(true)),
});

type Saved = z.infer<typeof savedSchema>;

interface ChannelUi extends Saved {
  /** Remembers `channelId` as the channel to open in server `guildId`. */
  setLastChannel: (guildId: string, channelId: string) => void;
  /** Collapses category `categoryId`, or expands it when collapsed. */
  toggleCategory: (categoryId: string) => void;
}

const EMPTY: Saved = { lastChannel: {}, collapsed: {} };

// Storage may be missing or throw (private mode, blocked site data): the
// list then simply forgets between visits.
const load = (): Saved => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === null) return EMPTY;
    const saved = savedSchema.safeParse(JSON.parse(raw));
    return saved.success ? saved.data : EMPTY;
  } catch {
    return EMPTY;
  }
};

const save = ({ lastChannel, collapsed }: Saved): void => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ lastChannel, collapsed }));
  } catch {
    // Kept in memory only.
  }
};

/** The last channel of each server and the collapsed categories, kept across visits. */
export const useChannelUi = create<ChannelUi>()((set, get) => ({
  ...load(),
  setLastChannel: (guildId, channelId) => {
    set({ lastChannel: { ...get().lastChannel, [guildId]: channelId } });
    save(get());
  },
  toggleCategory: (categoryId) => {
    const { [categoryId]: wasCollapsed, ...rest } = get().collapsed;
    set({ collapsed: wasCollapsed ? rest : { ...rest, [categoryId]: true } });
    save(get());
  },
}));

/** Forgets everything of the session, in storage too (the session ended). */
export const resetChannelUi = (): void => {
  useChannelUi.setState(EMPTY);
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    // Nothing was saved then.
  }
};
