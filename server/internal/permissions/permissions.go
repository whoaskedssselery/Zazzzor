// Package permissions holds the Discord-style permission model as pure
// functions: no storage, no HTTP, no logging.
package permissions

import (
	"encoding/json"
	"errors"
	"strconv"
)

// Permission is a 64-bit permission mask.
type Permission uint64

// Permission bits, numbered as in Discord.
const (
	CreateInvite       Permission = 1 << 0
	KickMembers        Permission = 1 << 1
	BanMembers         Permission = 1 << 2
	Administrator      Permission = 1 << 3
	ManageChannels     Permission = 1 << 4
	ManageGuild        Permission = 1 << 5
	AddReactions       Permission = 1 << 6
	ViewAuditLog       Permission = 1 << 7
	Stream             Permission = 1 << 9
	ViewChannel        Permission = 1 << 10
	SendMessages       Permission = 1 << 11
	ManageMessages     Permission = 1 << 13
	AttachFiles        Permission = 1 << 15
	ReadMessageHistory Permission = 1 << 16
	MentionEveryone    Permission = 1 << 17
	Connect            Permission = 1 << 20
	Speak              Permission = 1 << 21
	MuteMembers        Permission = 1 << 22
	DeafenMembers      Permission = 1 << 23
	MoveMembers        Permission = 1 << 24
	UseVoiceActivity   Permission = 1 << 25
	ChangeNickname     Permission = 1 << 26
	ManageNicknames    Permission = 1 << 27
	ManageRoles        Permission = 1 << 28
	ManageExpressions  Permission = 1 << 30
)

// All is the union of every defined permission. Unknown bits are never part
// of it.
const All = CreateInvite | KickMembers | BanMembers | Administrator |
	ManageChannels | ManageGuild | AddReactions | ViewAuditLog | Stream | ViewChannel | SendMessages |
	ManageMessages | AttachFiles | ReadMessageHistory | MentionEveryone |
	Connect | Speak | MuteMembers | DeafenMembers | MoveMembers |
	UseVoiceActivity | ChangeNickname | ManageNicknames | ManageRoles | ManageExpressions

// DefaultEveryone is the @everyone mask of a freshly created server.
const DefaultEveryone = AddReactions | ViewChannel | SendMessages | ReadMessageHistory |
	AttachFiles | MentionEveryone | CreateInvite | ChangeNickname |
	Connect | Speak | Stream | UseVoiceActivity

// Has reports whether every bit of p is set.
func (m Permission) Has(p Permission) bool { return m&p == p }

// maxDigits is the length of math.MaxUint64 in decimal.
const maxDigits = 20

// ErrInvalid is returned when a JSON permission is not a valid decimal string.
var ErrInvalid = errors.New("permissions: must be a decimal string that fits uint64")

// MarshalJSON encodes the mask as a decimal string.
func (m Permission) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, strconv.FormatUint(uint64(m), 10)), nil
}

// UnmarshalJSON accepts only a JSON string of decimal digits without
// leading zeros that fits uint64. Unknown bits are kept.
func (m *Permission) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return ErrInvalid
	}
	if s == "" || len(s) > maxDigits || (len(s) > 1 && s[0] == '0') {
		return ErrInvalid
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return ErrInvalid
		}
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return ErrInvalid
	}
	*m = Permission(v)
	return nil
}
