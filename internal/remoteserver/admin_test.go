package remoteserver

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/identityservice"
	"github.com/flexdinesh/diffx/internal/testsupport"
)

func TestAdminRoutesRequireAdminBrowserSession(t *testing.T) {
	store, handler := identityHandler(t, Settings{})
	admin := testsupport.RemoteAdmin(t, store, "alice", testPassword)
	cookie, csrf := loginIdentity(t, handler, "alice")
	memberCookie, memberCSRF := memberSession(t, store, "member")
	adminToken, err := identityservice.New(store).CreateToken(t.Context(), admin.Account.ID, "api", identityservice.PurposeAPI, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct {
		cookie       *http.Cookie
		csrf, bearer string
	}{{nil, "", ""}, {memberCookie, memberCSRF, ""}, {nil, "", "Bearer " + adminToken.Secret}} {
		w := identityRequest(handler, "GET", "/api/v2/admin/users", "", request.cookie, request.csrf, request.bearer)
		if w.Code != 401 && w.Code != 403 {
			t.Fatalf("non-admin listed users: %d", w.Code)
		}
	}
	w := identityRequest(handler, "GET", "/api/v2/admin/users", "", cookie, "", "")
	var listed struct {
		Users []adminUserJSON `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || w.Code != 200 || len(listed.Users) != 2 {
		t.Fatalf("list users: %d %s %v", w.Code, w.Body.String(), err)
	}
	var memberID string
	for _, user := range listed.Users {
		if user.Username == "member" {
			memberID = user.ID
		}
		if user.Email != nil || user.CreatedAt <= 0 || user.Admin != (user.ID == admin.Account.ID) {
			t.Fatalf("user projection: %+v", user)
		}
	}
	if w := identityRequest(handler, "POST", "/api/v2/admin/users/"+memberID+"/disable", "", cookie, "", ""); w.Code != 403 {
		t.Fatalf("disable without csrf: %d", w.Code)
	}
	for _, path := range []string{"/api/v2/admin/users/" + admin.Account.ID + "/disable", "/api/v2/admin/users/" + admin.Account.ID} {
		method := "POST"
		if path == "/api/v2/admin/users/"+admin.Account.ID {
			method = "DELETE"
		}
		if w := identityRequest(handler, method, path, "", cookie, csrf, ""); w.Code != 403 {
			t.Fatalf("admin changed itself %s: %d", path, w.Code)
		}
	}
	for _, test := range []struct {
		method, path string
		want         int
	}{{"POST", "/api/v2/admin/users/missing/disable", 404}, {"GET", "/api/v2/admin/users/" + memberID + "/disable", 405}, {"POST", "/api/v2/admin/users/" + memberID + "/promote", 404}, {"POST", "/api/v2/admin/settings", 404}} {
		if w := identityRequest(handler, test.method, test.path, "", cookie, csrf, ""); w.Code != test.want {
			t.Fatalf("%s %s: %d want %d", test.method, test.path, w.Code, test.want)
		}
	}

	server := httptest.NewServer(handler)
	defer server.Close()
	r, err := http.NewRequestWithContext(t.Context(), "GET", server.URL+"/api/v2/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.AddCookie(memberCookie)
	stream, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.StatusCode != 200 {
		t.Fatalf("member stream: %d", stream.StatusCode)
	}
	if w := identityRequest(handler, "POST", "/api/v2/admin/users/"+memberID+"/disable", "", cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("disable: %d %s", w.Code, w.Body.String())
	}
	closed := make(chan error, 1)
	go func() { _, err := io.Copy(&bytes.Buffer{}, stream.Body); closed <- err }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("disabled member kept stream open")
	}
	if w := identityRequest(handler, "GET", "/api/v2/contexts", "", memberCookie, "", ""); w.Code != 401 {
		t.Fatalf("disabled member session: %d", w.Code)
	}
	if w := identityRequest(handler, "POST", "/api/v2/admin/users/"+memberID+"/enable", "", cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("enable: %d", w.Code)
	}
	if w := identityRequest(handler, "DELETE", "/api/v2/admin/users/"+memberID, "", cookie, csrf, ""); w.Code != 204 {
		t.Fatalf("delete: %d", w.Code)
	}
	w = identityRequest(handler, "GET", "/api/v2/admin/users", "", cookie, "", "")
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil || len(listed.Users) != 1 {
		t.Fatalf("deleted member listed: %s", w.Body.String())
	}
}
