// The cross-import of entities/channel (FSD "@x" notation): what the channel
// list needs of the servers. Only entities/channel imports this file.
export {
  applyGatewayDispatch,
  type Channel,
  computeChannel,
  DEFAULT_EVERYONE,
  type GuildEntry,
  type GuildsState,
  getGuildsState,
  has,
  PERM,
  resetGuilds,
  useGuilds,
} from "../index";
