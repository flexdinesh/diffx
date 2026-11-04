// Package preferences persists native presentation choices. Review state belongs
// to the server and must never be stored here.
package preferences

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const Version = 1
const maxBytes = 4 << 20

type Preferences struct {
	Version              int               `json:"version"`
	Theme                string            `json:"theme"`
	CodeTheme            string            `json:"codeTheme"`
	Layout               string            `json:"layout"`
	Wrap                 bool              `json:"wrap"`
	NoLineNumbers        bool              `json:"noLineNumbers"`
	NoBackground         bool              `json:"noBackground"`
	Indicators           string            `json:"indicators"`
	Syntax               bool              `json:"syntax"`
	LineDiff             string            `json:"lineDiff"`
	FontSize             float32           `json:"fontSize"`
	FontFamily           string            `json:"fontFamily"`
	TabSize              int               `json:"tabSize"`
	HideReviewed         bool              `json:"hideReviewed"`
	ProjectPaneWidth     float32           `json:"projectPaneWidth"`
	FilePaneWidth        float32           `json:"filePaneWidth"`
	FileView             string            `json:"fileView"`
	NavigationMode       string            `json:"navigationMode"`
	GroupByRepository    bool              `json:"groupByRepository"`
	ProjectPaneCollapsed bool              `json:"projectPaneCollapsed"`
	FilePaneCollapsed    bool              `json:"filePaneCollapsed"`
	Seen                 map[string]string `json:"seen"`
	SeenInitialized      bool              `json:"seenInitialized"`
	Scopes               map[string]string `json:"scopes"`
}

func Defaults() Preferences {
	return Preferences{
		Version: Version, Theme: "system", CodeTheme: "match",
		Layout: "split", LineDiff: "word-alt", Indicators: "bars", Syntax: true,
		FontSize: 13, TabSize: 2,
		ProjectPaneWidth: defaultProjectPaneWidth, FilePaneWidth: defaultFilePaneWidth,
		FileView: "tree", NavigationMode: "list", GroupByRepository: true,
		Seen: map[string]string{}, Scopes: map[string]string{},
	}
}

// Path uses the OS application config directory, separate from browser storage
// and the server runtime/config/state directories.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("native preferences directory: %w", err)
	}
	return filepath.Join(dir, "diffx-native", "preferences.json"), nil
}

func Load() (Preferences, error) {
	path, err := Path()
	if err != nil {
		return Defaults(), err
	}
	return LoadFile(path)
}
func LoadFile(path string) (Preferences, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), fmt.Errorf("read native preferences %s: %w", path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return Defaults(), fmt.Errorf("read native preferences: %w", err)
	}
	if len(data) > maxBytes {
		return Defaults(), fmt.Errorf("native preferences exceed %d bytes; reset Settings to recover", maxBytes)
	}
	// A missing version cannot be mistaken for the current format. Missing
	// individual options, however, inherit defaults for additive evolution.
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Defaults(), fmt.Errorf("invalid native preferences %s; reset Settings to recover: %w", path, err)
	}
	if header.Version != Version {
		return Defaults(), fmt.Errorf("unsupported native preferences version %d (supported %d); original file preserved", header.Version, Version)
	}
	p := Defaults()
	if err := json.Unmarshal(data, &p); err != nil {
		return Defaults(), fmt.Errorf("invalid native preferences %s: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return Defaults(), fmt.Errorf("invalid native preferences %s; reset Settings to recover: %w", path, err)
	}
	return p, nil
}

func (p Preferences) Validate() error {
	if p.Version != Version {
		return fmt.Errorf("unsupported preferences version %d", p.Version)
	}
	if !themeIDs[p.Theme] {
		return fmt.Errorf("unknown theme %q", p.Theme)
	}
	for _, f := range []struct {
		name, value string
		allowed     []string
	}{
		{"code theme", p.CodeTheme, []string{"match", "pierre", "github", "vitesse", "catppuccin"}},
		{"layout", p.Layout, []string{"split", "unified"}},
		{"line diff", p.LineDiff, []string{"word-alt", "word", "char", "none"}},
		{"indicators", p.Indicators, []string{"bars", "classic", "none"}},
		{"file view", p.FileView, []string{"tree", "list"}},
		{"navigation mode", p.NavigationMode, []string{"list", "rail"}},
	} {
		if !slices.Contains(f.allowed, f.value) {
			return fmt.Errorf("unknown %s %q", f.name, f.value)
		}
	}
	finiteRange := func(n float32, lo, hi float32) bool {
		return !math.IsNaN(float64(n)) && !math.IsInf(float64(n), 0) && n >= lo && n <= hi
	}
	if !finiteRange(p.FontSize, 8, 32) {
		return errors.New("font size must be between 8 and 32")
	}
	if p.TabSize < 1 || p.TabSize > 16 {
		return errors.New("tab size must be between 1 and 16")
	}
	if !finiteRange(p.ProjectPaneWidth, 120, 1200) || !finiteRange(p.FilePaneWidth, 120, 1200) {
		return errors.New("pane widths must be between 120 and 1200")
	}
	if len(p.FontFamily) > 256 || strings.ContainsAny(p.FontFamily, "\x00\r\n") {
		return errors.New("invalid font family")
	}
	for _, m := range []map[string]string{p.Seen, p.Scopes} {
		if m == nil {
			return errors.New("seen and scopes must be objects")
		}
		for k, v := range m {
			if k == "" || v == "" || len(k) > 1024 || len(v) > 1024 {
				return errors.New("invalid saved stream metadata")
			}
		}
	}
	for _, scope := range p.Scopes {
		if !slices.Contains([]string{"all", "committed", "uncommitted", "staged", "unstaged"}, scope) {
			return fmt.Errorf("unknown saved scope %q", scope)
		}
	}
	return nil
}

func (p Preferences) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	return p.SaveFile(path)
}

func (p Preferences) SaveFile(path string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if len(data) > maxBytes {
		return errors.New("native preferences too large")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create native preferences directory: %w", err)
	}
	// Write beside the destination so Rename is atomic on the same filesystem.
	file, err := os.CreateTemp(dir, ".preferences-*")
	if err != nil {
		return fmt.Errorf("create native preferences: %w", err)
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write native preferences: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync native preferences: %w", err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("replace native preferences: %w", err)
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open native preferences directory: %w", err)
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("sync native preferences directory: %w", err)
	}
	return nil
}
