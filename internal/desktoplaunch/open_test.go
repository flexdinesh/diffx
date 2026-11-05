package desktoplaunch

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/flexdinesh/diffx/packages/localprotocol"
)

func TestMain(m *testing.M) {
	if os.Getenv("DIFFX_TEST_DESKTOP_STUB") == "1" {
		if len(os.Args) == 2 && os.Args[1] == "--desktop-contract" {
			build := os.Getenv("DIFFX_TEST_DESKTOP_BUILD")
			if build == "" {
				build = "dev"
			}
			_ = json.NewEncoder(os.Stdout).Encode(localprotocol.DesktopContract{ProtocolVersion: localprotocol.Version, Build: build, Sidecar: os.Getenv("DIFFX_TEST_CLI")})
			os.Exit(0)
		}
		if len(os.Args) == 1 {
			request := os.NewFile(3, "request")
			ack := os.NewFile(5, "ack")
			defer request.Close()
			defer ack.Close()
			var intent localprotocol.Intent
			if json.NewDecoder(request).Decode(&intent) != nil {
				os.Exit(20)
			}
			cwd, err := os.Getwd()
			if err != nil || cwd != intent.Directory || len(intent.Args) != 2 || intent.Args[0] != "connect" || intent.Args[1] != "secret invitation" {
				os.Exit(21)
			}
			if os.Getenv("DIFFX_TEST_STARTUP_FAIL") == "1" {
				os.Exit(22)
			}
			_ = json.NewEncoder(ack).Encode(localprotocol.Acceptance{Accepted: true})
			os.Exit(0)
		}
	}
	os.Exit(m.Run())
}

func TestOpenPassesPrivateIntentAndPreservesStdinBeforeAcceptance(t *testing.T) {
	cli, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	desktop := filepath.Join(filepath.Dir(cli), "diffx-desktop")
	if err = os.Link(cli, desktop); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(desktop) })
	t.Setenv("DIFFX_TEST_DESKTOP_STUB", "1")
	t.Setenv("DIFFX_TEST_CLI", cli)
	t.Setenv("DIFFX_DESKTOP_SIDECAR", cli)
	t.Setenv("DIFFX_RUNTIME_DIR", t.TempDir())
	dir, err := localprotocol.RuntimeDirectory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	input, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if _, err = input.WriteString("piped patch"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DIFFX_TEST_DESKTOP_BUILD", "incompatible")
	if handled, err := Open(t.Context(), []string{"connect", "secret invitation"}, input, io.Discard); handled || err == nil {
		t.Fatal("mismatched explicit desktop build label admitted")
	}
	t.Setenv("DIFFX_TEST_DESKTOP_BUILD", "dev")
	for _, fail := range []bool{true, false} {
		if _, err = input.Seek(0, 0); err != nil {
			t.Fatal(err)
		}
		value := "0"
		if fail {
			value = "1"
		}
		t.Setenv("DIFFX_TEST_STARTUP_FAIL", value)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		handled, err := Open(ctx, []string{"connect", "secret invitation"}, input, io.Discard)
		cancel()
		if handled == fail || fail && err == nil || !fail && err != nil {
			t.Fatalf("startup fail=%v handled=%v error=%v", fail, handled, err)
		}
		data, err := io.ReadAll(input)
		if err != nil || string(data) != "piped patch" {
			t.Fatalf("stdin consumed before acceptance: %q %v", data, err)
		}
	}
}

func TestResolveRequiresCompanionBesideSelectedCLI(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "diffx")
	desktop := filepath.Join(dir, "diffx-desktop")
	for _, path := range []string{cli, desktop} {
		if err := os.WriteFile(path, []byte("binary"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DIFFX_DESKTOP_SIDECAR", cli)
	if got, err := resolve(cli); err != nil || got != desktop {
		t.Fatalf("development pair %q: %v", got, err)
	}
	t.Setenv("DIFFX_DESKTOP_SIDECAR", filepath.Join(t.TempDir(), "other-diffx"))
	if _, err := resolve(cli); err == nil {
		t.Fatal("mismatched development pair admitted")
	}
	t.Setenv("DIFFX_DESKTOP_SIDECAR", "")
	if err := os.Remove(desktop); err != nil {
		t.Fatal(err)
	}
	if _, err := resolve(cli); err == nil {
		t.Fatal("unrelated PATH companion admitted")
	}
}

func TestResolveMacBundleCompanion(t *testing.T) {
	dir := t.TempDir()
	resources := filepath.Join(dir, "Resources")
	macos := filepath.Join(dir, "MacOS")
	for _, path := range []string{resources, macos} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	cli := filepath.Join(resources, "diffx")
	desktop := filepath.Join(macos, "diffx-desktop")
	for _, path := range []string{cli, desktop} {
		if err := os.WriteFile(path, []byte("binary"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DIFFX_DESKTOP_SIDECAR", "")
	if got, err := resolve(cli); err != nil || got != desktop {
		t.Fatalf("bundle pair %q: %v", got, err)
	}
}

func TestAcceptanceIsTheFallbackBoundary(t *testing.T) {
	if handled, err := accepted(localprotocol.Acceptance{Accepted: true}); !handled || err != nil {
		t.Fatal(handled, err)
	}
	if handled, err := accepted(localprotocol.Acceptance{Error: "private failure detail"}); handled || err == nil || err.Error() == "private failure detail" {
		t.Fatal("decline lost fallback boundary or exposed private diagnostic")
	}
}
