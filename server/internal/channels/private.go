package channels

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// privatePermissions are every recipient's in a DM or a group DM: the
// chat, and the call (voice, screen share and camera).
const privatePermissions = permissions.ViewChannel | permissions.ReadMessageHistory |
	permissions.SendMessages | permissions.AttachFiles | permissions.AddReactions |
	permissions.Connect | permissions.Speak | permissions.Stream | permissions.UseVoiceActivity

// DMChannel is the private channel as seen by one recipient.
type DMChannel struct {
	ID            snowflake.ID      `json:"id"`
	Type          Type              `json:"type"`
	Name          *string           `json:"name"`
	OwnerID       *snowflake.ID     `json:"owner_id"`
	Recipients    []auth.PublicUser `json:"recipients"`
	LastMessageID *snowflake.ID     `json:"last_message_id"`
}

// DMChannelDelete identifies the private channel removed from a recipient's list.
type DMChannelDelete struct {
	ID snowflake.ID `json:"id"`
}

// privateSnapshot holds the public profiles once for all recipient views.
type privateSnapshot struct {
	row     store.Channel
	members []auth.PublicUser
}

func (p privateSnapshot) view(user snowflake.ID) DMChannel {
	others := make([]auth.PublicUser, 0, len(p.members))
	for _, member := range p.members {
		if member.ID != user {
			others = append(others, member)
		}
	}
	return DMChannel{ID: p.row.ID, Type: Type(p.row.Type), Name: p.row.Name, OwnerID: p.row.OwnerID,
		Recipients: others, LastMessageID: p.row.LastMessageID}
}

func (p privateSnapshot) event(typ string, user snowflake.ID) events.Event {
	return events.Event{Type: typ, Audience: events.Audience{Kind: events.AudiencePrivateChannel,
		ChannelID: p.row.ID, UserIDs: []snowflake.ID{user}}, Payload: p.view(user)}
}

func privateDelete(id, user snowflake.ID) events.Event {
	return events.Event{Type: events.DMChannelDelete, Audience: events.Audience{Kind: events.AudienceUsers,
		UserIDs: []snowflake.ID{user}}, Payload: DMChannelDelete{ID: id}}
}

// privateProfiles resolves only the fields used in a DM's public recipient list.
func (s *Service) privateProfiles(ctx context.Context, q store.DBTX, ids []int64) (map[snowflake.ID][]auth.PublicUser, error) {
	rows, err := s.q.PrivateProfiles(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("channels: read private recipients: %w", err)
	}
	out := make(map[snowflake.ID][]auth.PublicUser)
	for _, r := range rows {
		profile := auth.User{ID: snowflake.ID(r.ID), Username: r.Username, DisplayName: r.DisplayName, AvatarHash: r.AvatarHash, CreatedAt: r.CreatedAt}
		id := snowflake.ID(r.ChannelID)
		out[id] = append(out[id], profile.Public())
	}
	return out, nil
}

// privateSnapshot loads one private channel and its current recipients through q.
func (s *Service) privateSnapshot(ctx context.Context, q store.DBTX, id snowflake.ID) (privateSnapshot, error) {
	row, err := s.q.ChannelByID(ctx, q, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return privateSnapshot{}, ErrNotFound
	}
	if err != nil {
		return privateSnapshot{}, fmt.Errorf("channels: read private channel: %w", err)
	}
	if !Type(row.Type).Private() {
		return privateSnapshot{}, ErrNotFound
	}
	profiles, err := s.privateProfiles(ctx, q, []int64{int64(id)})
	if err != nil {
		return privateSnapshot{}, err
	}
	return privateSnapshot{row: row, members: profiles[id]}, nil
}

