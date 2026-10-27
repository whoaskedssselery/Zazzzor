package guilds

import (
	"context"
	"fmt"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/guilds/store"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

// IconReferences lists the icons of all guilds, for files.Reconcile.
func (s *Service) IconReferences(ctx context.Context) ([]files.Reference, error) {
	rows, err := s.q.IconReferences(ctx, s.pool)
	if err != nil {
		return nil, fmt.Errorf("guilds: list icon references: %w", err)
	}
	out := make([]files.Reference, len(rows))
	for i, row := range rows {
		out[i] = files.Reference{Kind: files.KindIcon, OwnerID: row.ID, Hash: row.Hash}
	}
	return out, nil
}

// IconReferenced reports whether the guild guildID has the icon hash right
// now, for files.Reconcile.
func (s *Service) IconReferenced(ctx context.Context, guildID snowflake.ID, hash string) (bool, error) {
	ok, err := s.q.IconReferenced(ctx, s.pool, store.IconReferencedParams{ID: guildID, Hash: hash})
	if err != nil {
		return false, fmt.Errorf("guilds: check icon reference: %w", err)
	}
	return ok, nil
}
