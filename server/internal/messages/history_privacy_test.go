package messages_test

import (
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
)

// An old reply must not become a way to read the original's later edits
// after history access is revoked, including through a nonce retry.
func TestReplyOperationsRespectRevokedHistory(t *testing.T) {
	for _, action := range []string{"edit", "no-op edit", "nonce retry"} {
		t.Run(action, func(t *testing.T) {
			e := newEnv(t)
			alice, _ := e.user("alice")
			bob, bobUser := e.user("bob")
			g, general, _ := newGuild(t, alice, "home")
			e.join(g, bobUser)
			original := post(t, alice, general.ID, "original")
			resp := send(bob, general.ID, map[string]any{"content": "answer", "reply_to_id": original.ID, "nonce": "reply-1"})
			expect(t, resp, http.StatusCreated)
			var reply messageBody
			resp.JSON(&reply)
			if reply.ReferencedMessage == nil || reply.ReferencedMessage.Content != "original" {
				t.Fatalf("initial reply reference = %+v", reply.ReferencedMessage)
			}

			denyEveryone(t, alice, g, general.ID, permissions.ReadMessageHistory)
			// Bob has no gateway connection: he never receives this live edit.
			expect(t, alice.Do(http.MethodPatch, messagePath(general.ID, original.ID), map[string]any{"content": "new secret"}), http.StatusOK)
			problemOf(t, bob.Do(http.MethodGet, messagesPath(general.ID), nil), http.StatusForbidden, "missing_permission")
			content := "answer"
			switch action {
			case "edit":
				content = "edited answer"
				resp = bob.Do(http.MethodPatch, messagePath(general.ID, reply.ID), map[string]any{"content": content})
			case "no-op edit":
				resp = bob.Do(http.MethodPatch, messagePath(general.ID, reply.ID), map[string]any{"content": content})
			case "nonce retry":
				// Omitting reply_to_id must not bypass the original's history check.
				resp = send(bob, general.ID, map[string]any{"content": content, "nonce": "reply-1"})
			}
			status := http.StatusOK
			if action == "nonce retry" {
				status = http.StatusCreated
			}
			expect(t, resp, status)
			var got messageBody
			resp.JSON(&got)
			if got.ID != reply.ID || got.Content != content || got.ReplyToID == nil || *got.ReplyToID != original.ID {
				t.Fatalf("own reply was not preserved: %+v", got)
			}
			if got.ReferencedMessage != nil {
				t.Errorf("history denied but original returned: %+v", got.ReferencedMessage)
			}
			// The reply link is retained for readers who still have history access.
			for _, m := range list(t, alice, general.ID, "") {
				if m.ID == reply.ID && (m.ReferencedMessage == nil || m.ReferencedMessage.Content != "new secret") {
					t.Fatalf("authorized reader lost the reference: %+v", m)
				}
			}
		})
	}
}
