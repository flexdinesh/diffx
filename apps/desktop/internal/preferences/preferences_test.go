package preferences

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/flexdinesh/diffx/packages/api/goclient"
)

func TestDefaultsAndOSPath(t *testing.T) {
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
	p, err := LoadFile(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || !reflect.DeepEqual(p, Defaults()) {
		t.Fatalf("missing file: %+v %v", p, err)
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "preferences.json" || filepath.Base(filepath.Dir(path)) != "diffx-native" {
		t.Fatalf("OS path %s", path)
	}
	first, second := Defaults(), Defaults()
	first.Seen["stream"] = "context"
	if len(second.Seen) != 0 {
		t.Fatal("defaults share mutable metadata")
	}
}

func TestRoundTripDefaultsAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "preferences.json")
	p := Defaults()
	p.Theme = "nord"
	p.CodeTheme = "vitesse"
	p.Wrap = true
	p.Layout = "unified"
	p.Seen["opaque-stream"] = "opaque-context"
	p.Scopes["opaque-stream"] = "committed"
	p.FontFamily = "JetBrains Mono"
	p.FilePaneWidth = 400
	p.NoLineNumbers = true
	p.NoBackground = true
	p.Indicators = "classic"
	p.Syntax = false
	if err := p.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("round trip %+v %v", got, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file permissions %o", info.Mode().Perm())
	}
	if err := os.WriteFile(path, []byte(`{"version":1,"theme":"dark"}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadFile(path)
	if err != nil || got.Theme != "dark" || got.FontSize != 13 || !got.GroupByRepository || !got.Syntax || got.Indicators != "bars" || got.NoLineNumbers || got.NoBackground {
		t.Fatalf("omitted additive fields %+v %v", got, err)
	}
	corrupt := []byte(`{"version":1,"theme":`)
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	got, err = LoadFile(path)
	if err == nil || !reflect.DeepEqual(got, Defaults()) {
		t.Fatalf("corrupt defaults %+v %v", got, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, corrupt) {
		t.Fatal("load overwrote corrupt original")
	}
	if err := got.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err != nil {
		t.Fatalf("explicit save did not recover: %v", err)
	}
}

func TestBadFilesReturnDefaultsAndPreserveOriginal(t *testing.T) {
	for _, data := range []string{
		`null`, `[]`, `{}`, `{"version":99}`, `{"version":0}`,
		`{"version":1,"theme":"missing"}`, `{"version":1,"codeTheme":"bad"}`,
		`{"version":1,"layout":"sideways"}`, `{"version":1,"lineDiff":"bad"}`,
		`{"version":1,"fontSize":0}`, `{"version":1,"fontSize":1e99}`,
		`{"version":1,"tabSize":17}`, `{"version":1,"filePaneWidth":-1}`,
		`{"version":1,"seen":null}`, `{"version":1,"scopes":{"stream":"invented"}}`,
		`{"version":1,"indicators":"bad"}`, `{"version":1,"syntax":"yes"}`,
		`{"version":1} {"version":1}`,
	} {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "prefs.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := LoadFile(path)
			if err == nil || !reflect.DeepEqual(got, Defaults()) {
				t.Fatalf("accepted bad file: %+v %v", got, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != data {
				t.Fatal("bad original changed")
			}
		})
	}
}

func TestSavedScopesUsePublicWireValues(t *testing.T) {
	p := Defaults()
	for _, scope := range []goclient.DiffMode{goclient.DiffModeAll, goclient.DiffModeCommitted, goclient.DiffModeUncommitted, goclient.DiffModeStaged, goclient.DiffModeUnstaged} {
		p.Scopes["opaque-stream"] = string(scope)
		if err := p.Validate(); err != nil {
			t.Errorf("public wire scope %q rejected: %v", scope, err)
		}
	}
	for _, alias := range []string{"branch", "combined"} {
		p.Scopes["opaque-stream"] = alias
		if err := p.Validate(); err == nil {
			t.Errorf("presentation label %q accepted as wire scope", alias)
		}
	}
	for _, indicators := range []string{"bars", "classic", "none"} {
		p = Defaults()
		p.Indicators = indicators
		if err := p.Validate(); err != nil {
			t.Error(err)
		}
	}
}

func TestAtomicReplacementAndFailedSaveCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prefs.json")
	p := Defaults()
	if err := p.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p.Theme = "invalid"
	if err := p.SaveFile(path); err == nil {
		t.Fatal("invalid save accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("invalid save damaged destination")
	}
	// A directory destination forces rename failure after writing and syncing.
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := Defaults().SaveFile(blocked); err == nil {
		t.Fatal("rename onto directory succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("failed save leaked temporary files: %v", entries)
	}
	// Replacing a file must leave a reader of the original inode unchanged.
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	p = Defaults()
	p.Theme = "dark"
	if err := p.SaveFile(path); err != nil {
		t.Fatal(err)
	}
	var old Preferences
	if err := json.NewDecoder(reader).Decode(&old); err != nil || old.Theme != "system" {
		t.Fatalf("original inode modified %+v %v", old, err)
	}
	latest, err := LoadFile(path)
	if err != nil || latest.Theme != "dark" {
		t.Fatalf("replacement missing %+v %v", latest, err)
	}
}

func TestConcurrentReplacementNeverPublishesPartialJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	if err := Defaults().SaveFile(path); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, theme := range []string{"light", "dark", "nord"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 12 {
				p := Defaults()
				p.Theme = theme
				if err := p.SaveFile(path); err != nil {
					t.Error(err)
				}
				if _, err := LoadFile(path); err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
}
