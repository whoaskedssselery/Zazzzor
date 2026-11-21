# Changelog

**English** · [Русский](CHANGELOG.ru.md)

Notable changes to the Zazzzor server: the API, the web client and the
deploy stacks, which ship together as the `zazzzor-api`, `zazzzor-caddy`
and `zazzzor-backup` images. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until
1.0.0 a minor version may change behaviour; such changes are listed under
**Changed** or **Removed**, with what the server owner has to do.

The Windows desktop app has its own versions (`desktop-vX.Y.Z` tags) and
release notes on its GitHub releases.

## [Unreleased]

### Security

- **HSTS.** The web client and the API now send
  `Strict-Transport-Security: max-age=31536000`, through Caddy and through
  Traefik alike, so browsers stay on HTTPS for a year. There is no
  `preload` or `includeSubDomains`, since DuckDNS names are shared.
  Nothing to do: the header comes with the new `zazzzor-caddy` image (for
  Traefik, redeploy the stack).

### Fixed

- **`ZAZZZOR_ADMIN_USERNAMES` in `deploy/.env.example`.** The comment said
  these logins get administrator rights when they register. In fact
  ordinary sign-up under them is refused: they are reserved for
  `zazzzor create-admin` (section 10 of the deploy guide).

## [0.1.0] - 2026-11-21

The first versioned release: everything a group of friends needs to chat
and talk on their own server.

### Added

- **Servers and channels.** Create servers with text and voice channels in
  categories, invite friends by link, and reorder channels. A new server
  names its default channels in its creator's language.
- **Roles and permissions.** Ranked roles with per-channel overrides,
  nicknames, moderation (kick, ban, deleting other people's messages) and
  voice moderation (server mute and deafen).
- **Realtime chat.** Messages with replies, edits and deletion over a
  WebSocket gateway; history resyncs after a reconnect, so nothing is lost
  when a tab sleeps or the network drops.
- **Direct messages and groups.** One-to-one conversations and group chats,
  with voice and video calls that ring the other members.
- **Friends.** Friend requests, a friends list and blocking.
- **Profiles and statuses.** Avatars, display names, an "about me" text, a
  custom status line and an online, idle, do-not-disturb or invisible
  status.
- **Voice, cameras and screen sharing.** Voice channels on LiveKit with
  speaking indicators, push-to-talk, device selection and Enhanced noise
  suppression (DeepFilterNet3, falling back to RNNoise); cameras; screen
  shares at 1080p that drop to 720p when they cannot hold 30 fps, with tab
  or system audio where the browser allows it; per-person and per-share
  volume from 0 to 200 %.
- **Emoji and reactions.** Reactions on messages and custom server emoji.
- **Mentions and notifications.** User, role, `@everyone` and `@here` mentions,
  unread markers, a counter in the browser tab, and per-server and
  per-channel mute and alert levels.
- **Search.** Message search in a server or a channel, with a filter by
  author.
- **Link previews.** Titles, descriptions and pictures for links, including
  YouTube titles, fetched by the server within a size budget.
- **Attachments.** Files and images in messages, kept private to the people
  who can see the channel, with storage limits set by the owner.
- **Audit log.** A record of administrative actions in each server.
- **Administration.** An admin panel for the instance: open or close
  registration, look up, reset, block and unblock accounts. Administrators
  are named in the configuration or promoted from the command line.
- **English and Russian.** The web client, the server's texts, the
  connectivity test page and the documentation in both languages; each
  person picks the interface language in the settings.
- **Phones.** A layout for phone browsers with message actions, navigation
  and watching screen shares.
- **Backups and restore.** Consistent automatic database and file backups
  with retention, a tested restore, and a restore check that proves a
  backup can be read back.
- **Deploy stacks.** Docker Compose and Docker Swarm stacks behind Caddy
  with DuckDNS and Let's Encrypt, a Swarm stack behind an existing Traefik
  (with optional TURN over TLS), and prebuilt images on GitHub Container
  Registry.

[Unreleased]: https://github.com/whoaskedssselery/Zazzzor/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/whoaskedssselery/Zazzzor/releases/tag/v0.1.0
