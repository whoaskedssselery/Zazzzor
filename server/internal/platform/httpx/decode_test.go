package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

type loginReq struct {
	Username string `json:"username"`
}

func decodeHandler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return httpx.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		v, err := httpx.Decode[loginReq](r)
		if err != nil {
			return err
		}
		httpx.JSON(w, http.StatusOK, v)
		return nil
	})
}

func postJSON(body, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r
}

func TestDecodeRejectsUnknownFieldsAndBigBodies(t *testing.T) {
	big := `{"username":"` + strings.Repeat("a", 65*1024) + `"}`
	cases := []struct {
		name, body, ct string
		status         int
		code           string
	}{
		{"unknown field", `{"username":"a","admin":true}`, "application/json", http.StatusBadRequest, "invalid_body"},
		{"too large", big, "application/json", http.StatusBadRequest, "body_too_large"},
		{"no content type", `{"username":"a"}`, "", http.StatusUnsupportedMediaType, "unsupported_media_type"},
		{"wrong content type", `{"username":"a"}`, "text/plain", http.StatusUnsupportedMediaType, "unsupported_media_type"},
		{"malformed", `{"username":`, "application/json", http.StatusBadRequest, "invalid_body"},
		{"trailing data", `{"username":"a"}{}`, "application/json", http.StatusBadRequest, "invalid_body"},
		{"empty", ``, "application/json", http.StatusBadRequest, "invalid_body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			decodeHandler(t).ServeHTTP(rec, postJSON(tc.body, tc.ct))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.status, rec.Body.String())
			}
			if p := decodeProblem(t, rec); p.Code != tc.code {
				t.Errorf("code = %q, want %q", p.Code, tc.code)
			}
		})
	}
}

func TestDecodeAcceptsValidBody(t *testing.T) {
	rec := httptest.NewRecorder()
	decodeHandler(t).ServeHTTP(rec, postJSON(`{"username":"bob"}`, "application/json; charset=utf-8"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"username":"bob"}` {
		t.Errorf("body = %s", got)
	}
}
