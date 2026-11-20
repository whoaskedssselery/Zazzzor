package gateway_test

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"testing"

	"github.com/whoaskedssselery/zazzzor/server/internal/files"
	"github.com/whoaskedssselery/zazzzor/server/internal/gateway"
	"github.com/whoaskedssselery/zazzzor/server/internal/platform/testkit"
)

func createDMGroup(t *testing.T, c *testkit.Client, ids ...string) privateChannelBody {
	t.Helper()
	r := c.Do(http.MethodPost, privateChannels, map[string][]string{"recipient_ids": ids})
	dmExpect(t, r, http.StatusCreated)
	var out privateChannelBody
	r.JSON(&out)
	return out
}

func putDMFile(t *testing.T, c *testkit.Client, ch string) (id, url string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err = part.Write([]byte("private file")); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err = writer.Close(); err != nil {
		t.Fatalf("close upload: %v", err)
	}
	r := c.Do(http.MethodPost, "/api/v1/channels/"+ch+"/attachments", body.Bytes(), testkit.WithHeader("Content-Type", writer.FormDataContentType()))
	dmExpect(t, r, http.StatusCreated)
	var a struct{ ID, URL string }
	r.JSON(&a)
	return a.ID, a.URL
}

func TestGroupDMOwnersAndFiles(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	carol, c := e.user("carol")
	makeFriends(t, alice, bob, a, b)
	denied := alice.Do(http.MethodPost, privateChannels, map[string][]string{"recipient_ids": {b.ID, c.ID}})
	dmExpect(t, denied, http.StatusBadRequest)
	if dmCode(denied) != "not_a_friend" {
		t.Fatalf("non-friend denial = %s", denied.Body)
	}
	makeFriends(t, alice, carol, a, c)
	group := createDMGroup(t, alice, b.ID, c.ID)
	path := "/api/v1/channels/" + group.ID
	if group.Type != 3 || group.OwnerID == nil || *group.OwnerID != a.ID || group.Name != nil || len(group.Recipients) != 2 {
		t.Fatalf("group = %+v, want unnamed group owned by Alice", group)
	}
	named := bob.Do(http.MethodPatch, path, map[string]string{"name": "  Friday  "})
	dmExpect(t, named, http.StatusOK)
	named.JSON(&group)
	if group.Name == nil || *group.Name != "Friday" {
		t.Fatalf("name = %v, want Friday", group.Name)
	}
	named = bob.Do(http.MethodPatch, path, map[string]any{"name": nil})
	dmExpect(t, named, http.StatusOK)
	named.JSON(&group)
	if group.Name != nil {
		t.Fatalf("name = %v, want nil", group.Name)
	}
	dmExpect(t, bob.Do(http.MethodDelete, path+"/recipients/"+c.ID, nil), http.StatusForbidden)
	dmExpect(t, alice.Do(http.MethodDelete, path, nil), http.StatusNoContent)
	response := bob.Do(http.MethodGet, path, nil)
	dmExpect(t, response, http.StatusOK)
	response.JSON(&group)
	if group.OwnerID == nil || *group.OwnerID != b.ID {
		t.Fatalf("owner after leave = %v, want Bob", group.OwnerID)
	}
	dmExpect(t, bob.Do(http.MethodDelete, path+"/recipients/"+c.ID, nil), http.StatusNoContent)
	dmExpect(t, carol.Do(http.MethodGet, path+"/messages", nil), http.StatusNotFound)
	id, url := putDMFile(t, bob, group.ID)
	dmExpect(t, bob.Do(http.MethodPost, path+"/messages", map[string]any{"attachment_ids": []string{id}}), http.StatusCreated)
	dmExpect(t, bob.Do(http.MethodGet, url, nil), http.StatusOK)
	dmExpect(t, carol.Do(http.MethodGet, url, nil), http.StatusNotFound)
	dmExpect(t, bob.Do(http.MethodDelete, path, nil), http.StatusNoContent)
	if err := e.m.Messages.SweepAttachments(t.Context()); err != nil {
		t.Fatalf("sweep deleted channel: %v", err)
	}
	f, err := e.m.Files.OpenAttachment(files.AttachmentReference(parseID(t, group.ID), parseID(t, id)))
	if f != nil {
		_ = f.Close()
	}
	if !os.IsNotExist(err) {
		t.Errorf("deleted file open = %v, want missing", err)
	}
}

