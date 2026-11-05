package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

type savedPort struct {
	Version int `json:"version"`
	Port    int `json:"port"`
}

// listenPersisted binds before publishing or trusting an origin. An existing
// port is never replaced, including collisions or corrupt state: localStorage
// preferences belong to that exact origin across every desktop restart.
func listenPersisted(portFile string) (net.Listener, error) {
	if !filepath.IsAbs(portFile) {
		return nil, errors.New("desktop origin state path must be absolute")
	}
	file, err := os.Open(portFile)
	if err == nil {
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1024 {
			return nil, fmt.Errorf("invalid desktop origin state file %s", portFile)
		}
		var saved savedPort
		decoder := json.NewDecoder(io.LimitReader(file, 1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&saved); err != nil {
			return nil, fmt.Errorf("read saved desktop preference origin %s: %w", portFile, err)
		}
		var extra json.RawMessage
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("invalid trailing desktop origin state in %s", portFile)
		}
		if saved.Version != 1 || saved.Port < 1 || saved.Port > 65535 {
			return nil, fmt.Errorf("invalid desktop preference origin in %s; saved state left untouched", portFile)
		}
		address := net.JoinHostPort("127.0.0.1", strconv.Itoa(saved.Port))
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return nil, fmt.Errorf("desktop preference origin http://%s unavailable; free its port and retry (saved origin unchanged): %w", address, err)
		}
		return listener, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read desktop preference origin: %w", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	_, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		listener.Close()
		return nil, err
	}
	port, err := strconv.Atoi(portText)
	if err == nil {
		err = persistPort(portFile, savedPort{Version: 1, Port: port})
	}
	if err != nil {
		listener.Close()
		return nil, err
	}
	return listener, nil
}

// Publish a synced temporary file with an atomic, no-overwrite link. Two first
// launches cannot overwrite each other's origin. Linux and macOS support this
// in their per-user application-data filesystem.
func persistPort(path string, port savedPort) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".desktop-origin-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err := json.NewEncoder(file).Encode(port); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(file.Name(), path); err != nil {
		return fmt.Errorf("save desktop preference origin without replacing existing state (another launch may be active): %w", err)
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
