package relationships

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/relationships/store"
)

const (
	maxFriends  = 1000
	maxOutgoing = 100
)

type operation int

const (
	requestFriend operation = iota
	acceptFriend
	blockUser
	removeRelationship
)

type pair struct {
	own, other Type
}

// transition applies the privacy rules before checking capacity, so a
// hidden block returns exactly the same success at any quota.
func transition(op operation, before pair) (pair, error) {
	switch op {
	case requestFriend:
		switch {
		case before.own == Blocked:
			return before, httpx.BadRequest("blocked_by_you", "unblock this user first")
		case before.other == Blocked:
			return before, nil
		case before.own == Incoming:
			return pair{Friend, Friend}, nil
		case before.own == 0:
			return pair{Outgoing, Incoming}, nil
		default:
			return before, nil
		}
	case acceptFriend:
		if before.own != Incoming || before.other != Outgoing {
			return before, httpx.NotFound()
		}
		return pair{Friend, Friend}, nil
	case blockUser:
		if before.other == Blocked {
			return pair{Blocked, Blocked}, nil
		}
		return pair{own: Blocked}, nil
	case removeRelationship:
		if before.own == 0 {
			return before, nil
		}
		if before.own == Blocked {
			return pair{other: before.other}, nil
		}
		return pair{}, nil
	}
	return before, fmt.Errorf("relationships: invalid operation %d", op)
}

// checkCapacity counts newly introduced states under both account locks.
func (s *Service) checkCapacity(ctx context.Context, tx pgx.Tx, actor, other snowflake.ID, before, after pair) error {
	if after.own == Friend && before.own != Friend {
		for _, id := range []snowflake.ID{actor, other} {
			n, err := s.q.CountRelationships(ctx, tx, store.CountRelationshipsParams{UserID: int64(id), Type: int16(Friend)})
			if err != nil {
				return fmt.Errorf("relationships: count friends: %w", err)
			}
			if n >= maxFriends {
				return httpx.BadRequest("too_many_friends", fmt.Sprintf("a user can have at most %d friends", maxFriends))
			}
		}
	}
	if after.own == Outgoing && before.own != Outgoing {
		n, err := s.q.CountRelationships(ctx, tx, store.CountRelationshipsParams{UserID: int64(actor), Type: int16(Outgoing)})
		if err != nil {
			return fmt.Errorf("relationships: count outgoing requests: %w", err)
		}
		if n >= maxOutgoing {
			return httpx.BadRequest("too_many_outgoing_requests", fmt.Sprintf("a user can have at most %d outgoing requests", maxOutgoing))
		}
	}
	return nil
}