func TestGroupDMCapacity(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	carol, c := e.user("carol")
	makeFriends(t, alice, bob, a, b)
	makeFriends(t, alice, carol, a, c)
	group := createDMGroup(t, alice, b.ID, c.ID)
	path := "/api/v1/channels/" + group.ID + "/recipients/"
	if _, err := e.pool.Exec(t.Context(), `
  INSERT INTO users(id,username,display_name,password_hash,created_at)
  SELECT i,'friend'||i,'Friend','unused',now() FROM generate_series(1000,1007) AS i
 `); err != nil {
		t.Fatalf("seed friends: %v", err)
	}
	if _, err := e.pool.Exec(t.Context(), `
  INSERT INTO relationships(user_id,other_id,type,since)
  SELECT $1::bigint,id,1,now() FROM users WHERE id BETWEEN 1000 AND 1007
  UNION ALL SELECT id,$1::bigint,1,now() FROM users WHERE id BETWEEN 1000 AND 1007
 `, int64(a.id(t))); err != nil {
		t.Fatalf("seed friendship: %v", err)
	}
	for id := 1000; id < 1007; id++ {
		dmExpect(t, alice.Do(http.MethodPut, path+fmt.Sprint(id), nil), http.StatusNoContent)
	}
	dmExpect(t, alice.Do(http.MethodPut, path+"1000", nil), http.StatusNoContent)
	refused := alice.Do(http.MethodPut, path+"1007", nil)
	dmExpect(t, refused, http.StatusBadRequest)
	if dmCode(refused) != "group_full" {
		t.Fatalf("capacity error = %s", refused.Body)
	}
}

func TestGroupDMCreationLimits(t *testing.T) {
	t.Parallel()
	e := newEnv(t, gateway.Options{})
	alice, a := e.user("alice")
	bob, b := e.user("bob")
	carol, c := e.user("carol")
	makeFriends(t, alice, bob, a, b)
	makeFriends(t, alice, carol, a, c)
	if _, err := e.pool.Exec(t.Context(), `
  INSERT INTO channels(id,type,owner_id,position,permission_synced,created_at)
  SELECT i,3,$1::bigint,0,false,now() FROM generate_series(2000,2099) AS i
 `, int64(b.id(t))); err != nil {
		t.Fatalf("seed groups: %v", err)
	}
	if _, err := e.pool.Exec(t.Context(), `
  INSERT INTO channel_recipients(channel_id,user_id,joined_at)
  SELECT id,$1::bigint,now() FROM channels WHERE id BETWEEN 2000 AND 2099
 `, int64(b.id(t))); err != nil {
		t.Fatalf("seed recipients: %v", err)
	}
	refused := alice.Do(http.MethodPost, privateChannels, map[string][]string{"recipient_ids": {b.ID, c.ID}})
	dmExpect(t, refused, http.StatusBadRequest)
	if dmCode(refused) != "too_many_groups" {
		t.Fatalf("group quota = %s", refused.Body)
	}
	if _, err := e.pool.Exec(t.Context(), "DELETE FROM channels WHERE id BETWEEN 2000 AND 2099"); err != nil {
		t.Fatalf("remove seed groups: %v", err)
	}
	// The refused request took one slot.
	for range 9 {
		createDMGroup(t, alice, b.ID, c.ID)
	}
	refused = alice.Do(http.MethodPost, privateChannels, map[string][]string{"recipient_ids": {b.ID, c.ID}})
	dmExpect(t, refused, http.StatusTooManyRequests)
}
