// Package relationships owns friend requests, friendships and private blocks.
package relationships

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/events"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/db"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/relationships/store"
)

// Type is the relationship as seen by its owner.
type Type int16

// Relationship types use Discord's directional values.
const (
	Friend   Type = 1
	Blocked  Type = 2
	Incoming Type = 3
	Outgoing Type = 4
)

// Relationship describes the other account and the current directional state.
type Relationship struct {
	ID    snowflake.ID    `json:"id"`
	Type  Type            `json:"type"`
	User  auth.PublicUser `json:"user"`
	Since string          `json:"since"`
}

// Removed identifies the state that disappeared from one user's list.
type Removed struct {
	ID   snowflake.ID `json:"id"`
	Type Type         `json:"type"`
}

// Options configures a Service. Pool is required, Now defaults to time.Now.
type Options struct {
	Pool      *pgxpool.Pool
	Publisher events.Publisher
	Now       func() time.Time
}

// Service stores both sides of a relationship in one transaction.
type Service struct {
	pool *pgxpool.Pool
	q    *store.Queries
	pub  events.Publisher
	now  func() time.Time
	// A pair's commit and events must reach this process's gateway in order.
	locks [64]sync.Mutex
}

// NewService builds a relationship service from o.
func NewService(o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Service{pool: o.Pool, q: store.New(), pub: o.Publisher, now: o.Now}
}

// List returns the caller's relationships with current public profiles.
func (s *Service) List(ctx context.Context, user snowflake.ID) ([]Relationship, error) {
	rows, err := s.q.ListRelationships(ctx, s.pool, int64(user))
	if err != nil {
		return nil, fmt.Errorf("relationships: list: %w", err)
	}
	out := make([]Relationship, len(rows))
	for i, row := range rows {
		id := snowflake.ID(row.OtherID)
		profile := publicUser(id, row.Username, row.DisplayName, row.AvatarHash, row.CreatedAt)
		out[i] = relationship(profile, Type(row.Type), row.Since)
	}
	return out, nil
}

// Request charges an attempt before resolving the username. An existing
// incoming request is accepted without creating another pending row.
func (s *Service) Request(ctx context.Context, actor snowflake.ID, username string) error {
	if err := s.chargeRequest(ctx, actor); err != nil {
		return fmt.Errorf("relationships: limit request: %w", err)
	}
	name, valid := auth.NormalizeUsername(username)
	if !valid {
		return httpx.NotFound()
	}
	id, err := s.q.RelationshipUserByUsername(ctx, s.pool, name)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && snowflake.ID(id) == actor) {
		return httpx.NotFound()
	}
	if err != nil {
		return fmt.Errorf("relationships: look up username: %w", err)
	}
	return s.change(ctx, actor, snowflake.ID(id), requestFriend)
}

// Put accepts an incoming request or creates a private block.
func (s *Service) Put(ctx context.Context, actor, other snowflake.ID, kind Type) error {
	if actor == other {
		return httpx.NotFound()
	}
	switch kind {
	case Friend:
		return s.change(ctx, actor, other, acceptFriend)
	case Blocked:
		return s.change(ctx, actor, other, blockUser)
	default:
		return httpx.BadRequest("validation_failed", "some fields are invalid",
			httpx.FieldError{Field: "type", Message: "must be 1 or 2"})
	}
}

// Delete removes the caller's relationship, preserving any invisible block.
func (s *Service) Delete(ctx context.Context, actor, other snowflake.ID) error {
	if actor == other {
		return nil
	}
	return s.change(ctx, actor, other, removeRelationship)
}

