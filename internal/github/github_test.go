package github

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testKey = func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
}()

const testSecret = "client-secret-value"

func testClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := New(App{ID: 42, ClientID: "Iv1.client", ClientSecret: testSecret, PrivateKey: testKey,
		WebhookSecret: []byte("webhook-secret")}, WithAPIURL(server.URL+"/"), WithWebURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func writeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	writer.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Error(err)
	}
}

func tokenHandler(t *testing.T, mints *atomic.Int32, lifetime time.Duration) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		count := mints.Add(1)
		writeJSON(t, writer, map[string]string{"token": fmt.Sprintf("ghs_%d", count),
			"expires_at": time.Now().Add(lifetime).UTC().Format(time.RFC3339)})
	}
}

func TestParsePrivateKey(t *testing.T) {
	pkcs8, err := x509.MarshalPKCS8PrivateKey(testKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, block := range map[string]*pem.Block{
		"pkcs1": {Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testKey)},
		"pkcs8": {Type: "PRIVATE KEY", Bytes: pkcs8},
	} {
		key, err := ParsePrivateKey(pem.EncodeToMemory(block))
		if err != nil || !key.Equal(testKey) {
			t.Fatalf("%s: key=%v err=%v", name, key != nil, err)
		}
	}
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecPKCS8, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string][]byte{
		"not pem":   []byte("not a key"),
		"bad der":   pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: []byte("junk")}),
		"ec key":    pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecPKCS8}),
		"wrong pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("x")}),
	} {
		if _, err := ParsePrivateKey(input); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestNewValidatesApp(t *testing.T) {
	valid := App{ID: 1, ClientID: "c", ClientSecret: "s", PrivateKey: testKey, WebhookSecret: []byte("w")}
	if _, err := New(valid); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*App){
		"id":      func(app *App) { app.ID = 0 },
		"client":  func(app *App) { app.ClientID = "" },
		"secret":  func(app *App) { app.ClientSecret = "" },
		"key":     func(app *App) { app.PrivateKey = nil },
		"webhook": func(app *App) { app.WebhookSecret = nil },
	}
	for name, mutate := range cases {
		app := valid
		mutate(&app)
		if _, err := New(app); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
	for _, option := range []Option{WithAPIURL("ftp://example.com"), WithWebURL("https://u:p@github.com"), WithHTTPClient(nil)} {
		if _, err := New(valid, option); err == nil {
			t.Fatal("expected option validation error")
		}
	}
}

func TestAppSlugSignsJWT(t *testing.T) {
	var authorization string
	client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/app" || request.Header.Get("Accept") != "application/vnd.github+json" ||
			request.Header.Get("X-GitHub-Api-Version") != "2022-11-28" || request.Header.Get("User-Agent") != "diffx" {
			t.Errorf("unexpected request %s %v", request.URL.Path, request.Header)
		}
		authorization = request.Header.Get("Authorization")
		writeJSON(t, writer, map[string]string{"slug": "diffx-app"})
	}))
	before := time.Now().Unix()
	slug, err := client.AppSlug(context.Background())
	if err != nil || slug != "diffx-app" {
		t.Fatalf("slug=%q err=%v", slug, err)
	}
	jwt, ok := strings.CutPrefix(authorization, "Bearer ")
	parts := strings.Split(jwt, ".")
	if !ok || len(parts) != 3 {
		t.Fatalf("authorization %q", authorization)
	}
	var header struct{ Alg, Typ string }
	var claims struct {
		IssuedAt  int64  `json:"iat"`
		ExpiresAt int64  `json:"exp"`
		Issuer    string `json:"iss"`
	}
	decodeSegment(t, parts[0], &header)
	decodeSegment(t, parts[1], &claims)
	if header.Alg != "RS256" || header.Typ != "JWT" || claims.Issuer != "42" {
		t.Fatalf("header=%+v claims=%+v", header, claims)
	}
	if claims.IssuedAt < before-61 || claims.IssuedAt > before-59 || claims.ExpiresAt-claims.IssuedAt != 600 {
		t.Fatalf("claims times %+v from %d", claims, before)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&testKey.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("signature: %v", err)
	}
}

