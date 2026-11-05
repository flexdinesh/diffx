// Package pairing owns ephemeral, run-bound admission for personal servers.
// Only credential hashes survive an exchange, and nothing is persisted.
package pairing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/flexdinesh/diffx/packages/api"
	"github.com/flexdinesh/diffx/packages/connection"
)

const InvitationLifetime = 10 * time.Minute
const AttemptsPerMinute = 20
const MaxCredentials = 256

type Server struct {
	RunID       string
	Pin         string
	certificate tls.Certificate
	mu          sync.Mutex
	invitation  [32]byte
	expires     time.Time
	credentials map[[32]byte]struct{}
	window      time.Time
	attempts    int
	now         func() time.Time
}

func secret() string {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		panic("system random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(data[:])
}

func New() (*Server, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errors.New("create personal server identity failed")
	}
	now := time.Now()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, errors.New("create personal server identity failed")
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "diffx personal server"}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(10, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, errors.New("create personal server identity failed")
	}
	pin := sha256.Sum256(der)
	return &Server{RunID: secret(), Pin: base64.RawURLEncoding.EncodeToString(pin[:]), certificate: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, credentials: make(map[[32]byte]struct{}), now: time.Now}, nil
}

func (s *Server) TLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{s.certificate}}
}

// Rotate replaces only the unused invitation. Addresses share one consumption.
// Credential admission survives rotation and ends with this Server's lifetime.
func (s *Server) Rotate(addresses []string) ([]string, error) {
	if len(addresses) == 0 || len(addresses) > 64 {
		return nil, errors.New("no usable personal server address")
	}
	value := secret()
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		encoded := (connection.Invitation{Version: connection.Version, Address: address, RunID: s.RunID, Pin: s.Pin, Secret: value}).String()
		if encoded == "" {
			return nil, errors.New("invalid personal server address")
		}
		result = append(result, encoded)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invitation = sha256.Sum256([]byte(value))
	s.expires = s.now().Add(InvitationLifetime)
	return result, nil
}

func (s *Server) exchange(input connection.PairRequest) (connection.PairResponse, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	hash := sha256.Sum256([]byte(input.Secret))
	if input.Version != connection.Version || input.RunID != s.RunID || !connection.ValidSecret(input.Secret) || !now.Before(s.expires) || subtle.ConstantTimeCompare(hash[:], s.invitation[:]) != 1 {
		return connection.PairResponse{}, http.StatusUnauthorized
	}
	if len(s.credentials) >= MaxCredentials {
		return connection.PairResponse{}, http.StatusTooManyRequests
	}
	value := secret()
	s.credentials[sha256.Sum256([]byte(value))] = struct{}{}
	s.invitation, s.expires = [32]byte{}, time.Time{}
	return connection.PairResponse{Version: connection.Version, RunID: s.RunID, Credential: value}, http.StatusOK
}

func (s *Server) admitAttempt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if s.window.IsZero() || !now.Before(s.window.Add(time.Minute)) {
		s.window, s.attempts = now, 0
	}
	if s.attempts >= AttemptsPerMinute {
		return false
	}
	s.attempts++
	return true
}

func (s *Server) authorized(r *http.Request) bool {
	if r.Header.Get("X-Diffx-Run") != s.RunID || len(r.Header.Values("Authorization")) != 1 {
		return false
	}
	value, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !connection.ValidSecret(value) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok = s.credentials[sha256.Sum256([]byte(value))]
	return ok
}

func problem(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}{"about:blank", http.StatusText(status), status, "Personal server request refused"})
}

// Handler exposes only pairing and the shared public review allowlist. Browser
// authority, private control, account, MCP and ingestion never cross this edge.
func (s *Server) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.TLS == nil || r.Header.Get("Origin") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Sec-Fetch-Site") != "" || r.Header.Get("Sec-Fetch-Mode") != "" {
			problem(w, http.StatusForbidden)
			return
		}
		if r.URL.EscapedPath() != r.URL.Path {
			problem(w, http.StatusNotFound)
			return
		}
		if r.URL.Path == connection.PairPath && r.Method == http.MethodPost {
			if !s.admitAttempt() {
				w.Header().Set("Retry-After", "60")
				problem(w, http.StatusTooManyRequests)
				return
			}
			if strings.Split(r.Header.Get("Content-Type"), ";")[0] != "application/json" {
				problem(w, http.StatusUnsupportedMediaType)
				return
			}
			_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(10 * time.Second))
			var input connection.PairRequest
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&input) != nil || decoder.Decode(new(struct{})) != io.EOF {
				problem(w, http.StatusBadRequest)
				return
			}
			if r.Header.Get("X-Diffx-Run") != s.RunID {
				input.RunID = ""
			}
			result, status := s.exchange(input)
			if status != http.StatusOK {
				if status == http.StatusTooManyRequests {
					w.Header().Set("Retry-After", "60")
				}
				problem(w, status)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(result)
			return
		}
		if !api.PublicReviewRoute(r.Method, r.URL.Path) {
			problem(w, http.StatusNotFound)
			return
		}
		if !s.authorized(r) {
			problem(w, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
