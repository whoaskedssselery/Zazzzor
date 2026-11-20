package channels_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/permissions"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/snowflake"
)

func TestEmptyChannelLastMessageIDIsNull(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("owner")
	g := createGuild(t, owner, "Guild")
	ch := createChannel(t, owner, g, map[string]any{"name": "general", "type": 0})
	resp := owner.Do(http.MethodGet, "/api/v1/channels/"+ch.ID, nil)
	expect(t, resp, http.StatusOK)
	var body map[string]json.RawMessage
	resp.JSON(&body)
	if got := string(body["last_message_id"]); got != "null" {
		t.Fatalf("last_message_id = %q, want null", got)
	}
}

func TestJoiningStartsReadAtVisibleChannelHead(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("owner")
	_, member := e.user("member")
	g := createGuild(t, owner, "Guild")
	ch := createChannel(t, owner, g, map[string]any{"name": "general", "type": 0})
	const head = 42
	if _, err := e.pool.Exec(t.Context(), "UPDATE channels SET last_message_id = $1 WHERE id = $2", head, ch.id(t)); err != nil {
		t.Fatalf("set channel head: %v", err)
	}
	e.join(g, member)
	var got snowflake.ID
	if err := e.pool.QueryRow(t.Context(), "SELECT last_read_id FROM read_states WHERE user_id = $1 AND channel_id = $2", member.id(t), ch.id(t)).Scan(&got); err != nil {
		t.Fatalf("load read state: %v", err)
	}
	if got != head {
		t.Fatalf("last_read_id = %v, want %v", got, head)
	}
}

func TestJoiningSkipsHiddenChannelReadState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("owner")
	_, member := e.user("member")
	g := createGuild(t, owner, "Guild")
	ch := createChannel(t, owner, g, map[string]any{"name": "general", "type": 0})
	expect(t, putOverwrite(owner, ch.ID, g.ID, 0, 0, permissions.ViewChannel), http.StatusOK)
	e.join(g, member)
	assertNoReadState(t, e, member.id(t), ch.id(t))
}

func TestLosingChannelVisibilityDropsReadState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("owner")
	_, member := e.user("member")
	g := createGuild(t, owner, "Guild")
	ch := createChannel(t, owner, g, map[string]any{"name": "general", "type": 0})
	e.join(g, member)
	insertReadState(t, e, g, ch, member)
	expect(t, putOverwrite(owner, ch.ID, g.ID, 0, 0, permissions.ViewChannel), http.StatusOK)
	assertNoReadState(t, e, member.id(t), ch.id(t))
}

func assertNoReadState(t *testing.T, e *env, userID, channelID snowflake.ID) {
	t.Helper()
	var exists bool
	if err := e.pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM read_states WHERE user_id = $1 AND channel_id = $2)", userID, channelID).Scan(&exists); err != nil {
		t.Fatalf("check read state: %v", err)
	}
	if exists {
		t.Fatalf("read state exists = %t, want false", exists)
	}
}

func TestLosingViewThroughRolePermissionsDropsReadState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("owner")
	_, member := e.user("member")
	g := createGuild(t, owner, "Guild")
	ch := createChannel(t, owner, g, map[string]any{"name": "general", "type": 0})
	e.join(g, member)
	insertReadState(t, e, g, ch, member)
	resp := owner.Do(http.MethodPatch, "/api/v1/guilds/"+g.ID+"/roles/"+g.ID,
		map[string]any{"permissions": perm(permissions.DefaultEveryone &^ permissions.ViewChannel)})
	expect(t, resp, http.StatusOK)
	assertNoReadState(t, e, member.id(t), ch.id(t))
}

func TestSyncingWithHiddenCategoryDropsReadState(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	owner, _ := e.user("owner")
	_, member := e.user("member")
	g := createGuild(t, owner, "Guild")
	cat := createChannel(t, owner, g, map[string]any{"name": "private", "type": 4})
	ch := createChannel(t, owner, g, map[string]any{"name": "room", "type": 0, "parent_id": cat.ID})
	e.join(g, member)
	expect(t, putOverwrite(owner, ch.ID, member.ID, memberTarget, permissions.ViewChannel, 0), http.StatusOK)
	expect(t, putOverwrite(owner, cat.ID, g.ID, roleTarget, 0, permissions.ViewChannel), http.StatusOK)
	insertReadState(t, e, g, ch, member)
	expect(t, owner.Do(http.MethodPatch, "/api/v1/channels/"+ch.ID, map[string]any{"sync_permissions": true}), http.StatusOK)
	assertNoReadState(t, e, member.id(t), ch.id(t))
}

func TestLosingRoleVisibilityDropsReadState(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"member role removal", "role deletion"} {
		t.Run(change, func(t *testing.T) {
			e := newEnv(t)
			owner, _ := e.user("owner")
			_, member := e.user("member")
			g := createGuild(t, owner, "Guild")
			ch := createChannel(t, owner, g, map[string]any{"name": "room", "type": 0})
			role := createRole(t, owner, g, "reader", 0)
			expect(t, putOverwrite(owner, ch.ID, g.ID, roleTarget, 0, permissions.ViewChannel), http.StatusOK)
			expect(t, putOverwrite(owner, ch.ID, role, roleTarget, permissions.ViewChannel, 0), http.StatusOK)
			e.join(g, member)
			setRoles(t, owner, g, member.ID, role)
			insertReadState(t, e, g, ch, member)

			if change == "member role removal" {
				setRoles(t, owner, g, member.ID)
			} else {
				expect(t, owner.Do(http.MethodDelete, "/api/v1/guilds/"+g.ID+"/roles/"+role, nil), http.StatusNoContent)
			}
			assertNoReadState(t, e, member.id(t), ch.id(t))
		})
	}
}

func insertReadState(t *testing.T, e *env, g guildBody, ch channelBody, member userBody) {
	t.Helper()
	if _, err := e.pool.Exec(t.Context(), "INSERT INTO read_states (user_id, channel_id, guild_id, last_read_id, mention_count) VALUES ($1, $2, $3, 0, 0) ON CONFLICT DO NOTHING", member.id(t), ch.id(t), g.id(t)); err != nil {
		t.Fatalf("create read state: %v", err)
	}
}