func decodeSegment(t *testing.T, segment string, out any) {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(segment)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyWebhook(t *testing.T) {
	client := testClient(t, http.NotFoundHandler())
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte("webhook-secret"))
	mac.Write(body)
	valid := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !client.VerifyWebhook(body, valid) {
		t.Fatal("valid signature rejected")
	}
	for name, signature := range map[string]string{
		"empty":     "",
		"no prefix": strings.TrimPrefix(valid, "sha256="),
		"sha1":      "sha1=" + strings.TrimPrefix(valid, "sha256="),
		"bad hex":   "sha256=zz",
		"short":     valid[:len(valid)-2],
		"wrong":     "sha256=" + strings.Repeat("00", 32),
	} {
		if client.VerifyWebhook(body, signature) {
			t.Fatalf("%s: accepted", name)
		}
	}
	if client.VerifyWebhook([]byte(`{"action":"closed"}`), valid) {
		t.Fatal("tampered body accepted")
	}
}

func TestInstallationTokenRequestBody(t *testing.T) {
	var bodies []string
	client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/app/installations/7/access_tokens" ||
			len(strings.Split(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "), ".")) != 3 {
			t.Errorf("unexpected token request %s %s", request.Method, request.URL.Path)
		}
		data, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		bodies = append(bodies, string(data))
		writeJSON(t, writer, map[string]string{"token": "ghs_scoped", "expires_at": "2030-01-01T00:00:00Z"})
	}))
	token, err := client.InstallationToken(context.Background(), 7, []int64{1, 2}, map[string]string{"contents": "read"})
	if err != nil || token.Value != "ghs_scoped" || !token.ExpiresAt.Equal(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	if _, err := client.InstallationToken(context.Background(), 7, nil, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{`{"repository_ids":[1,2],"permissions":{"contents":"read"}}`, `{}`}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("bodies %q", bodies)
	}
}

func TestInstallationTokenCache(t *testing.T) {
	for name, test := range map[string]struct {
		lifetime time.Duration
		mints    int32
	}{"fresh": {time.Hour, 1}, "near expiry": {4 * time.Minute, 2}} {
		t.Run(name, func(t *testing.T) {
			var mints atomic.Int32
			mux := http.NewServeMux()
			mux.Handle("POST /app/installations/9/access_tokens", tokenHandler(t, &mints, test.lifetime))
			mux.HandleFunc("GET /repos/octo/repo/pulls/1", func(writer http.ResponseWriter, request *http.Request) {
				if want := fmt.Sprintf("Bearer ghs_%d", mints.Load()); request.Header.Get("Authorization") != want {
					t.Errorf("authorization %q want %q", request.Header.Get("Authorization"), want)
				}
				writeJSON(t, writer, map[string]int{"number": 1})
			})
			client := testClient(t, mux)
			for range 2 {
				if _, err := client.PullRequest(context.Background(), 9, "octo/repo", 1); err != nil {
					t.Fatal(err)
				}
			}
			if mints.Load() != test.mints {
				t.Fatalf("mints %d want %d", mints.Load(), test.mints)
			}
		})
	}
}

func TestInstallationTokenForgottenAfterUnauthorized(t *testing.T) {
	var mints, reads atomic.Int32
	mux := http.NewServeMux()
	mux.Handle("POST /app/installations/9/access_tokens", tokenHandler(t, &mints, time.Hour))
	mux.HandleFunc("GET /repos/octo/repo/pulls/1", func(writer http.ResponseWriter, request *http.Request) {
		if reads.Add(1) == 1 {
			writer.WriteHeader(http.StatusUnauthorized)
			writeJSON(t, writer, map[string]string{"message": "Bad credentials"})
			return
		}
		writeJSON(t, writer, map[string]int{"number": 1})
	})
	client := testClient(t, mux)
	if _, err := client.PullRequest(context.Background(), 9, "octo/repo", 1); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err %v", err)
	}
	if _, err := client.PullRequest(context.Background(), 9, "octo/repo", 1); err != nil {
		t.Fatal(err)
	}
	if mints.Load() != 2 {
		t.Fatalf("mints %d", mints.Load())
	}
}

