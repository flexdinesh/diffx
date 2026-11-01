package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
)

type fakeEngineDocker struct {
	container *engineContainer
	volume    *engineCacheVolume
	calls     []string
	version   string
	failList  bool
}

func validEngineContainer() *engineContainer {
	container := &engineContainer{Name: "/" + engineName}
	container.Config.Image = engineImage
	container.Config.Labels = map[string]string{engineOwnerLabel: "diffx", engineVersionLabel: daggerVersion}
	container.State.Running = true
	container.HostConfig.Privileged = true
	container.HostConfig.NetworkMode = "bridge"
	container.Mounts = append(container.Mounts, struct {
		Type        string
		Name        string
		Destination string
	}{"volume", engineVolume, "/var/lib/dagger"})
	return container
}

func validEngineVolume() *engineCacheVolume {
	return &engineCacheVolume{Name: engineVolume, Driver: "local", Labels: map[string]string{engineOwnerLabel: "diffx", engineVersionLabel: daggerVersion}}
}

func (fake *fakeEngineDocker) execute(_ context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	fake.calls = append(fake.calls, call)
	switch {
	case name == "dagger" && slices.Equal(args, []string{"version"}):
		version := fake.version
		if version == "" {
			version = "dagger v" + daggerVersion + " (registry.dagger.io/engine) linux/amd64"
		}
		return []byte(version), nil
	case slices.Equal(args, []string{"container", "ls", "--all", "--format", "{{.Names}}"}):
		if fake.failList {
			return nil, errors.New("daemon unavailable")
		}
		names := "unrelated-validation-engine\ndagger-engine-v0.20.0\n"
		if fake.container != nil {
			names += engineName + "\n"
		}
		return []byte(names), nil
	case slices.Equal(args, []string{"volume", "ls", "--format", "{{.Name}}"}):
		names := "unrelated-validation-cache\n"
		if fake.volume != nil {
			names += engineVolume + "\n"
		}
		return []byte(names), nil
	case slices.Equal(args, []string{"container", "inspect", engineName}):
		return json.Marshal([]*engineContainer{fake.container})
	case slices.Equal(args, []string{"volume", "inspect", engineVolume}):
		return json.Marshal([]*engineCacheVolume{fake.volume})
	case len(args) > 1 && args[0] == "volume" && args[1] == "create":
		fake.volume = validEngineVolume()
		return []byte(engineVolume), nil
	case len(args) > 0 && args[0] == "run":
		fake.container = validEngineContainer()
		return []byte("created"), nil
	default:
		return nil, errors.New("unexpected command: " + call)
	}
}

func (fake *fakeEngineDocker) assertNoMutations(t *testing.T) {
	t.Helper()
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "docker run ") || strings.HasPrefix(call, "docker volume create ") {
			t.Fatalf("unexpected mutation: %s", call)
		}
	}
}

func TestEngineSetupCreatesOnlyDedicatedResources(t *testing.T) {
	fake := &fakeEngineDocker{}
	if err := setupEngineWith(t.Context(), fake.execute); err != nil {
		t.Fatal(err)
	}
	if fake.container == nil || fake.volume == nil {
		t.Fatal("missing dedicated resources")
	}
	for _, call := range fake.calls {
		if strings.Contains(call, "docker run ") {
			for _, required := range []string{"--name " + engineName, engineImage, "source=" + engineVolume, "--platform linux/amd64", "--restart no"} {
				if !strings.Contains(call, required) {
					t.Fatalf("missing %q in %s", required, call)
				}
			}
			for _, forbidden := range []string{"--rm", "--publish", "--network host", "docker.sock"} {
				if strings.Contains(call, forbidden) {
					t.Fatalf("unexpected %q in %s", forbidden, call)
				}
			}
		}
	}
	fake.calls = nil
	if err := setupEngineWith(t.Context(), fake.execute); err != nil {
		t.Fatal(err)
	}
	fake.assertNoMutations(t)
}

func TestEngineSetupLeavesUnknownOrStoppedResourcesUntouched(t *testing.T) {
	tests := []struct {
		name   string
		change func(*fakeEngineDocker)
	}{
		{"foreign container", func(f *fakeEngineDocker) { f.container.Config.Labels = nil }},
		{"wrong version", func(f *fakeEngineDocker) { f.container.Config.Labels[engineVersionLabel] = "0.20.0" }},
		{"wrong image", func(f *fakeEngineDocker) { f.container.Config.Image = "registry.dagger.io/engine:latest" }},
		{"stopped", func(f *fakeEngineDocker) { f.container.State.Running = false }},
		{"host network", func(f *fakeEngineDocker) { f.container.HostConfig.NetworkMode = "host" }},
		{"foreign volume", func(f *fakeEngineDocker) { f.container = nil; f.volume.Labels = nil }},
		{"unexpected mount", func(f *fakeEngineDocker) { f.container.Mounts[0].Name = "another-project-cache" }},
		{"daemon unavailable", func(f *fakeEngineDocker) { f.failList = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fake := &fakeEngineDocker{container: validEngineContainer(), volume: validEngineVolume()}
			test.change(fake)
			if err := setupEngineWith(t.Context(), fake.execute); err == nil {
				t.Fatal("expected refusal")
			}
			fake.assertNoMutations(t)
		})
	}
}

func TestEngineEnvironmentNeverProvisions(t *testing.T) {
	fake := &fakeEngineDocker{}
	if _, err := engineEnvironmentWith(t.Context(), fake.execute, nil); err == nil {
		t.Fatal("missing engine accepted")
	}
	fake.assertNoMutations(t)
	fake.container = validEngineContainer()
	fake.volume = validEngineVolume()
	fake.version = "dagger v0.20.0"
	if _, err := engineEnvironmentWith(t.Context(), fake.execute, nil); err == nil {
		t.Fatal("wrong CLI version accepted")
	}
	fake.assertNoMutations(t)
}

func TestEngineEnvironmentOverridesInheritedSessions(t *testing.T) {
	fake := &fakeEngineDocker{container: validEngineContainer(), volume: validEngineVolume()}
	env, err := engineEnvironmentWith(t.Context(), fake.execute, []string{
		"PATH=/tools", "DAGGER_SESSION_PORT=9999", "DAGGER_SESSION_TOKEN=foreign",
		"DAGGER_LEAVE_OLD_ENGINE=false", "_EXPERIMENTAL_DAGGER_RUNNER_HOST=docker-image://foreign",
		"DAGGER_CLOUD_TOKEN=foreign", "_EXPERIMENTAL_DAGGER_CLI_BIN=/foreign/cli",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"PATH=/tools", "DAGGER_LEAVE_OLD_ENGINE=true", "_EXPERIMENTAL_DAGGER_RUNNER_HOST=docker-container://" + engineName}
	if !slices.Equal(env, want) {
		t.Fatalf("environment = %v, want %v", env, want)
	}
	fake.assertNoMutations(t)
}
