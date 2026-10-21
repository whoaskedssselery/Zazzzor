package users

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
)

const (
	// uploadReadTimeout replaces the global 15 s body deadline on upload
	// routes: 4 MiB over a 1 Mbit/s uplink takes about 32 s.
	uploadReadTimeout = 120 * time.Second
	// uploadWriteTimeout leaves time to answer after a slow upload.
	uploadWriteTimeout = uploadReadTimeout + 30*time.Second
	// multipartOverhead is what the form around the file may add.
	multipartOverhead = 64 << 10
	// maxUploadBody caps the whole multipart body.
	maxUploadBody = files.MaxImageBytes + multipartOverhead
	// busyRetryAfter is the Retry-After of 503 server_busy.
	busyRetryAfter = 5 * time.Second
)

// Mount registers the profile endpoints on r, which must already run
// auth.Middleware:
//
//	GET, PATCH /users/@me; POST /users/@me/password; PUT, DELETE /users/@me/avatar
func Mount(r chi.Router, s *Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, noStore)
		r.Get("/users/@me", httpx.Wrap(handleMe))
		r.Patch("/users/@me", httpx.Wrap(s.handlePatchMe))
		r.Post("/users/@me/password", httpx.Wrap(s.handlePassword))
		r.Put("/users/@me/avatar", httpx.Wrap(s.handlePutAvatar))
		r.Delete("/users/@me/avatar", httpx.Wrap(s.handleDeleteAvatar))
	})
}

func noStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) auth.User {
	u, _ := auth.FromContext(r.Context()) // auth.Require ran
	return u
}

func handleMe(w http.ResponseWriter, r *http.Request) error {
	httpx.JSON(w, http.StatusOK, currentUser(r).Public())
	return nil
}

// patchMeRequest keeps the raw field to tell absent from null.
type patchMeRequest struct {
	DisplayName json.RawMessage `json:"display_name"`
}

func fieldError(field, msg string) error {
	return httpx.BadRequest("validation_failed", "some fields are invalid", httpx.FieldError{Field: field, Message: msg})
}

func (s *Service) handlePatchMe(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[patchMeRequest](r)
	if err != nil {
		return err
	}
	u := currentUser(r)
	if req.DisplayName != nil {
		var dn string
		if bytes.Equal(req.DisplayName, []byte("null")) || json.Unmarshal(req.DisplayName, &dn) != nil {
			return fieldError("display_name", "must be a string")
		}
		if u, err = s.UpdateDisplayName(r.Context(), u, dn); err != nil {
			return auth.ToProblem(err)
		}
	}
	httpx.JSON(w, http.StatusOK, u.Public())
	return nil
}

type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Service) handlePassword(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[passwordRequest](r)
	if err != nil {
		return err
	}
	sessionID, _ := auth.SessionFromContext(r.Context())
	err = s.ChangePassword(r.Context(), currentUser(r).ID, sessionID, req.CurrentPassword, req.NewPassword)
	if errors.Is(err, ErrWrongPassword) {
		return fieldError("current_password", "is incorrect")
	}
	if err != nil {
		return auth.ToProblem(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func tooLarge() error {
	return &httpx.Problem{Status: http.StatusRequestEntityTooLarge, Code: "file_too_large",
		Title: http.StatusText(http.StatusRequestEntityTooLarge), Detail: "the file must be at most 4 MiB"}
}

// readUpload returns the bytes of the "file" part of a multipart body of at
// most maxUploadBody bytes; the file itself may be one byte over
// files.MaxImageBytes so that SaveImage can tell it is too large.
func readUpload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/form-data" || params["boundary"] == "" {
		return nil, httpx.UnsupportedMediaType("Content-Type must be multipart/form-data")
	}
	mr := multipart.NewReader(http.MaxBytesReader(w, r.Body, maxUploadBody), params["boundary"])
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, fieldError("file", "is required")
		}
		if err != nil {
			return nil, uploadReadError(err)
		}
		if part.FormName() != "file" {
			continue // NextPart skips the rest of this part
		}
		data, err := io.ReadAll(io.LimitReader(part, files.MaxImageBytes+1))
		if err != nil {
			return nil, uploadReadError(err)
		}
		if len(data) > files.MaxImageBytes {
			return nil, tooLarge()
		}
		return data, nil
	}
}

func uploadReadError(err error) error {
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return tooLarge()
	}
	return httpx.BadRequest("invalid_body", "request body is not valid multipart/form-data")
}

// extendDeadlines gives an upload route more time than the global
// per-request deadlines. Writers without deadline support (tests) are fine.
func extendDeadlines(w http.ResponseWriter) {
	rc := http.NewResponseController(w)
	now := time.Now()
	_ = rc.SetReadDeadline(now.Add(uploadReadTimeout))
	_ = rc.SetWriteDeadline(now.Add(uploadWriteTimeout))
}

func (s *Service) handlePutAvatar(w http.ResponseWriter, r *http.Request) error {
	extendDeadlines(w)
	data, err := readUpload(w, r)
	if err != nil {
		return err
	}
	u, err := s.SetAvatar(r.Context(), currentUser(r).ID, data)
	switch {
	case errors.Is(err, files.ErrUnsupportedImage):
		return httpx.BadRequest("unsupported_image", "the file must be a PNG, JPEG, WebP or GIF image of at most 8192 pixels on a side and 40 megapixels")
	case errors.Is(err, files.ErrTooLarge):
		return tooLarge()
	case errors.Is(err, files.ErrBusy):
		return httpx.ServiceUnavailableRetryAfter("server_busy", busyRetryAfter)
	case err != nil:
		return auth.ToProblem(err)
	}
	httpx.JSON(w, http.StatusOK, u.Public())
	return nil
}

func (s *Service) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) error {
	u, err := s.ClearAvatar(r.Context(), currentUser(r).ID)
	if err != nil {
		return auth.ToProblem(err)
	}
	httpx.JSON(w, http.StatusOK, u.Public())
	return nil
}
