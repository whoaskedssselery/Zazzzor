package messages_test

import (
	"context"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
)

// cancelUploadQuery fails one database operation without cancelling the request.
type cancelUploadQuery struct {
	name string
	hit  atomic.Bool
}

func (q *cancelUploadQuery) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "-- name: "+q.name+" ") {
		q.hit.Store(true)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		return cancelled
	}
	return ctx
}

func (*cancelUploadQuery) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestUploadDatabaseFailureCleansOnlyBeforeInsert(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"AttachmentBytesByUploader", 0},
		{"CreateAttachment", 1},
	} {
		t.Run(tc.query, func(t *testing.T) {
			root := t.TempDir()
			disk, err := files.NewDisk(root, files.Options{})
			if err != nil {
				t.Fatalf("NewDisk: %v", err)
			}
			fault := &cancelUploadQuery{name: tc.query}
			e := newEnv(t, func(o *messages.Options) {
				cfg := o.Pool.Config()
				cfg.ConnConfig.Tracer = fault
				pool, poolErr := pgxpool.NewWithConfig(t.Context(), cfg)
				if poolErr != nil {
					t.Fatalf("new traced pool: %v", poolErr)
				}
				t.Cleanup(pool.Close)
				o.Pool, o.Files, o.AttachmentQuotaBytes = pool, disk, 1024
			})
			c, _ := e.user("alice")
			_, ch, _ := newGuild(t, c, "club")
			expect(t, uploadAttachment(t, c, ch.ID, "data.txt", []byte("saved bytes")), http.StatusInternalServerError)
			if !fault.hit.Load() {
				t.Fatalf("failed query %s was not reached", tc.query)
			}
			var rows int
			if err = e.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_attachments").Scan(&rows); err != nil {
				t.Fatalf("count metadata: %v", err)
			}
			if rows != 0 {
				t.Errorf("metadata rows = %d, want 0", rows)
			}
			originals := 0
			err = filepath.WalkDir(filepath.Join(root, "attachments"), func(_ string, entry fs.DirEntry, err error) error {
				if err == nil && entry.Type().IsRegular() {
					originals++
				}
				return err
			})
			if err != nil {
				t.Fatalf("walk originals: %v", err)
			}
			if originals != tc.want {
				t.Errorf("original files = %d, want %d", originals, tc.want)
			}
		})
	}
}
