// The cross-import of entities/channel (FSD "@x" notation): the read-only
// part of the servers the channel list needs. No store writers here; the
// channel tests seed the store through @/entities/guild like any test.
export {
  type Channel,
  computeChannel,
  type GuildEntry,
  type GuildsState,
  getGuildsState,
  has,
  PERM,
  useGuilds,
} from "../index";
