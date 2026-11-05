// Package connection defines native personal-server connections. Secrets belong
// to native callers and must never be projected into browser state or URLs.
package connection

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const Version = 1
const PairPath = "/api/v2/connections/pair"

type Target struct {
	Kind       string `json:"kind"`
	Socket     string `json:"socket,omitempty"`
	URL        string `json:"url,omitempty"`
	RunID      string `json:"runId,omitempty"`
	Pin        string `json:"pin,omitempty"`
	Credential string `json:"credential,omitempty"`
}

type Invitation struct {
	Version int    `json:"version"`
	Address string `json:"address"`
	RunID   string `json:"runId"`
	Pin     string `json:"pin"`
	Secret  string `json:"secret"`
}

type PairRequest struct {
	Version int    `json:"version"`
	RunID   string `json:"runId"`
	Secret  string `json:"secret"`
}

type PairResponse struct {
	Version    int    `json:"version"`
	RunID      string `json:"runId"`
	Credential string `json:"credential"`
}

// ValidSecret reports whether value is a canonical, 256-bit opaque value.
func ValidSecret(value string) bool {
	data, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(data) == 32 && base64.RawURLEncoding.EncodeToString(data) == value
}

func (inv Invitation) validate() error {
	if inv.Version != Version || !ValidSecret(inv.RunID) || !ValidSecret(inv.Pin) || !ValidSecret(inv.Secret) {
		return errors.New("invalid connection invitation")
	}
	_, err := endpoint("https://"+inv.Address, "https", false)
	return err
}

func Parse(value string) (Invitation, error) {
	var inv Invitation
	if !strings.HasPrefix(value, "diffx1_") || len(value) > 8192 {
		return inv, errors.New("invalid connection invitation")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "diffx1_"))
	if err != nil || json.Unmarshal(data, &inv) != nil || inv.validate() != nil {
		return Invitation{}, errors.New("invalid connection invitation")
	}
	return inv, nil
}

func (inv Invitation) String() string {
	if inv.validate() != nil {
		return ""
	}
	data, _ := json.Marshal(inv)
	return "diffx1_" + base64.RawURLEncoding.EncodeToString(data)
}

func endpoint(value, scheme string, loopback bool) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != scheme || u.Host == "" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" {
		return nil, errors.New("invalid connection endpoint")
	}
	host, port, err := net.SplitHostPort(u.Host)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 || host == "" || strings.ContainsAny(host, "%/?#@\\ \t\r\n") {
		return nil, errors.New("invalid connection endpoint")
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsUnspecified() || ip.IsMulticast()) {
		return nil, errors.New("invalid connection endpoint")
	}
	if ip == nil {
		if len(host) > 253 {
			return nil, errors.New("invalid connection endpoint")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return nil, errors.New("invalid connection endpoint")
			}
			for _, character := range label {
				if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-') {
					return nil, errors.New("invalid connection endpoint")
				}
			}
		}
	}
	if loopback && (ip == nil || !ip.IsLoopback()) {
		return nil, errors.New("TCP connection must use a loopback IP address")
	}
	return u, nil
}

type Transport struct {
	http   *http.Transport
	target Target
	origin *url.URL
}

func NewTransport(target Target) (*Transport, error) {
	t := &Transport{target: target, http: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 30 * time.Second, IdleConnTimeout: 60 * time.Second}}
	var err error
	switch target.Kind {
	case "unix":
		if !filepath.IsAbs(target.Socket) || strings.ContainsRune(target.Socket, 0) || target.URL != "" || target.RunID != "" || target.Pin != "" || target.Credential != "" {
			return nil, errors.New("invalid Unix connection")
		}
		t.origin = &url.URL{Scheme: "http", Host: "localhost"}
		t.http.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "unix", target.Socket)
		}
	case "tcp":
		if target.Socket != "" || target.RunID != "" || target.Pin != "" || target.Credential != "" {
			return nil, errors.New("invalid TCP connection")
		}
		t.origin, err = endpoint(target.URL, "http", true)
	case "tls":
		if target.Socket != "" || !ValidSecret(target.RunID) || !ValidSecret(target.Pin) || target.Credential != "" && !ValidSecret(target.Credential) {
			return nil, errors.New("invalid TLS connection")
		}
		t.origin, err = endpoint(target.URL, "https", false)
		t.http.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true, // Exact certificate pin replaces public CA and hostname trust.
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return errors.New("server identity refused")
				}
				pin := sha256.Sum256(state.PeerCertificates[0].Raw)
				if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(pin[:])), []byte(target.Pin)) != 1 {
					return errors.New("server identity refused")
				}
				return nil
			},
		}
	default:
		return nil, errors.New("unsupported connection kind")
	}
	if err != nil {
		return nil, err
	}
	if target.Kind != "unix" {
		t.http.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	}
	return t, nil
}

func (t *Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	r := request.Clone(request.Context())
	r.URL.Scheme, r.URL.Host, r.URL.User = t.origin.Scheme, t.origin.Host, nil
	r.Host = t.origin.Host
	for _, name := range []string{"Authorization", "Cookie", "Origin", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "X-Diffx-Run"} {
		r.Header.Del(name)
	}
	if t.target.Kind == "tls" {
		r.Header.Set("X-Diffx-Run", t.target.RunID)
		if t.target.Credential != "" {
			r.Header.Set("Authorization", "Bearer "+t.target.Credential)
		}
	}
	response, err := t.http.RoundTrip(r)
	if err != nil {
		if request.Context().Err() != nil {
			return nil, request.Context().Err()
		}
		return nil, errors.New("connection request failed")
	}
	return response, nil
}

func (t *Transport) CloseIdleConnections() { t.http.CloseIdleConnections() }

func Pair(ctx context.Context, inv Invitation) (Target, error) {
	if err := inv.validate(); err != nil {
		return Target{}, errors.New("invalid connection invitation")
	}
	target := Target{Kind: "tls", URL: "https://" + inv.Address, RunID: inv.RunID, Pin: inv.Pin}
	transport, err := NewTransport(target)
	if err != nil {
		return Target{}, errors.New("invalid connection invitation")
	}
	defer transport.CloseIdleConnections()
	data, _ := json.Marshal(PairRequest{Version: Version, RunID: inv.RunID, Secret: inv.Secret})
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, http.MethodPost, target.URL+PairPath, strings.NewReader(string(data)))
	r.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	if err != nil {
		return Target{}, errors.New("pairing connection failed")
	}
	defer response.Body.Close()
	var result PairResponse
	decoder := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4096))
	decoder.DisallowUnknownFields()
	if response.StatusCode != http.StatusOK || decoder.Decode(&result) != nil || decoder.Decode(new(struct{})) != io.EOF || result.Version != Version || result.RunID != inv.RunID || !ValidSecret(result.Credential) {
		return Target{}, errors.New("pairing refused; request a current invitation")
	}
	target.Credential = result.Credential
	return target, nil
}
