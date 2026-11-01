package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const (
	daggerVersion      = "0.21.11"
	engineName         = "diffx-validation-engine-v" + daggerVersion
	engineVolume       = "diffx-validation-cache-v" + daggerVersion
	engineImage        = "registry.dagger.io/engine@sha256:6b557cc8b5fdfee0e2e8c82d451ac922097a1255482143e1b96c0be3ff2a303c"
	engineOwnerLabel   = "io.diffx.validation.owner"
	engineVersionLabel = "io.diffx.validation.version"
)

// engineCommand is deliberately narrower than a Docker client. There are no
// remove, stop, restart or prune operations in this owner's lifecycle.
type engineCommand func(context.Context, string, ...string) ([]byte, error)

func engineCommandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Env = isolatedEngineEnvironment(os.Environ())
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func isolatedEngineEnvironment(inherited []string) []string {
	environment := make([]string, 0, len(inherited)+2)
	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "DAGGER_") || strings.HasPrefix(key, "_EXPERIMENTAL_DAGGER_") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment,
		"DAGGER_LEAVE_OLD_ENGINE=true",
		"_EXPERIMENTAL_DAGGER_RUNNER_HOST=docker-container://"+engineName,
	)
}

type engineContainer struct {
	Name   string
	Config struct {
		Image  string
		Labels map[string]string
	}
	State      struct{ Running bool }
	HostConfig struct {
		Privileged   bool
		NetworkMode  string
		PortBindings map[string]json.RawMessage
	}
	Mounts []struct {
		Type        string
		Name        string
		Destination string
	}
}

type engineCacheVolume struct {
	Name   string
	Driver string
	Labels map[string]string
}

func ownedEngineLabels(labels map[string]string) bool {
	return labels[engineOwnerLabel] == "diffx" && labels[engineVersionLabel] == daggerVersion
}

func inspectEngine(ctx context.Context, execute engineCommand) (bool, error) {
	output, err := execute(ctx, "docker", "container", "ls", "--all", "--format", "{{.Names}}")
	if err != nil {
		return false, err
	}
	if !listedEngineResource(output, engineName) {
		return false, nil
	}
	output, err = execute(ctx, "docker", "container", "inspect", engineName)
	if err != nil {
		return false, err
	}
	var containers []engineContainer
	if err := json.Unmarshal(output, &containers); err != nil {
		return false, err
	}
	if len(containers) != 1 {
		return false, fmt.Errorf("expected exactly one engine named %s", engineName)
	}
	container := containers[0]
	if container.Name != "/"+engineName || !ownedEngineLabels(container.Config.Labels) || container.Config.Image != engineImage {
		return false, fmt.Errorf("engine %s has unrecognized ownership or image; left untouched", engineName)
	}
	if !container.State.Running {
		return false, fmt.Errorf("engine %s is stopped; coordinate an explicit restart with other users", engineName)
	}
	if !container.HostConfig.Privileged || container.HostConfig.NetworkMode == "host" || len(container.HostConfig.PortBindings) != 0 {
		return false, fmt.Errorf("engine %s has incompatible runtime configuration; left untouched", engineName)
	}
	if len(container.Mounts) != 1 || container.Mounts[0].Type != "volume" || container.Mounts[0].Name != engineVolume || container.Mounts[0].Destination != "/var/lib/dagger" {
		return false, fmt.Errorf("engine %s has unexpected mounts; left untouched", engineName)
	}
	return true, nil
}

func inspectEngineVolume(ctx context.Context, execute engineCommand) (bool, error) {
	output, err := execute(ctx, "docker", "volume", "ls", "--format", "{{.Name}}")
	if err != nil {
		return false, err
	}
	if !listedEngineResource(output, engineVolume) {
		return false, nil
	}
	output, err = execute(ctx, "docker", "volume", "inspect", engineVolume)
	if err != nil {
		return false, err
	}
	var volumes []engineCacheVolume
	if err := json.Unmarshal(output, &volumes); err != nil {
		return false, err
	}
	if len(volumes) != 1 || volumes[0].Name != engineVolume || volumes[0].Driver != "local" || !ownedEngineLabels(volumes[0].Labels) {
		return false, fmt.Errorf("volume %s has unrecognized ownership or driver; left untouched", engineVolume)
	}
	return true, nil
}

func listedEngineResource(output []byte, name string) bool {
	for _, entry := range strings.Fields(string(output)) {
		if entry == name {
			return true
		}
	}
	return false
}

// setupEngine is an explicit opt-in command. Ordinary validation never calls it.
// Failed or interrupted setup leaves its owned resources for inspection; it does
// not roll back by deleting resources another simultaneous setup may be using.
func setupEngine(ctx context.Context) error {
	return setupEngineWith(ctx, engineCommandOutput)
}

func setupEngineWith(ctx context.Context, execute engineCommand) error {
	present, err := inspectEngine(ctx, execute)
	if err != nil {
		return err
	}
	volumePresent, err := inspectEngineVolume(ctx, execute)
	if err != nil {
		return err
	}
	if present {
		if !volumePresent {
			return fmt.Errorf("engine cache %s is missing", engineVolume)
		}
		return nil
	}
	if !volumePresent {
		if _, err := execute(ctx, "docker", "volume", "create", "--label", engineOwnerLabel+"=diffx", "--label", engineVersionLabel+"="+daggerVersion, engineVolume); err != nil {
			return err
		}
		if present, err := inspectEngineVolume(ctx, execute); err != nil {
			return err
		} else if !present {
			return fmt.Errorf("engine cache %s was not created", engineVolume)
		}
	}
	_, runErr := execute(ctx, "docker", "run", "--detach", "--name", engineName,
		"--label", engineOwnerLabel+"=diffx", "--label", engineVersionLabel+"="+daggerVersion,
		"--privileged", "--restart", "no", "--platform", "linux/amd64",
		"--mount", "type=volume,source="+engineVolume+",target=/var/lib/dagger",
		engineImage)
	// A concurrent setup may have won the name. Accept only a fully verified
	// running owner, never adopt an unknown container or restart a stopped one.
	present, inspectErr := inspectEngine(ctx, execute)
	if inspectErr != nil {
		return inspectErr
	}
	if !present {
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("engine %s was not created", engineName)
	}
	return nil
}

// engineEnvironment returns the complete sanitized process environment. Apply it
// before initializing the Go SDK, which launches the pinned CLI subprocess.
func engineEnvironment(ctx context.Context) ([]string, error) {
	return engineEnvironmentWith(ctx, engineCommandOutput, os.Environ())
}

func engineEnvironmentWith(ctx context.Context, execute engineCommand, inherited []string) ([]string, error) {
	present, err := inspectEngine(ctx, execute)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, fmt.Errorf("engine %s is unavailable; run mise run check:engine:setup explicitly", engineName)
	}
	volumePresent, err := inspectEngineVolume(ctx, execute)
	if err != nil {
		return nil, err
	}
	if !volumePresent {
		return nil, fmt.Errorf("engine cache %s is missing", engineVolume)
	}
	version, err := execute(ctx, "dagger", "version")
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(string(version))
	if len(fields) < 2 || fields[0] != "dagger" || fields[1] != "v"+daggerVersion {
		return nil, fmt.Errorf("validation requires Dagger CLI v%s, found %q", daggerVersion, strings.TrimSpace(string(version)))
	}
	return isolatedEngineEnvironment(inherited), nil
}
