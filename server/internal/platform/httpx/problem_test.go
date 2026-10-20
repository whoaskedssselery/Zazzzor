package httpx_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

type problemBody struct {
	Type   string             `json:"type"`
	Title  string             `json:"title"`
	Status int                `json:"status"`
	Detail string             `json:"detail"`
	Code   string             `json:"code"`
	Errors []httpx.FieldError `json:"errors"`
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) problemBody {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
	var p problemBody
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem body %q: %v", rec.Body.String(), err)
	}
	return p
}

func TestWrapWritesProblemJSON(t *testing.T) {
	h := httpx.Wrap(func(http.ResponseWriter, *http.Request) error {
		return httpx.Forbidden("missing_permission")
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p.Code != "missing_permission" {
		t.Errorf("code = %q, want missing_permission", p.Code)
	}
	if p.Type != "urn:zazzzor:error:missing_permission" {
		t.Errorf("type = %q", p.Type)
	}
	if p.Status != http.StatusForbidden || p.Title == "" {
		t.Errorf("status/title = %d/%q", p.Status, p.Title)
	}
}

func TestWrapUnwrapsWrappedProblem(t *testing.T) {
	h := httpx.Wrap(func(http.ResponseWriter, *http.Request) error {
		return errors.Join(errors.New("ctx"), httpx.NotFound())
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if p := decodeProblem(t, rec); p.Code != "not_found" {
		t.Errorf("code = %q", p.Code)
	}
}

func TestWrapHidesInternalErrors(t *testing.T) {
	const internal = "storage: connection reset by peer 10.0.0.7:5432"
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	h := httpx.Wrap(func(http.ResponseWriter, *http.Request) error {
		return errors.New(internal)
	})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(httpx.WithLogger(req.Context(), logger))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p.Code != "internal" {
		t.Errorf("code = %q, want internal", p.Code)
	}
	if strings.Contains(rec.Body.String(), "connection reset") || strings.Contains(rec.Body.String(), "10.0.0.7") {
		t.Errorf("body leaks internal error: %s", rec.Body.String())
	}
	if !strings.Contains(logs.String(), "connection reset") {
		t.Errorf("internal error was not logged; logs = %q", logs.String())
	}
}

func TestBadRequestCarriesFieldErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteProblem(rec, httpx.BadRequest("validation_failed", "bad input",
		httpx.FieldError{Field: "username", Message: "too short"}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	p := decodeProblem(t, rec)
	if p.Detail != "bad input" || len(p.Errors) != 1 || p.Errors[0].Field != "username" {
		t.Errorf("problem = %+v", p)
	}
}

func TestTooManyRequestsSetsRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.WriteProblem(rec, httpx.TooManyRequests(1500*time.Millisecond))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Errorf("Retry-After = %q, want 2 (rounded up)", got)
	}
	if p := decodeProblem(t, rec); p.Code != "rate_limited" {
		t.Errorf("code = %q", p.Code)
	}
}

func TestJSONWritesBody(t *testing.T) {
	rec := httptest.NewRecorder()
	httpx.JSON(rec, http.StatusCreated, map[string]string{"status": "ok"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"status":"ok"}` {
		t.Errorf("body = %s", got)
	}
}