const forkPullRequest = `{
  "number": 12, "state": "closed", "draft": false, "title": "Fix", "body": null,
  "html_url": "https://github.com/octo/repo/pull/12", "merged_at": "2026-01-02T03:04:05Z",
  "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-02T03:04:05Z",
  "user": {"login": "fork-user", "id": 77, "html_url": "https://github.com/fork-user", "type": "User"},
  "head": {"label": "fork-user:fix", "ref": "fix", "sha": "aaaa", "user": {"login": "fork-user", "id": 77}, "repo": null},
  "base": {"label": "octo:main", "ref": "main", "sha": "bbbb",
    "repo": {"id": 101, "name": "repo", "full_name": "octo/repo", "private": true,
      "clone_url": "https://github.com/octo/repo.git", "owner": {"login": "octo", "id": 5}}}
}`

func TestPullRequestMapsForkWithDeletedHead(t *testing.T) {
	var mints atomic.Int32
	mux := http.NewServeMux()
	mux.Handle("POST /app/installations/9/access_tokens", tokenHandler(t, &mints, time.Hour))
	mux.HandleFunc("GET /repos/octo/repo/pulls/12", func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, forkPullRequest)
	})
	client := testClient(t, mux)
	pull, err := client.PullRequest(context.Background(), 9, "octo/repo", 12)
	if err != nil {
		t.Fatal(err)
	}
	want := PullRequest{Number: 12, State: "closed", Merged: true, Title: "Fix", HTMLURL: "https://github.com/octo/repo/pull/12",
		Author: Account{ID: 77, Login: "fork-user", HTMLURL: "https://github.com/fork-user"},
		Base: Ref{Ref: "main", SHA: "bbbb", Repository: Repository{ID: 101, FullName: "octo/repo",
			CloneURL: "https://github.com/octo/repo.git", Private: true}},
		Head:      Ref{Ref: "fix", SHA: "aaaa"},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	if !reflect.DeepEqual(pull, want) {
		t.Fatalf("pull\n%+v\nwant\n%+v", pull, want)
	}
}

func TestRepositoryReadsVisibilityAndSizeWithInstallationWideToken(t *testing.T) {
	var mints atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /app/installations/9/access_tokens", func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := io.ReadAll(request.Body)
		if strings.Contains(string(raw), "repository_ids") || strings.Contains(string(raw), "permissions") {
			t.Errorf("repository read narrowed its token: %s", raw)
		}
		tokenHandler(t, &mints, time.Hour)(writer, request)
	})
	mux.HandleFunc("GET /repos/vercel/next.js", func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer ghs_1" {
			t.Errorf("authorization %q", request.Header.Get("Authorization"))
		}
		io.WriteString(writer, `{"id":70107786,"full_name":"vercel/next.js","clone_url":"https://github.com/vercel/next.js.git","private":false,"visibility":"public","size":2400000}`)
	})
	client := testClient(t, mux)
	repository, err := client.Repository(context.Background(), 9, "vercel/next.js")
	if err != nil {
		t.Fatal(err)
	}
	want := Repository{ID: 70107786, FullName: "vercel/next.js", CloneURL: "https://github.com/vercel/next.js.git", Visibility: "public", Size: 2400000}
	if !reflect.DeepEqual(repository, want) {
		t.Fatalf("repository\n%+v\nwant\n%+v", repository, want)
	}
	var status *Error
	if _, err := client.Repository(context.Background(), 9, "octo/missing"); !errors.As(err, &status) || status.Status != http.StatusNotFound {
		t.Fatalf("missing repository: %v", err)
	}
}

