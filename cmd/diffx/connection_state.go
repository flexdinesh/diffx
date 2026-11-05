package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/flexdinesh/diffx/internal/config"
	"github.com/flexdinesh/diffx/internal/processlock"
	"github.com/flexdinesh/diffx/packages/connection"
)

var errCachedCredentialRefused = errors.New("remote credential refused; request a current invitation")

// Serialize publication so simultaneous launches do not consume one invite twice.
func pairedTarget(ctx context.Context, invitation connection.Invitation) (connection.Target, error) {
	state, err := config.StateDir()
	if err != nil {
		return connection.Target{}, err
	}
	directory := filepath.Join(state, "connections")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return connection.Target{}, errors.New("cannot prepare private connection cache")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return connection.Target{}, errors.New("connection cache must be an owner-only directory")
	}
	identity := sha256.Sum256([]byte(invitation.RunID + ":" + invitation.Pin))
	path := filepath.Join(directory, hex.EncodeToString(identity[:])+".json")
	lock, err := processlock.AcquireContext(ctx, path+".lock")
	if err != nil {
		if ctx.Err() != nil {
			return connection.Target{}, ctx.Err()
		}
		return connection.Target{}, errors.New("cannot lock private connection cache")
	}
	defer lock.Close()
	file, err := os.Open(path)
	if err == nil {
		info, statErr := file.Stat()
		var cached connection.Target
		decoder := json.NewDecoder(io.LimitReader(file, 8192))
		decodeErr := decoder.Decode(&cached)
		_ = file.Close()
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || decodeErr != nil || cached.Kind != "tls" || cached.RunID != invitation.RunID || cached.Pin != invitation.Pin || !connection.ValidSecret(cached.Credential) {
			return connection.Target{}, errors.New("invalid private connection cache")
		}
		cached.URL = "https://" + invitation.Address
		if err := checkConnection(ctx, cached); err == nil {
			return cached, nil
		} else if !errors.Is(err, errCachedCredentialRefused) {
			return connection.Target{}, err
		}
		// A fresh invitation must repair a rejected cached credential. Network
		// failures retain the credential and never consume an invitation.
		if err := os.Remove(path); err != nil {
			return connection.Target{}, errors.New("cannot replace refused connection credential")
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return connection.Target{}, errors.New("cannot read private connection cache")
	}
	target, err := connection.Pair(ctx, invitation)
	if err != nil {
		return connection.Target{}, err
	}
	temporary, err := os.CreateTemp(directory, ".credential-*")
	if err != nil {
		return connection.Target{}, errors.New("cannot save connection credential; request a fresh invitation")
	}
	defer os.Remove(temporary.Name())
	encodeErr := json.NewEncoder(temporary).Encode(target)
	syncErr := temporary.Sync()
	closeErr := temporary.Close()
	if encodeErr != nil || syncErr != nil || closeErr != nil || os.Rename(temporary.Name(), path) != nil {
		return connection.Target{}, errors.New("cannot save connection credential; request a fresh invitation")
	}
	parent, err := os.Open(directory)
	if err != nil {
		return connection.Target{}, errors.New("cannot sync private connection cache")
	}
	err = parent.Sync()
	_ = parent.Close()
	if err != nil {
		return connection.Target{}, errors.New("cannot sync private connection cache")
	}
	return target, nil
}

func checkConnection(ctx context.Context, target connection.Target) error {
	transport, err := connection.NewTransport(target)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/api/v2/health", nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(r)
	if err != nil {
		return errors.New("remote connection unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return errCachedCredentialRefused
	}
	if response.StatusCode != http.StatusOK {
		return errors.New("remote connection unavailable")
	}
	return nil
}
