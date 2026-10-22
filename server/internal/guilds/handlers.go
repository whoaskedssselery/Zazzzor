package guilds

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/whoaskedssselery/zazzzor/server/internal/auth"
	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/httpx"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/ratelimit"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/upload"
)

// Paging of GET /guilds/{id}/members.
const (
	defaultMembersLimit = 100
	maxMembersLimit     = 1000
)

// Mount registers the guild endpoints on r, which must already run
// auth.Middleware:
//
//	POST /guilds; GET /users/@me/guilds; DELETE /users/@me/guilds/{guild_id}
//	GET, PATCH, DELETE /guilds/{guild_id}; PUT /guilds/{guild_id}/icon
//	GET /guilds/{guild_id}/members; PATCH /guilds/{guild_id}/members/{user_id}
//	GET, POST, PATCH /guilds/{guild_id}/roles; PATCH, DELETE /guilds/{guild_id}/roles/{role_id}
func Mount(r chi.Router, s *Service) {
	r.Group(func(r chi.Router) {
		r.Use(auth.Require, noStore)
		writes := r.With(limit(s.writeLimiter))

		r.Post("/guilds", httpx.Wrap(s.handleCreate))
		r.Get("/users/@me/guilds", httpx.Wrap(s.handleList))
		writes.Delete("/users/@me/guilds/{guild_id}", httpx.Wrap(s.handleLeave))

		r.Get("/guilds/{guild_id}", httpx.Wrap(s.handleGet))
		writes.Patch("/guilds/{guild_id}", httpx.Wrap(s.handlePatch))
		writes.Delete("/guilds/{guild_id}", httpx.Wrap(s.handleDelete))
		writes.Put("/guilds/{guild_id}/icon", httpx.Wrap(s.handlePutIcon))

		r.Get("/guilds/{guild_id}/members", httpx.Wrap(s.handleMembers))
		writes.Patch("/guilds/{guild_id}/members/{user_id}", httpx.Wrap(s.handlePatchMember))

		r.Get("/guilds/{guild_id}/roles", httpx.Wrap(s.handleRoles))
		writes.Post("/guilds/{guild_id}/roles", httpx.Wrap(s.handleCreateRole))
		writes.Patch("/guilds/{guild_id}/roles", httpx.Wrap(s.handleReorderRoles))
		writes.Patch("/guilds/{guild_id}/roles/{role_id}", httpx.Wrap(s.handlePatchRole))
		writes.Delete("/guilds/{guild_id}/roles/{role_id}", httpx.Wrap(s.handleDeleteRole))
	})
}

