package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

// argonParams are the tunable Argon2id parameters stored in a PHC string.
type argonParams struct {
	memory  uint32 // KiB
	time    uint32
	threads uint8
}

// productionParams are the server's cost of a new hash.
var productionParams = argonParams{memory: 64 * 1024, time: 3, threads: 2}

// testParams are a test binary's: its tests register users by the hundred,
// and a hash stores its own parameters, so verifying works the same way.
var testParams = argonParams{memory: 8 * 1024, time: 1, threads: 1}

// currentParams hash every new password; hashes made with other parameters
// are upgraded on the next successful login.
var currentParams = func() argonParams {
	if testing.Testing() {
		return testParams
	}
	return productionParams
}()

const (
	saltLen = 16
	keyLen  = 32

	// Upper bounds for parameters read back from storage, so a corrupted
	// row cannot make a login allocate gigabytes.
	maxMemory  = 256 * 1024
	maxTime    = 16
	maxThreads = 16

	// maxConcurrentHashes caps simultaneous Argon2id runs (64 MiB each),
	// part of the process memory budget documented in package upload.
	maxConcurrentHashes = 4
)

// hashSlots bounds concurrent hashing.
var hashSlots = make(chan struct{}, maxConcurrentHashes)

// hashWaitTimeout is how long a request waits for a free hashing slot
// before it is turned away with ErrBusy.
var hashWaitTimeout = 3 * time.Second

// ErrBusy means every hashing slot stayed taken for hashWaitTimeout; the
// request is answered 503 server_busy instead of queueing without bound.
var ErrBusy = errors.New("auth: password hashing is saturated")

// argonKey runs Argon2id in one of the hashing slots. It never hashes for a
// request whose context is done, and gives up with ErrBusy when no slot
// frees up in time.
func argonKey(ctx context.Context, pw string, salt []byte, p argonParams, n uint32) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timer := time.NewTimer(hashWaitTimeout)
	defer timer.Stop()
	select {
	case hashSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, ErrBusy
	}
	defer func() { <-hashSlots }()
	// The context may have ended while this call won the slot.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return argon2.IDKey([]byte(pw), salt, p.time, p.memory, p.threads, n), nil
}

// errMalformedHash is returned for stored hashes that are not a PHC string
// this package can verify.
var errMalformedHash = errors.New("auth: malformed password hash")

// HashPassword returns the Argon2id PHC string of pw with a fresh random salt:
// $argon2id$v=19$m=65536,t=3,p=2$<salt>$<key> (unpadded standard base64).
func HashPassword(ctx context.Context, pw string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: salt: %w", err)
	}
	key, err := argonKey(ctx, pw, salt, currentParams, keyLen)
	if err != nil {
		return "", err
	}
	return encodePHC(currentParams, salt, key), nil
}

func encodePHC(p argonParams, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.memory, p.time, p.threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

// VerifyPassword reports whether pw matches encoded, comparing in constant
// time. needsRehash is true when pw matches but encoded was made with
// parameters other than the current ones. err is non-nil for a malformed
// encoded string, a done ctx or ErrBusy.
func VerifyPassword(ctx context.Context, pw, encoded string) (ok, needsRehash bool, err error) {
	h, err := parsePHC(encoded)
	if err != nil {
		return false, false, err
	}
	key, err := argonKey(ctx, pw, h.salt, h.params, uint32(len(h.key))) //nolint:gosec // len checked in parsePHC
	if err != nil {
		return false, false, err
	}
	if subtle.ConstantTimeCompare(key, h.key) != 1 {
		return false, false, nil
	}
	rehash := h.params != currentParams || len(h.salt) != saltLen || len(h.key) != keyLen
	return true, rehash, nil
}

type phc struct {
	params    argonParams
	salt, key []byte
}

func parsePHC(s string) (phc, error) {
	// "", "argon2id", "v=19", "m=..,t=..,p=..", salt, key
	parts := strings.Split(s, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" ||
		parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return phc{}, errMalformedHash
	}
	var h phc
	var seen int
	for kv := range strings.SplitSeq(parts[3], ",") {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			return phc{}, errMalformedHash
		}
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil {
			return phc{}, errMalformedHash
		}
		switch {
		case k == "m" && seen&1 == 0 && n >= 8 && n <= maxMemory:
			h.params.memory, seen = uint32(n), seen|1
		case k == "t" && seen&2 == 0 && n >= 1 && n <= maxTime:
			h.params.time, seen = uint32(n), seen|2
		case k == "p" && seen&4 == 0 && n >= 1 && n <= maxThreads:
			h.params.threads, seen = uint8(n), seen|4
		default:
			return phc{}, errMalformedHash
		}
	}
	if seen != 7 {
		return phc{}, errMalformedHash
	}
	var err error
	if h.salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil || len(h.salt) < 8 {
		return phc{}, errMalformedHash
	}
	if h.key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(h.key) < 16 || len(h.key) > 64 {
		return phc{}, errMalformedHash
	}
	return h, nil
}

// dummyHash is verified when a login names no existing account, so that the
// response takes as long as a real wrong-password check.
var dummyHash = sync.OnceValue(func() string {
	salt := make([]byte, saltLen)
	_, _ = rand.Read(salt)
	key := make([]byte, keyLen)
	_, _ = rand.Read(key)
	return encodePHC(currentParams, salt, key)
})
