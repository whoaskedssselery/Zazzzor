package channels_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

type viewerQueryKey struct{}

// viewerQueries measures named SELECTs and the member assignments returned by them.
type viewerQueries struct {
	selects atomic.Int64
	members atomic.Int64
}

func (q *viewerQueries) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.HasPrefix(data.SQL, "-- name:") {
		q.selects.Add(1)
	}
	return context.WithValue(ctx, viewerQueryKey{}, strings.Contains(data.SQL, "-- name: MembersWithRoles "))
}

func (q *viewerQueries) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if members, _ := ctx.Value(viewerQueryKey{}).(bool); members {
		q.members.Add(data.CommandTag.RowsAffected())
	}
}

func TestConnectedViewersReadOnlyCandidateAssignments(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c, owner := e.user("owner")
	g := createGuild(t, c, "club")
	ch := byName(t, listChannels(t, c, g), channels.DefaultTextChannel)
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO users
		(id, username, display_name, password_hash, created_at)
		SELECT id, 'offline' || id, 'Offline', 'unused', now() FROM generate_series(10001, 10499) AS id`); err != nil {
		t.Fatalf("insert offline accounts: %v", err)
	}
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO members (guild_id, user_id, joined_at)
		SELECT $1, id, now() FROM generate_series(10001, 10499) AS id`, int64(g.id(t))); err != nil {
		t.Fatalf("insert offline memberships: %v", err)
	}
	counts := &viewerQueries{}
	cfg := e.pool.Config()
	cfg.ConnConfig.Tracer = counts
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatalf("new traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	svc := channels.NewService(channels.Options{Pool: pool, Guilds: e.guilds})
	if _, err = svc.Viewers(t.Context(), ch.id(t)); err != nil {
		t.Fatalf("Viewers: %v", err)
	}
	t.Logf("all members: queries=%d member assignments=%d", counts.selects.Load(), counts.members.Load())
	if counts.selects.Load() != 5 || counts.members.Load() != 500 {
		t.Fatalf("baseline queries/members = %d/%d, want 5/500", counts.selects.Load(), counts.members.Load())
	}
	counts.selects.Store(0)
	counts.members.Store(0)
	candidates := []snowflake.ID{owner.id(t), 10001, 10002, 10003, 10004, 999999}
	ids, _, err := svc.ViewersAmong(t.Context(), ch.id(t), candidates)
	if err != nil {
		t.Fatalf("ViewersAmong: %v", err)
	}
	t.Logf("connected members: queries=%d member assignments=%d", counts.selects.Load(), counts.members.Load())
	if len(ids) != 5 || counts.selects.Load() != 5 || counts.members.Load() != 5 {
		t.Errorf("viewers/queries/members = %d/%d/%d, want 5/5/5", len(ids), counts.selects.Load(), counts.members.Load())
	}
	expect(t, putOverwrite(c, ch.ID, "10001", 1, 0, permissions.ViewChannel), http.StatusOK)
	ids, readers, err := svc.ViewersAmong(t.Context(), ch.id(t), candidates)
	if err != nil {
		t.Fatalf("ViewersAmong after revocation: %v", err)
	}
	if len(ids) != 4 || readers[10001] {
		t.Errorf("revoked viewer count/history = %d/%v, want 4/false", len(ids), readers[10001])
	}
	ids, _, err = svc.ViewersAmong(t.Context(), ch.id(t), nil)
	if err != nil || len(ids) != 0 {
		t.Errorf("empty candidates viewers/error = %v/%v, want []/nil", ids, err)
	}
	access, err := e.guilds.AccessAmongIn(t.Context(), e.pool, g.id(t), nil)
	if err != nil || len(access.MemberIDs()) != 0 {
		t.Errorf("empty guild candidates members/error = %d/%v, want 0/nil", len(access.MemberIDs()), err)
	}
}
