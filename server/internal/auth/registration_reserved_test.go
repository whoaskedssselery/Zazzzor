package auth_test

import (
	"bytes"
	"net/http"
	"testing"
)

func TestRegisterReservedNameLooksTaken(t *testing.T) {
	c := newEnv(t).client()
	const path = "/api/v1/auth/register"
	register(t, c, "alice", "password123")
	taken := c.Do(http.MethodPost, path, creds("alice", "password123"))
	reserved := c.Do(http.MethodPost, path, creds("BOSS", "password123"))
	if reserved.Status != taken.Status || !bytes.Equal(reserved.Body, taken.Body) {
		t.Fatalf("reserved = %d %s, want %d %s", reserved.Status, reserved.Body, taken.Status, taken.Body)
	}
}