func TestOpenPullRequestsFollowsLinks(t *testing.T) {
	var mints atomic.Int32
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.Handle("POST /app/installations/9/access_tokens", tokenHandler(t, &mints, time.Hour))
	mux.HandleFunc("GET /repos/octo/repo/pulls", func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("state") != "open" || query.Get("per_page") != "100" {
			t.Errorf("query %v", query)
		}
		if query.Get("page") == "2" {
			writer.Header().Set("Link", `<`+server.URL+`/repos/octo/repo/pulls?state=open&per_page=100&page=1>; rel="prev"`)
			io.WriteString(writer, `[{"number": 3}]`)
			return
		}
		writer.Header().Set("Link", `<`+server.URL+`/repos/octo/repo/pulls?state=open&per_page=100&page=2>; rel="next", <`+
			server.URL+`/repos/octo/repo/pulls?state=open&per_page=100&page=2>; rel="last"`)
		io.WriteString(writer, `[{"number": 1}, {"number": 2}]`)
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, err := New(App{ID: 42, ClientID: "c", ClientSecret: "s", PrivateKey: testKey, WebhookSecret: []byte("w")},
		WithAPIURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	pulls, err := client.OpenPullRequests(context.Background(), 9, "octo/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(pulls) != 3 || pulls[0].Number != 1 || pulls[2].Number != 3 {
		t.Fatalf("pulls %+v", pulls)
	}
}

func TestOpenPullRequestsCapsResults(t *testing.T) {
	var mints, pages atomic.Int32
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.Handle("POST /app/installations/9/access_tokens", tokenHandler(t, &mints, time.Hour))
	mux.HandleFunc("GET /repos/octo/repo/pulls", func(writer http.ResponseWriter, request *http.Request) {
		page := pages.Add(1)
		writer.Header().Set("Link", fmt.Sprintf(`<%s/repos/octo/repo/pulls?page=%d>; rel="next"`, server.URL, page+1))
		items := make([]map[string]int, 100)
		for index := range items {
			items[index] = map[string]int{"number": int(page)*100 + index}
		}
		writeJSON(t, writer, items)
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, err := New(App{ID: 42, ClientID: "c", ClientSecret: "s", PrivateKey: testKey, WebhookSecret: []byte("w")},
		WithAPIURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	pulls, err := client.OpenPullRequests(context.Background(), 9, "octo/repo")
	if err != nil || len(pulls) != 3000 || pages.Load() != 30 {
		t.Fatalf("pulls=%d pages=%d err=%v", len(pulls), pages.Load(), err)
	}
}

func TestPaginationRejectsForeignOrigin(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Link", `<https://evil.example/user/installations?page=2>; rel="next"`)
		io.WriteString(writer, `{"installations": [{"id": 1}]}`)
	}))
	if _, err := client.UserRepositories(context.Background(), "ghu_token"); err == nil {
		t.Fatal("expected foreign pagination link error")
	}
}

func TestUserRepositories(t *testing.T) {
	var server *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("GET /user/installations", func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer ghu_token" || request.URL.Query().Get("per_page") != "100" {
			t.Errorf("request %v %v", request.Header, request.URL)
		}
		if request.URL.Query().Get("page") == "2" {
			io.WriteString(writer, `{"total_count": 2, "installations": [{"id": 20, "account": {"login": "org"}}]}`)
			return
		}
		writer.Header().Set("Link", `<`+server.URL+`/user/installations?per_page=100&page=2>; rel="next"`)
		io.WriteString(writer, `{"total_count": 2, "installations": [{"id": 10, "account": {"login": "me"}}]}`)
	})
	mux.HandleFunc("GET /user/installations/10/repositories", func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, `{"total_count": 1, "repositories": [{"id": 1, "full_name": "me/a", "clone_url": "https://github.com/me/a.git", "private": true}]}`)
	})
	mux.HandleFunc("GET /user/installations/20/repositories", func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, `{"total_count": 1, "repositories": [{"id": 2, "full_name": "org/b", "clone_url": "https://github.com/org/b.git"}]}`)
	})
	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	client, err := New(App{ID: 42, ClientID: "c", ClientSecret: "s", PrivateKey: testKey, WebhookSecret: []byte("w")},
		WithAPIURL(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	repositories, err := client.UserRepositories(context.Background(), "ghu_token")
	if err != nil {
		t.Fatal(err)
	}
	want := []Repository{
		{ID: 1, FullName: "me/a", CloneURL: "https://github.com/me/a.git", Private: true, InstallationID: 10},
		{ID: 2, FullName: "org/b", CloneURL: "https://github.com/org/b.git", InstallationID: 20},
	}
	if !reflect.DeepEqual(repositories, want) {
		t.Fatalf("repositories %+v", repositories)
	}
}

func TestAuthorizeURL(t *testing.T) {
	client := testClient(t, http.NotFoundHandler())
	parsed, err := url.Parse(client.AuthorizeURL("st&ate", "https://diffx.example/callback"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if parsed.Path != "/login/oauth/authorize" || query.Get("client_id") != "Iv1.client" ||
		query.Get("state") != "st&ate" || query.Get("redirect_uri") != "https://diffx.example/callback" {
		t.Fatalf("authorize URL %s", parsed)
	}
}

func TestExchange(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Error(err)
		}
		if request.Method != http.MethodPost || request.URL.Path != "/login/oauth/access_token" ||
			request.Header.Get("Accept") != "application/json" || request.PostForm.Get("client_id") != "Iv1.client" ||
			request.PostForm.Get("client_secret") != testSecret || request.PostForm.Get("code") != "code-1" ||
			request.PostForm.Get("redirect_uri") != "https://diffx.example/callback" {
			t.Errorf("unexpected exchange %s %v %v", request.URL.Path, request.Header, request.PostForm)
		}
		io.WriteString(writer, `{"access_token":"ghu_a","expires_in":28800,"refresh_token":"ghr_r","refresh_token_expires_in":15897600,"token_type":"bearer","scope":""}`)
	}))
	before := time.Now()
	token, err := client.Exchange(context.Background(), "code-1", "https://diffx.example/callback")
	if err != nil || token.AccessToken != "ghu_a" || token.RefreshToken != "ghr_r" {
		t.Fatalf("token=%+v err=%v", token, err)
	}
	if lifetime := token.ExpiresAt.Sub(before); lifetime < 8*time.Hour || lifetime > 8*time.Hour+time.Minute {
		t.Fatalf("expires at %v", token.ExpiresAt)
	}
	if lifetime := token.RefreshExpiresAt.Sub(before); lifetime < 184*24*time.Hour || lifetime > 184*24*time.Hour+time.Minute {
		t.Fatalf("refresh expires at %v", token.RefreshExpiresAt)
	}
}