// change locks both accounts, applies the transition and publishes only
// committed differences. Database locks also cover concurrent pairs that
// share one account and therefore share its capacity.
func (s *Service) change(ctx context.Context, actor, other snowflake.ID, op operation) error {
	hash := int64(actor) ^ int64(other) ^ actor.Time().UnixMilli() ^ other.Time().UnixMilli()
	mu := &s.locks[hash&int64(len(s.locks)-1)]
	mu.Lock()
	defer mu.Unlock()
	var profiles [2]auth.PublicUser
	var before, after pair
	var since time.Time
	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		people, err := s.q.LockRelationshipUsers(ctx, tx, store.LockRelationshipUsersParams{ActorID: int64(actor), OtherID: int64(other)})
		if err != nil {
			return fmt.Errorf("relationships: lock accounts: %w", err)
		}
		before, after = pair{}, pair{}
		if len(people) != 2 {
			if op == removeRelationship {
				return nil
			}
			return httpx.NotFound()
		}
		for _, row := range people {
			id := snowflake.ID(row.ID)
			i := 0
			if id == other {
				i = 1
			}
			profiles[i] = publicUser(id, row.Username, row.DisplayName, row.AvatarHash, row.CreatedAt)
		}
		rows, err := s.q.RelationshipPair(ctx, tx, store.RelationshipPairParams{ActorID: int64(actor), OtherID: int64(other)})
		if err != nil {
			return fmt.Errorf("relationships: load pair: %w", err)
		}
		for _, r := range rows {
			if snowflake.ID(r.UserID) == actor {
				before.own = Type(r.Type)
			} else {
				before.other = Type(r.Type)
			}
		}
		after, err = transition(op, before)
		if err != nil || before == after {
			return err
		}
		if err = s.checkCapacity(ctx, tx, actor, other, before, after); err != nil {
			return err
		}
		since = s.now()
		if err = s.writeSide(ctx, tx, actor, other, before.own, after.own, since); err != nil {
			return err
		}
		return s.writeSide(ctx, tx, other, actor, before.other, after.other, since)
	})
	if err != nil {
		return fmt.Errorf("relationships: change pair: %w", err)
	}
	s.publishSide(ctx, actor, profiles[1], before.own, after.own, since)
	s.publishSide(ctx, other, profiles[0], before.other, after.other, since)
	return nil
}

// writeSide updates one direction only when its state changes.
func (s *Service) writeSide(ctx context.Context, tx pgx.Tx, user, other snowflake.ID, before, after Type, since time.Time) error {
	if before == after {
		return nil
	}
	if after == 0 {
		if err := s.q.DeleteRelationship(ctx, tx, store.DeleteRelationshipParams{UserID: int64(user), OtherID: int64(other)}); err != nil {
			return fmt.Errorf("relationships: delete direction: %w", err)
		}
		return nil
	}
	if err := s.q.SaveRelationship(ctx, tx, store.SaveRelationshipParams{
		UserID: int64(user), OtherID: int64(other), Type: int16(after), Since: since,
	}); err != nil {
		return fmt.Errorf("relationships: save direction: %w", err)
	}
	return nil
}

// publishSide upserts changed states and removes only an owner's visible row.
func (s *Service) publishSide(ctx context.Context, owner snowflake.ID, other auth.PublicUser, before, after Type, since time.Time) {
	if s.pub == nil || before == after {
		return
	}
	e := events.Event{Audience: events.Audience{Kind: events.AudienceUsers, UserIDs: []snowflake.ID{owner}}}
	if after == 0 {
		e.Type, e.Payload = events.RelationshipRemove, Removed{ID: other.ID, Type: before}
	} else {
		e.Type, e.Payload = events.RelationshipAdd, relationship(other, after, since)
	}
	s.pub.Publish(ctx, e)
}

// publicUser reuses account serialization and never reads credentials.
func publicUser(id snowflake.ID, username, displayName string, avatarHash *string, createdAt time.Time) auth.PublicUser {
	return (auth.User{ID: id, Username: username, DisplayName: displayName, AvatarHash: avatarHash, CreatedAt: createdAt}).Public()
}

func relationship(other auth.PublicUser, kind Type, since time.Time) Relationship {
	return Relationship{ID: other.ID, User: other, Type: kind, Since: httpx.FormatTime(since)}
}
