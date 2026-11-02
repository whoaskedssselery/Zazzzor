// Package voice connects members to voice channels through LiveKit (the
// SFU; media never passes through the API). It issues LiveKit tokens whose
// grants follow the member's permissions in the channel, keeps who is in
// which voice channel, and announces it as VOICE_STATE_UPDATE.
//
// LiveKit webhooks are the source of truth for presence in a room: a state
// appears when LiveKit reports the connection, not when a token is issued.
// The states live in memory, keyed by user (a user is in at most one voice
// channel), so the API must run as a single instance. After a restart they
// are rebuilt from the LiveKit server API (Run reconciles at start and
// every ReconcileInterval); the self_mute and self_deaf flags are not known
// to LiveKit and start false, so clients send them again after READY.
// Server mute/deaf flags are kept per guild and user across reconnects,
// until lifted or the API restarts. Admission reapplies current grants to
// LiveKit, including when a token predates a moderation change.
//
// A room is named by its voice channel's ID; rooms with other names (the
// connectivity test's) are left alone.
package voice