func TestExchangeWithoutExpiry(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, `{"access_token":"ghu_a","token_type":"bearer"}`)
	}))
	token, err := client.Exchange(context.Background(), "code", "")
	if err != nil || !token.ExpiresAt.IsZero() || !token.RefreshExpiresAt.IsZero() || token.RefreshToken != "" {
		t.Fatalf("token=%+v err=%v", token, err)
	}
}

func TestOAuthErrors(t *testing.T) {
	for code, unauthorized := range map[string]bool{
		"bad_refresh_token": true, "bad_verification_code": true, "incorrect_client_credentials": false,
	} {
		client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if err := request.ParseForm(); err != nil {
				t.Error(err)
			}
			if request.PostForm.Get("grant_type") != "refresh_token" || request.PostForm.Get("refresh_token") != "ghr_old" {
				t.Errorf("refresh form %v", request.PostForm)
			}
			writeJSON(t, writer, map[string]string{"error": code, "error_description": "nope", "error_uri": "https://docs"})
		}))
		_, err := client.Refresh(context.Background(), "ghr_old")
		if err == nil || errors.Is(err, ErrUnauthorized) != unauthorized {
			t.Fatalf("%s: err %v", code, err)
		}
		if strings.Contains(err.Error(), testSecret) || strings.Contains(err.Error(), "ghr_old") {
			t.Fatalf("%s: error leaks secret: %v", code, err)
		}
	}
}

func TestUnauthorizedResponse(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
		writeJSON(t, writer, map[string]string{"message": "Bad credentials " + strings.Repeat("x", 2000)})
	}))
	_, err := client.User(context.Background(), "ghu_bad")
	var apiError *Error
	if !errors.Is(err, ErrUnauthorized) || !errors.As(err, &apiError) || apiError.Status != 401 ||
		len(apiError.Message) > maxMessageBytes || !strings.HasPrefix(apiError.Message, "Bad credentials") {
		t.Fatalf("err %v", err)
	}
}

func TestRateLimitResponses(t *testing.T) {
	for name, test := range map[string]struct {
		status int
		header map[string]string
		want   int
	}{
		"primary":   {http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "0"}, 429},
		"secondary": {http.StatusForbidden, map[string]string{"Retry-After": "60"}, 429},
		"429":       {http.StatusTooManyRequests, nil, 429},
		"forbidden": {http.StatusForbidden, map[string]string{"X-RateLimit-Remaining": "4999"}, 403},
	} {
		client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			for key, value := range test.header {
				writer.Header().Set(key, value)
			}
			writer.WriteHeader(test.status)
			io.WriteString(writer, `{"message":"API rate limit exceeded"}`)
		}))
		_, err := client.User(context.Background(), "ghu")
		var apiError *Error
		if !errors.As(err, &apiError) || apiError.Status != test.want || errors.Is(err, ErrUnauthorized) {
			t.Fatalf("%s: err %v", name, err)
		}
	}
}

