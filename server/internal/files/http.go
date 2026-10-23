package files

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/upload"
)

// Mount registers the public image routes on r:
//
//	GET /files/avatars/{owner_id}/{hash}.png, GET /files/icons/{owner_id}/{hash}.png
//
// They need no session: the URLs are unguessable only by convention and the
// images are not secret.
func Mount(r chi.Router, d *Disk) {
	r.Get("/files/avatars/{owner_id}/{file}", httpx.Wrap(d.serve(KindAvatar)))
	r.Get("/files/icons/{owner_id}/{file}", httpx.Wrap(d.serve(KindIcon)))
}

// parseTarget validates the raw path segments: a canonical decimal snowflake
// and "<16 lower-case hex>.png". chi hands over the still-escaped segments,
// so "%2F" and friends simply fail the check.
func parseTarget(rawOwner, rawFile string) (snowflake.ID, string, bool) {
	owner, err := snowflake.Parse(rawOwner)
	if err != nil || owner <= 0 || owner.String() != rawOwner {
		return 0, "", false
	}
	hash, ok := strings.CutSuffix(rawFile, ".png")
	if !ok || !ValidHash(hash) {
		return 0, "", false
	}
	return owner, hash, true
}

func (d *Disk) serve(kind Kind) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		owner, hash, ok := parseTarget(chi.URLParam(r, "owner_id"), chi.URLParam(r, "file"))
		if !ok {
			return httpx.NotFound()
		}
		f, err := d.open(kind, owner, hash)
		if errors.Is(err, os.ErrNotExist) {
			return httpx.NotFound()
		}
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		st, err := f.Stat()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return httpx.NotFound()
		}

		h := w.Header()
		h.Set("Content-Type", "image/png")
		h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
		w.WriteHeader(http.StatusOK)
		// The status is out; a failed copy can only cut the response short.
		_, _ = io.Copy(w, f)
		return nil
	}
}

// busyRetryAfter is the Retry-After of 503 server_busy.
const busyRetryAfter = 5 * time.Second

// ToProblem maps the errors of SaveImage to their HTTP problem
// (unsupported_image, file_too_large, server_busy); any other error is
// returned unchanged.
func ToProblem(err error) error {
	switch {
	case errors.Is(err, ErrUnsupportedImage):
		return httpx.BadRequest("unsupported_image", fmt.Sprintf(
			"the file must be a PNG, JPEG, WebP or GIF image of at most %d pixels on a side", MaxDimension))
	case errors.Is(err, ErrTooLarge):
		return upload.TooLarge(MaxImageBytes)
	case errors.Is(err, ErrBusy):
		return httpx.ServiceUnavailableRetryAfter("server_busy", busyRetryAfter)
	}
	return err
}
