package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

// MaxBodyBytes is the largest JSON request body Decode accepts.
const MaxBodyBytes = 64 << 10

// Decode reads a single JSON object of type T from the request body. It
// requires Content-Type: application/json (415 unsupported_media_type),
// rejects bodies over MaxBodyBytes (400 body_too_large) and unknown fields,
// malformed or trailing data (400 invalid_body). Errors are *Problem.
func Decode[T any](r *http.Request) (T, error) {
	var v T
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return v, UnsupportedMediaType("Content-Type must be application/json")
	}

	body := http.MaxBytesReader(nil, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, decodeError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err != nil {
			if p := tooLarge(err); p != nil {
				return v, p
			}
		}
		return v, BadRequest("invalid_body", "request body must contain a single JSON object")
	}
	return v, nil
}

func decodeError(err error) *Problem {
	if p := tooLarge(err); p != nil {
		return p
	}
	if errors.Is(err, io.EOF) {
		return BadRequest("invalid_body", "request body is empty")
	}
	// The decoder's message names the offending field or position; it holds
	// no server internals, but keep the response generic anyway.
	return BadRequest("invalid_body", "request body is not valid JSON for this endpoint")
}

func tooLarge(err error) *Problem {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return BadRequest("body_too_large", "request body exceeds 64 KiB")
	}
	return nil
}
