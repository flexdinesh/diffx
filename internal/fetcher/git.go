package fetcher

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	networkTimeout = 5 * time.Minute
	localTimeout   = time.Minute
	maxOutput      = 64 << 20
	maxErrorOutput = 16 << 10
)

func basicCredential(token string) string {
	return base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
}

// git runs one command in the mirror with a minimal environment that ignores
// system and user configuration, prompts and hooks, and allows only the
// remote's protocol. The token reaches only network commands, through
// environment configuration; never argv or on-disk config. Public leases
// have no token and fetch without any credential.
func (repository mirror) git(ctx context.Context, network bool, stdin string, arguments ...string) (string, error) {
	timeout := localTimeout
	environment := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + repository.dir,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "SSH_ASKPASS=",
		"GIT_ALLOW_PROTOCOL=" + repository.protocol, "GIT_NO_LAZY_FETCH=1",
	}
	if network {
		timeout = networkTimeout
	}
	if network && repository.token != "" {
		environment = append(environment, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+basicCredential(repository.token))
	}
	name := arguments[0]
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	arguments = append([]string{
		"-c", "protocol.allow=never", "-c", "protocol." + repository.protocol + ".allow=always", "-c", "core.hooksPath=" + os.DevNull,
		// Credentials must not follow redirects; maintenance must finish within the timeout.
		"-c", "http.followRedirects=false", "-c", "gc.autoDetach=false", "-c", "maintenance.autoDetach=false",
	}, arguments...)
	command := exec.CommandContext(commandContext, "git", arguments...)
	command.Dir = repository.dir
	command.Env = environment
	command.Stdin = strings.NewReader(stdin)
	output := &limitedBuffer{maximum: maxOutput}
	errorOutput := &limitedBuffer{maximum: maxErrorOutput}
	command.Stdout, command.Stderr = output, errorOutput
	isolate(command)
	err := command.Run()
	reap(command)
	if commandContext.Err() != nil {
		return "", fmt.Errorf("git %s: %w", name, commandContext.Err())
	}
	if err != nil {
		if detail := strings.TrimSpace(errorOutput.String()); detail != "" {
			err = fmt.Errorf("%w: %s", err, detail)
		}
		return "", errors.New(redact(fmt.Sprintf("git %s: %v", name, err), repository.token))
	}
	if output.exceeded {
		return "", fmt.Errorf("git %s: output exceeds %d bytes", name, maxOutput)
	}
	return output.String(), nil
}

type limitedBuffer struct {
	bytes.Buffer
	maximum  int
	exceeded bool
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	if remaining := buffer.maximum - buffer.Len(); len(value) > remaining {
		buffer.exceeded = true
		buffer.Buffer.Write(value[:max(remaining, 0)])
		return len(value), nil
	}
	return buffer.Buffer.Write(value)
}

var gitVersion = regexp.MustCompile(`^git version (\d+)\.(\d+)`)

// requireGit needs 2.44 for GIT_NO_LAZY_FETCH, which keeps collection offline.
func requireGit(ctx context.Context) error {
	raw, err := (mirror{dir: os.TempDir(), protocol: "https"}).git(ctx, false, "", "version")
	if err != nil {
		return fmt.Errorf("diffx-fetcher requires git 2.44 or later: %w", err)
	}
	if !gitAtLeast(raw, 2, 44) {
		return fmt.Errorf("diffx-fetcher requires git 2.44 or later; found %q", strings.TrimSpace(raw))
	}
	return nil
}

func gitAtLeast(raw string, major, minor int) bool {
	match := gitVersion.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return false
	}
	foundMajor, _ := strconv.Atoi(match[1])
	foundMinor, _ := strconv.Atoi(match[2])
	return foundMajor > major || foundMajor == major && foundMinor >= minor
}
