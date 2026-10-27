package auth

import (
	"context"
	"fmt"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// AvatarReferences lists the avatars of all accounts, for files.Reconcile.
func (s *Service) AvatarReferences(ctx context.Context) ([]files.Reference, error) {
	rows, err := s.q.AvatarReferences(ctx, s.pool)
	if err != nil {
		return nil, fmt.Errorf("auth: list avatar references: %w", err)
	}
	out := make([]files.Reference, len(rows))
	for i, row := range rows {
		out[i] = files.Reference{Kind: files.KindAvatar, OwnerID: row.ID, Hash: row.Hash}
	}
	return out, nil
}

// AvatarReferenced reports whether the account userID has the avatar hash
// right now, for files.Reconcile.
func (s *Service) AvatarReferenced(ctx context.Context, userID snowflake.ID, hash string) (bool, error) {
	ok, err := s.q.AvatarReferenced(ctx, s.pool, store.AvatarReferencedParams{ID: userID, Hash: hash})
	if err != nil {
		return false, fmt.Errorf("auth: check avatar reference: %w", err)
	}
	return ok, nil
}