func TestOversizeResponse(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		io.WriteString(writer, `{"login":"`+strings.Repeat("a", maxResponseBytes)+`"}`)
	}))
	if _, err := client.User(context.Background(), "ghu"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err %v", err)
	}
}

func TestCancelledContext(t *testing.T) {
	client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writeJSON(t, writer, map[string]any{"id": 1, "login": "me"})
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.User(ctx, "ghu"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestRepositoryPath(t *testing.T) {
	for fullName, want := range map[string]string{
		"octo/repo":       "/repos/octo/repo",
		"Octo-1/re.po_x":  "/repos/Octo-1/re.po_x",
		"octo":            "",
		"octo/repo/pulls": "",
		"/repo":           "",
		"octo/":           "",
		"../repo":         "",
		"octo/..":         "",
		"octo/.":          "",
		"octo/re po":      "",
		"octo/re%2Fpo":    "",
		"octo/repo?x=1":   "",
	} {
		path, err := repositoryPath(fullName)
		if want == "" {
			if !errors.Is(err, ErrInvalidRepository) {
				t.Fatalf("%q: path=%q err=%v", fullName, path, err)
			}
			continue
		}
		if err != nil || path != want {
			t.Fatalf("%q: path=%q err=%v", fullName, path, err)
		}
	}
	client := testClient(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Errorf("unexpected request %s", request.URL)
	}))
	if _, err := client.OpenPullRequests(context.Background(), 1, "a/b/c"); !errors.Is(err, ErrInvalidRepository) {
		t.Fatalf("err %v", err)
	}
}

func TestParseEvent(t *testing.T) {
	cases := map[string]struct {
		event string
		body  string
		want  any
	}{
		"fork pull request": {"pull_request", `{"action":"synchronize","number":12,
			"pull_request":` + forkPullRequest + `,
			"repository":{"id":101,"name":"repo","full_name":"octo/repo","private":true,"clone_url":"https://github.com/octo/repo.git"},
			"installation":{"id":9,"node_id":"x"},"sender":{"login":"fork-user","id":77}}`,
			PullRequestEvent{Action: "synchronize", InstallationID: 9, Number: 12, Repository: Repository{ID: 101,
				FullName: "octo/repo", CloneURL: "https://github.com/octo/repo.git", Private: true, InstallationID: 9}}},
		"installation": {"installation", `{"action":"deleted","installation":{"id":9,"account":{"login":"octo","id":5},
			"app_id":42},"repositories":[{"id":101,"full_name":"octo/repo","private":true}],"sender":{"id":5}}`,
			InstallationEvent{Action: "deleted", InstallationID: 9}},
		"installation repositories": {"installation_repositories", `{"action":"added","installation":{"id":9},
			"repository_selection":"selected",
			"repositories_added":[{"id":102,"node_id":"n","name":"new","full_name":"octo/new","private":false}],
			"repositories_removed":[],"sender":{"id":5}}`,
			InstallationRepositoriesEvent{Action: "added", InstallationID: 9,
				Added:   []Repository{{ID: 102, FullName: "octo/new", InstallationID: 9}},
				Removed: []Repository{}}},
		"authorization": {"github_app_authorization", `{"action":"revoked","sender":{"login":"me","id":77,"type":"User"}}`,
			AuthorizationEvent{Action: "revoked", UserID: 77}},
		"ping":    {"ping", `{"zen":"Keep it logically awesome.","hook_id":1}`, nil},
		"unknown": {"issues", `not json`, nil},
	}
	for name, test := range cases {
		got, err := ParseEvent(test.event, []byte(test.body))
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: got=%#v err=%v", name, got, err)
		}
	}
	for name, test := range map[string]struct{ event, body string }{
		"malformed":            {"pull_request", `{`},
		"missing action":       {"installation", `{"installation":{"id":9}}`},
		"missing installation": {"pull_request", `{"action":"opened","number":1,"repository":{"id":1}}`},
		"missing repository":   {"pull_request", `{"action":"opened","number":1,"installation":{"id":9}}`},
		"missing sender":       {"github_app_authorization", `{"action":"revoked"}`},
	} {
		if _, err := ParseEvent(test.event, []byte(test.body)); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}
