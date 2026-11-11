package main

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/channels"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
)

func TestFilesReconcileRestoresReferencedAttachment(t *testing.T) {
	m, cfg := newFilesModules(t)
	ctx := t.Context()
	u, _, err := m.Auth.Register(ctx, auth.RegisterInput{Username: "restore", Password: "password123"}, auth.SessionMeta{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	g, err := m.Guilds.CreateGuild(ctx, u, "restore")
	if err != nil {
		t.Fatalf("CreateGuild: %v", err)
	}
	ch, err := m.Channels.CreateChannel(ctx, g.Guild.ID, u.ID, channels.CreateFields{Name: "files", Type: channels.TypeText})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	const id = 42
	original := []byte("restore these original bytes")
	ref := files.AttachmentReference(ch.ID, id)
	info, err := m.Files.SaveAttachment(ctx, ref, bytes.NewReader(original))
	if err != nil {
		t.Fatalf("SaveAttachment: %v", err)
	}
	// A database dump taken before the move still references the file.
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("open fixture database: %v", err)
	}
	defer pool.Close()
	_, err = pool.Exec(ctx, `INSERT INTO message_attachments
		(id, channel_id, uploader_id, filename, content_type, size, digest, created_at)
		VALUES ($1, $2, $3, 'file', $4, $5, $6, now())`,
		int64(id), int64(ch.ID), int64(u.ID), info.ContentType, info.Size, info.Digest)
	if err != nil {
		t.Fatalf("seed attachment reference: %v", err)
	}
	if err = m.Files.DeleteAttachment(ref); err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"files", "reconcile"}, filesEnv(cfg), &stdout, &stderr); code != 0 {
		t.Fatalf("reconcile exit = %d, want 0: %s", code, &stderr)
	}
	if !strings.Contains(stdout.String(), "restored 1") {
		t.Errorf("reconcile stdout = %q, want one restored file", stdout.String())
	}
	f, err := m.Files.OpenAttachment(ref)
	if err != nil {
		t.Fatalf("open reconciled attachment: %v", err)
	}
	defer func() { _ = f.Close() }()
	got, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(got, original) {
		t.Errorf("restored bytes = %q, %v, want %q", got, err, original)
	}
}
