package auth_test

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
)

func TestPasswordRoundTrip(t *testing.T) {
	const pw = "correct horse battery staple"
	encoded, err := auth.HashPassword(t.Context(), pw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("encoded = %q, want an argon2id PHC string with m=65536,t=3,p=2", encoded)
	}
	if strings.Contains(encoded, pw) {
		t.Fatal("hash contains the password")
	}
	parts := strings.Split(encoded, "$")
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		t.Fatalf("salt = %d bytes (%v), want 16", len(salt), err)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) != 32 {
		t.Fatalf("key = %d bytes (%v), want 32", len(key), err)
	}

	ok, rehash, err := auth.VerifyPassword(t.Context(), pw, encoded)
	if err != nil || !ok || rehash {
		t.Fatalf("VerifyPassword(right) = %v, %v, %v; want true, false, nil", ok, rehash, err)
	}
	ok, _, err = auth.VerifyPassword(t.Context(), pw+"x", encoded)
	if err != nil || ok {
		t.Fatalf("VerifyPassword(wrong) = %v, %v; want false, nil", ok, err)
	}

	again, err := auth.HashPassword(t.Context(), pw)
	if err != nil {
		t.Fatal(err)
	}
	if again == encoded {
		t.Fatal("two hashes of the same password are equal; salt is not random")
	}
}

func TestVerifyDetectsRehash(t *testing.T) {
	const pw = "old-params-password" //nolint:gosec // test password
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	key := argon2.IDKey([]byte(pw), salt, 2, 19456, 1, 32)
	old := fmt.Sprintf("$argon2id$v=19$m=19456,t=2,p=1$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))

	ok, rehash, err := auth.VerifyPassword(t.Context(), pw, old)
	if err != nil || !ok {
		t.Fatalf("VerifyPassword = %v, %v; want true, nil", ok, err)
	}
	if !rehash {
		t.Fatal("needsRehash = false for m=19456, want true")
	}
	// A wrong password never asks for a rehash.
	if ok, rehash, _ := auth.VerifyPassword(t.Context(), "nope", old); ok || rehash {
		t.Fatalf("wrong password: ok=%v rehash=%v", ok, rehash)
	}
}

func TestVerifyRejectsMalformedHashes(t *testing.T) {
	for _, enc := range []string{
		"",
		"plain",
		"$argon2i$v=19$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0c2FsdA$a2V5",
		"$argon2id$v=18$m=65536,t=3,p=2$c2FsdHNhbHRzYWx0c2FsdA$a2V5",
		"$argon2id$v=19$m=x,t=3,p=2$c2FsdHNhbHRzYWx0c2FsdA$a2V5",
		"$argon2id$v=19$m=65536,t=3,p=2$!!!$a2V5",
		"$argon2id$v=19$m=65536,t=0,p=2$c2FsdHNhbHRzYWx0c2FsdA$a2V5",
		"$argon2id$v=19$m=4294967295,t=3,p=2$c2FsdHNhbHRzYWx0c2FsdA$a2V5",
	} {
		if ok, _, err := auth.VerifyPassword(t.Context(), "pw", enc); ok || err == nil {
			t.Errorf("VerifyPassword(%q) = %v, %v; want false and an error", enc, ok, err)
		}
	}
}
