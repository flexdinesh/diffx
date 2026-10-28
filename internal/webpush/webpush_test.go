package webpush_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/internal/testsupport"
	"github.com/flexdinesh/diffx/internal/webpush"
)

type captured struct {
	header http.Header
	body   []byte
}

func endpoint(t *testing.T, status int, header map[string]string) (*httptest.Server, chan captured) {
	t.Helper()
	requests := make(chan captured, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- captured{r.Header.Clone(), body}
		for name, value := range header {
			w.Header().Set(name, value)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, requests
}

// verifyVAPID checks the RFC 8292 header: an ES256 JWT for the endpoint's
// origin, signed by the key it names.
func verifyVAPID(t *testing.T, authorization string, keys webpush.Keys, audience, subject string) {
	t.Helper()
	token, key, ok := strings.Cut(strings.TrimPrefix(authorization, "vapid t="), ", k=")
	if !ok || !strings.HasPrefix(authorization, "vapid t=") || key != keys.Public {
		t.Fatalf("authorization %q", authorization)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT %q", token)
	}
	var claims struct {
		Audience string `json:"aud"`
		Subject  string `json:"sub"`
		Expires  int64  `json:"exp"`
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(raw, &claims) != nil {
		t.Fatalf("claims %s %v", raw, err)
	}
	if claims.Audience != audience || claims.Subject != subject || claims.Expires <= time.Now().Unix() || claims.Expires > time.Now().Add(24*time.Hour).Unix() {
		t.Fatalf("claims %+v, want aud %s sub %s", claims, audience, subject)
	}
	point, err := base64.RawURLEncoding.DecodeString(keys.Public)
	if err != nil {
		t.Fatal(err)
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), point)
	if x == nil {
		t.Fatal("public key is not an uncompressed P-256 point")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		t.Fatalf("signature %v", err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		t.Fatal("VAPID JWT not signed by the advertised key")
	}
}

func TestSendEncryptsAndSignsForTheEndpointOrigin(t *testing.T) {
	keys, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	server, requests := endpoint(t, http.StatusCreated, nil)
	device := testsupport.NewPushDevice(t)
	client := webpush.New(keys, "https://diffx.test")
	payload := []byte(`{"kind":"test","title":"diffx"}`)
	result := client.Send(t.Context(), webpush.Subscription{Endpoint: server.URL + "/push/abc?x=1", P256dh: device.P256dh(), Auth: device.AuthSecret()}, payload,
		webpush.Options{TTL: 24 * time.Hour, Topic: "stream-topic", Urgency: webpush.UrgencyNormal})
	if result.Outcome != webpush.Accepted || result.Status != http.StatusCreated || result.Err != nil {
		t.Fatalf("result %+v", result)
	}
	request := <-requests
	verifyVAPID(t, request.header.Get("Authorization"), keys, server.URL, "https://diffx.test")
	for name, want := range map[string]string{"TTL": "86400", "Topic": "stream-topic", "Urgency": "normal", "Content-Encoding": "aes128gcm", "Content-Type": "application/octet-stream"} {
		if got := request.header.Get(name); got != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
	if strings.Contains(string(request.body), "diffx") {
		t.Fatal("payload sent in plaintext")
	}
	if got := device.Decrypt(t, request.body); string(got) != string(payload) {
		t.Fatalf("decrypted %q", got)
	}
}

func TestSendClassifiesPushServiceResponses(t *testing.T) {
	keys, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	device := testsupport.NewPushDevice(t)
	client := webpush.New(keys, "https://diffx.test")
	retryAt := time.Now().Add(10 * time.Minute).UTC().Format(http.TimeFormat)
	for _, c := range []struct {
		status     int
		header     map[string]string
		outcome    webpush.Outcome
		retryAfter time.Duration
	}{
		{200, nil, webpush.Accepted, 0},
		{202, nil, webpush.Accepted, 0},
		{404, nil, webpush.Gone, 0},
		{410, nil, webpush.Gone, 0},
		{429, map[string]string{"Retry-After": "120"}, webpush.Retry, 2 * time.Minute},
		{503, map[string]string{"Retry-After": retryAt}, webpush.Retry, 10 * time.Minute},
		{500, nil, webpush.Retry, 0},
		{400, nil, webpush.Permanent, 0},
		{403, nil, webpush.Permanent, 0},
		{413, nil, webpush.Permanent, 0},
	} {
		server, requests := endpoint(t, c.status, c.header)
		result := client.Send(t.Context(), webpush.Subscription{Endpoint: server.URL, P256dh: device.P256dh(), Auth: device.AuthSecret()}, []byte("{}"), webpush.Options{TTL: time.Minute})
		<-requests
		if result.Outcome != c.outcome || result.Status != c.status {
			t.Fatalf("HTTP %d: %+v, want %s", c.status, result, c.outcome)
		}
		if c.retryAfter != 0 && (result.RetryAfter < c.retryAfter-2*time.Second || result.RetryAfter > c.retryAfter) {
			t.Fatalf("HTTP %d: Retry-After %s, want %s", c.status, result.RetryAfter, c.retryAfter)
		}
		if c.outcome != webpush.Accepted && result.Err == nil {
			t.Fatalf("HTTP %d: failure without detail", c.status)
		}
	}
}

func TestSendRetriesUnreachableEndpointsAndRefusesInvalidKeys(t *testing.T) {
	keys, err := webpush.GenerateKeys()
	if err != nil {
		t.Fatal(err)
	}
	device := testsupport.NewPushDevice(t)
	client := webpush.New(keys, "https://diffx.test")
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	if result := client.Send(t.Context(), webpush.Subscription{Endpoint: closed.URL, P256dh: device.P256dh(), Auth: device.AuthSecret()}, []byte("{}"), webpush.Options{}); result.Outcome != webpush.Retry || result.Status != 0 {
		t.Fatalf("unreachable endpoint: %+v", result)
	}
	server, _ := endpoint(t, 201, nil)
	if result := client.Send(t.Context(), webpush.Subscription{Endpoint: server.URL, P256dh: "AAAA", Auth: device.AuthSecret()}, []byte("{}"), webpush.Options{}); result.Outcome != webpush.Permanent {
		t.Fatalf("invalid key: %+v", result)
	}
}
