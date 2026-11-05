package host

import (
	"bytes"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDesktopOriginSurvivesRestartAndRefusesCollision(t *testing.T) {
	state := filepath.Join(t.TempDir(), "data", "desktop-origin.json")
	assets := fstest.MapFS{"index.html": {Data: []byte("desktop")}}
	first, err := Start(assets, state)
	if err != nil {
		t.Fatal(err)
	}
	origin := first.Origin
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(state)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("origin state permissions: %v", info.Mode())
	}
	second, err := Start(assets, state)
	if err != nil {
		t.Fatal(err)
	}
	if second.Origin != origin {
		second.Close()
		t.Fatalf("restart changed preference origin: %s -> %s", origin, second.Origin)
	}
	response, err := http.Get(second.Origin)
	if err != nil {
		second.Close()
		t.Fatal(err)
	}
	_, err = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if err != nil {
		second.Close()
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	occupier, err := net.Listen("tcp", strings.TrimPrefix(origin, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer occupier.Close()
	if unexpected, err := Start(assets, state); err == nil {
		unexpected.Close()
		t.Fatal("occupied preference origin silently fell back")
	} else if !strings.Contains(err.Error(), "saved origin unchanged") || !strings.Contains(err.Error(), origin) {
		t.Fatalf("unclear collision failure: %v", err)
	}
	after, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("collision overwrote saved origin")
	}
}

func TestConcurrentFirstLaunchPublishesOneOrigin(t *testing.T) {
	state := filepath.Join(t.TempDir(), "desktop-origin.json")
	start := make(chan struct{})
	type result struct {
		listener net.Listener
		err      error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() { <-start; listener, err := listenPersisted(state); results <- result{listener, err} }()
	}
	close(start)
	var winner net.Listener
	successes := 0
	for range 2 {
		r := <-results
		if r.err == nil {
			successes++
			winner = r.listener
			defer r.listener.Close()
		}
	}
	if successes != 1 {
		t.Fatalf("first launches published %d origins", successes)
	}
	if err := winner.Close(); err != nil {
		t.Fatal(err)
	}
	restart, err := listenPersisted(state)
	if err != nil {
		t.Fatal(err)
	}
	defer restart.Close()
	if restart.Addr().String() != winner.Addr().String() {
		t.Fatal("first launch race lost published origin")
	}
}

func TestCorruptStateAndAtomicPublicationNeverReplaceSettingsOrigin(t *testing.T) {
	for _, data := range []string{`{`, `{"version":2,"port":3210}`, `{"version":1,"port":0}`, `{"version":1,"port":65536}`, `{"version":1,"port":3210,"origin":"http://attacker.example"}`, `{"version":1,"port":3210} {}`} {
		path := filepath.Join(t.TempDir(), "desktop-origin.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if listener, err := listenPersisted(path); err == nil {
			listener.Close()
			t.Fatalf("accepted corrupt state %q", data)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != data {
			t.Fatal("corrupt state replaced silently")
		}
	}
	path := filepath.Join(t.TempDir(), "desktop-origin.json")
	if err := persistPort(path, savedPort{Version: 1, Port: 3210}); err != nil {
		t.Fatal(err)
	}
	if err := persistPort(path, savedPort{Version: 1, Port: 3211}); !errors.Is(err, os.ErrExist) {
		t.Fatalf("atomic publication overwrote origin: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("3210")) {
		t.Fatal("origin changed")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary state files leaked: %v", entries)
	}
}