// PrivateChannels lists open 1:1 DMs and groups in one read snapshot.
func (s *Service) PrivateChannels(ctx context.Context, user snowflake.ID) ([]DMChannel, error) {
	out := []DMChannel{}
	err := s.read(ctx, func(tx pgx.Tx) error {
		rows, err := s.q.OpenPrivateChannels(ctx, tx, int64(user))
		if err != nil {
			return fmt.Errorf("channels: list private channels: %w", err)
		}
		ids := make([]int64, len(rows))
		for i, row := range rows {
			ids[i] = int64(row.ID)
		}
		profiles, err := s.privateProfiles(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, row := range rows {
			snapshot := privateSnapshot{row: row, members: profiles[row.ID]}
			out = append(out, snapshot.view(user))
		}
		return nil
	})
	return out, err
}

// PrivateChannel returns one private channel only to a current recipient.
func (s *Service) PrivateChannel(ctx context.Context, user, id snowflake.ID) (DMChannel, error) {
	var out DMChannel
	err := s.read(ctx, func(tx pgx.Tx) error {
		snapshot, err := s.privateSnapshot(ctx, tx, id)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(snapshot.members, func(m auth.PublicUser) bool { return m.ID == user }) {
			return ErrNotFound
		}
		out = snapshot.view(user)
		return nil
	})
	return out, err
}

// PrivateRecipientsIn reads the current recipients through the write transaction.
func (s *Service) PrivateRecipientsIn(ctx context.Context, tx pgx.Tx, id snowflake.ID) ([]snowflake.ID, error) {
	ids, err := s.q.PrivateRecipientIDs(ctx, tx, int64(id))
	if err != nil {
		return nil, fmt.Errorf("channels: list private recipient IDs: %w", err)
	}
	out := make([]snowflake.ID, len(ids))
	for i, user := range ids {
		out[i] = snowflake.ID(user)
	}
	return out, nil
}

// LockPrivateIn orders private message writes and membership changes.
func (s *Service) LockPrivateIn(ctx context.Context, tx pgx.Tx, id snowflake.ID) error {
	_, err := s.q.LockPrivateChannel(ctx, tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("channels: lock private channel: %w", err)
	}
	return nil
}

// LockPrivateEvents orders message commits and publications for a channel,
// including its membership events when the channel is private.
func (s *Service) LockPrivateEvents(id snowflake.ID) func() {
	key := int64(id) ^ id.Time().UnixMilli()
	mu := &s.privateLocks[key&int64(len(s.privateLocks)-1)]
	mu.Lock()
	return mu.Unlock
}

// privateWrite orders channel state changes with sends and publishes only after commit.
func (s *Service) privateWrite(ctx context.Context, id, user snowflake.ID,
	change func(pgx.Tx, privateSnapshot) ([]events.Event, error),
) error {
	defer s.LockPrivateEvents(id)()
	var announce []events.Event
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.LockPrivateIn(ctx, tx, id); err != nil {
			return err
		}
		snapshot, err := s.privateSnapshot(ctx, tx, id)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(snapshot.members, func(m auth.PublicUser) bool { return m.ID == user }) {
			return ErrNotFound
		}
		announce, err = change(tx, snapshot)
		return err
	})
	if err != nil {
		return fmt.Errorf("channels: change private channel: %w", err)
	}
	for _, event := range announce {
		s.publish(ctx, event)
	}
	return nil
}

// CloseDirect closes the channel only for this recipient.
func (s *Service) CloseDirect(ctx context.Context, id, user snowflake.ID) error {
	return s.privateWrite(ctx, id, user, func(tx pgx.Tx, snapshot privateSnapshot) ([]events.Event, error) {
		if Type(snapshot.row.Type) != TypeDM {
			return nil, invalid("type", "must be a 1:1 DM")
		}
		n, err := s.q.SetDirectClosed(ctx, tx, store.SetDirectClosedParams{ChannelID: int64(id), UserID: int64(user), Closed: true})
		if err != nil {
			return nil, fmt.Errorf("channels: close direct channel: %w", err)
		}
		if n == 0 {
			return nil, nil
		}
		return []events.Event{privateDelete(id, user)}, nil
	})
}
