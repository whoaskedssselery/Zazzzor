package guilds

import (
	"context"
	"slices"
	"strconv"

	"github.com/whoaskedssselery/zazzzor/server/internal/audit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// record writes an audit log entry of guildID in tx; without a recorder it
// does nothing.
func (s *Service) record(ctx context.Context, tx audit.Tx, e audit.Entry) error {
	return audit.Record(ctx, s.audit, tx, e)
}

// roleChanges lists what differs between two states of a role; a zero
// before is a creation, a zero after a deletion.
func roleChanges(before, after Role) []audit.Change {
	var out []audit.Change
	add := func(key string, old, next any, same bool) {
		if !same {
			out = append(out, audit.Change{Key: key, Old: old, New: next})
		}
	}
	created, deleted := before.ID == 0, after.ID == 0
	val := func(missing bool, v any) any {
		if missing {
			return nil
		}
		return v
	}
	add("name", val(created, before.Name), val(deleted, after.Name), before.Name == after.Name)
	add("color", val(created, before.Color), val(deleted, after.Color), before.Color == after.Color)
	add("permissions", val(created, strconv.FormatUint(uint64(before.Permissions), 10)), val(deleted, strconv.FormatUint(uint64(after.Permissions), 10)),
		before.Permissions == after.Permissions)
	add("hoist", val(created, before.Hoist), val(deleted, after.Hoist), before.Hoist == after.Hoist)
	add("mentionable", val(created, before.Mentionable), val(deleted, after.Mentionable),
		before.Mentionable == after.Mentionable)
	return out
}

// roleDiff is the roles added to and removed from a member, for
// MEMBER_ROLE_UPDATE ($add and $remove, as Discord names them).
func roleDiff(have, want []snowflake.ID) []audit.Change {
	var added, removed []string
	for _, id := range want {
		if !slices.Contains(have, id) {
			added = append(added, id.String())
		}
	}
	for _, id := range have {
		if !slices.Contains(want, id) {
			removed = append(removed, id.String())
		}
	}
	var out []audit.Change
	if len(added) > 0 {
		out = append(out, audit.Change{Key: "$add", New: added})
	}
	if len(removed) > 0 {
		out = append(out, audit.Change{Key: "$remove", New: removed})
	}
	return out
}
