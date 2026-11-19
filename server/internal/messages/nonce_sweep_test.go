package messages_test

import (
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/messages"
)

func TestSweepExpiresInactiveAuthorsNoncesInBatches(t *testing.T) {
	e := newEnv(t)
	c, user := e.user("alice")
	_, ch, _ := newGuild(t, c, "club")
	msg := post(t, c, ch.ID, "keep message")
	live := "still-live"
	if _, err := e.msgs.Create(t.Context(), parseID(t, ch.ID), parseID(t, user.ID), messages.CreateFields{Content: "live", Nonce: &live}); err != nil {
		t.Fatalf("create live nonce: %v", err)
	}
	// The author does not send again after these claims expire.
	if _, err := e.pool.Exec(t.Context(), `INSERT INTO message_nonces
		(author_id, channel_id, nonce, message_id, expires_at)
		SELECT $1, $2, id::text, $3, now() - interval '1 hour'
		FROM generate_series(1, 1500) AS id ON CONFLICT DO NOTHING`, int64(parseID(t, user.ID)), int64(parseID(t, ch.ID)), int64(parseID(t, msg.ID))); err != nil {
		t.Fatalf("restore expired nonces: %v", err)
	}
	if err := e.msgs.SweepAttachments(t.Context()); err != nil {
		t.Fatalf("SweepAttachments: %v", err)
	}
	var expired, fresh int
	if err := e.pool.QueryRow(t.Context(), `SELECT count(*) FILTER (WHERE expires_at <= now()),
		count(*) FILTER (WHERE expires_at > now()) FROM message_nonces`).Scan(&expired, &fresh); err != nil {
		t.Fatalf("count nonces: %v", err)
	}
	if expired == 0 || expired == 1500 || fresh != 1 {
		t.Errorf("nonces expired/live = %d/%d, want partial expiry and 1 live", expired, fresh)
	}
	if err := e.msgs.SweepAttachments(t.Context()); err != nil {
		t.Fatalf("second SweepAttachments: %v", err)
	}
	if err := e.pool.QueryRow(t.Context(), "SELECT count(*) FROM message_nonces WHERE expires_at <= now()").Scan(&expired); err != nil {
		t.Fatalf("count remaining expired nonces: %v", err)
	}
	if expired != 0 {
		t.Errorf("remaining expired nonces = %d, want 0", expired)
	}
}