// limit charges l per user before any of the body is read.
func limit(l *ratelimit.Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ok, retry := l.Allow(currentUser(r).ID.String()); !ok {
				httpx.WriteProblem(w, httpx.TooManyRequests(retry))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
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

// pathID parses a canonical snowflake path parameter; anything else is 404.
func pathID(r *http.Request, name string) (snowflake.ID, error) {
	raw := chi.URLParam(r, name)
	id, err := snowflake.Parse(raw)
	if err != nil || id <= 0 || id.String() != raw {
		return 0, httpx.NotFound()
	}
	return id, nil
}

func fail(err error) error { return auth.ToProblem(ToProblem(err)) }

type createGuildRequest struct {
	Name string `json:"name"`
}

func (s *Service) handleCreate(w http.ResponseWriter, r *http.Request) error {
	req, err := httpx.Decode[createGuildRequest](r)
	if err != nil {
		return err
	}
	snap, err := s.CreateGuild(r.Context(), currentUser(r), req.Name)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusCreated, snap.Guild.Public())
	return nil
}

func (s *Service) handleList(w http.ResponseWriter, r *http.Request) error {
	gs, err := s.GuildsOf(r.Context(), currentUser(r).ID)
	if err != nil {
		return fail(err)
	}
	out := make([]PublicGuild, len(gs))
	for i, g := range gs {
		out[i] = g.Public()
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

func (s *Service) handleLeave(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	if err = s.Leave(r.Context(), id, currentUser(r)); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handleGet(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	g, err := s.Guild(r.Context(), id, currentUser(r).ID)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, g.Public())
	return nil
}

type patchGuildRequest struct {
	Name *string `json:"name"`
}

func (s *Service) handlePatch(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[patchGuildRequest](r)
	if err != nil {
		return err
	}
	actor := currentUser(r).ID
	var g Guild
	if req.Name != nil {
		g, err = s.Rename(r.Context(), id, actor, *req.Name)
	} else {
		// Nothing to change, but the permission still applies.
		if err = s.CheckManageGuild(r.Context(), id, actor); err == nil {
			g, err = s.Guild(r.Context(), id, actor)
		}
	}
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, g.Public())
	return nil
}

func (s *Service) handleDelete(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	if err = s.DeleteGuild(r.Context(), id, currentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Service) handlePutIcon(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	actor := currentUser(r).ID
	// Refuse before reading a large body the actor may not upload.
	if err = s.CheckManageGuild(r.Context(), id, actor); err != nil {
		return fail(err)
	}
	upload.ExtendDeadlines(w)
	data, err := upload.ReadFile(w, r, "file", files.MaxImageBytes)
	if err != nil {
		return err
	}
	g, err := s.SetIcon(r.Context(), id, actor, data)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, g.Public())
	return nil
}

func (s *Service) handleMembers(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	q := r.URL.Query()
	var after snowflake.ID
	if raw := q.Get("after"); raw != "" {
		if after, err = snowflake.Parse(raw); err != nil || after < 0 {
			return fail(invalid("after", "must be a user ID"))
		}
	}
	lim := defaultMembersLimit
	if raw := q.Get("limit"); raw != "" {
		if lim, err = strconv.Atoi(raw); err != nil || lim < 1 || lim > maxMembersLimit {
			return fail(invalid("limit", "must be an integer from 1 to 1000"))
		}
	}
	ms, err := s.Members(r.Context(), id, currentUser(r).ID, after, lim)
	if err != nil {
		return fail(err)
	}
	out := make([]PublicMember, len(ms))
	for i, m := range ms {
		out[i] = m.Public()
	}
	httpx.JSON(w, http.StatusOK, out)
	return nil
}

// patchMemberRequest keeps nick raw to tell absent from null.
type patchMemberRequest struct {
	Nick  json.RawMessage `json:"nick"`
	Roles *[]snowflake.ID `json:"roles"`
}

func (s *Service) handlePatchMember(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	target, err := pathID(r, "user_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[patchMemberRequest](r)
	if err != nil {
		return err
	}
	f := MemberFields{Roles: req.Roles}
	if req.Nick != nil {
		var nick *string
		if !bytes.Equal(req.Nick, []byte("null")) {
			var n string
			if json.Unmarshal(req.Nick, &n) != nil {
				return fail(invalid("nick", "must be a string or null"))
			}
			nick = &n
		}
		f.Nick = &nick
	}
	m, err := s.UpdateMember(r.Context(), id, target, currentUser(r).ID, f)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, m.Public())
	return nil
}

func publicRoles(rs []Role) []PublicRole {
	out := make([]PublicRole, len(rs))
	for i, r := range rs {
		out[i] = r.Public()
	}
	return out
}

func (s *Service) handleRoles(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	rs, err := s.Roles(r.Context(), id, currentUser(r).ID)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, publicRoles(rs))
	return nil
}

type roleRequest struct {
	Name        *string                 `json:"name"`
	Permissions *permissions.Permission `json:"permissions"`
	Color       *int                    `json:"color"`
	Hoist       *bool                   `json:"hoist"`
	Mentionable *bool                   `json:"mentionable"`
}

func (q roleRequest) fields() RoleFields {
	return RoleFields(q)
}

func (s *Service) handleCreateRole(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[roleRequest](r)
	if err != nil {
		return err
	}
	role, err := s.CreateRole(r.Context(), id, currentUser(r).ID, req.fields())
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusCreated, role.Public())
	return nil
}

func (s *Service) handlePatchRole(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	roleID, err := pathID(r, "role_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[roleRequest](r)
	if err != nil {
		return err
	}
	role, err := s.UpdateRole(r.Context(), id, roleID, currentUser(r).ID, req.fields())
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, role.Public())
	return nil
}

type rolePositionRequest struct {
	ID       snowflake.ID `json:"id"`
	Position int          `json:"position"`
}

func (s *Service) handleReorderRoles(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	req, err := httpx.Decode[[]rolePositionRequest](r)
	if err != nil {
		return err
	}
	moves := make([]RolePosition, len(req))
	for i, m := range req {
		moves[i] = RolePosition(m)
	}
	rs, err := s.ReorderRoles(r.Context(), id, currentUser(r).ID, moves)
	if err != nil {
		return fail(err)
	}
	httpx.JSON(w, http.StatusOK, publicRoles(rs))
	return nil
}

func (s *Service) handleDeleteRole(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r, "guild_id")
	if err != nil {
		return err
	}
	roleID, err := pathID(r, "role_id")
	if err != nil {
		return err
	}
	if err = s.DeleteRole(r.Context(), id, roleID, currentUser(r).ID); err != nil {
		return fail(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
