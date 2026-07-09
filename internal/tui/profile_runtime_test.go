package tui

import (
	"testing"

	"dbtool/internal/config"
)

func TestDetectRuntimeProfilePrefersRunningDockerContainer(t *testing.T) {
	origDocker := dockerRuntimeProfileDetector
	origCompose := composeRuntimeProfileDetector
	defer func() {
		dockerRuntimeProfileDetector = origDocker
		composeRuntimeProfileDetector = origCompose
	}()

	composeCalled := false
	dockerRuntimeProfileDetector = func(input config.Profile, dir string) (config.Profile, string, error) {
		input.Runtime = &config.RuntimeProfile{Type: "docker", Source: "docker-cli", Container: "pg-live"}
		input.Port = 55432
		return input, "Runtime: docker • docker-cli • container pg-live", nil
	}
	composeRuntimeProfileDetector = func(input config.Profile, dir string) (config.Profile, string, error) {
		composeCalled = true
		input.Runtime = &config.RuntimeProfile{Type: "docker", Source: "docker-compose", ServiceName: "postgres"}
		return input, "Runtime: docker • docker-compose • service postgres", nil
	}

	input := config.Profile{Driver: "postgres", Host: "localhost", Port: 5432, Database: "app", User: "postgres"}
	got, msg, err := detectRuntimeProfile(input, ".", nil)
	if err != nil {
		t.Fatalf("detectRuntimeProfile error: %v", err)
	}
	if composeCalled {
		t.Fatalf("compose detector should not run when docker runtime already matched")
	}
	if got.Runtime == nil || got.Runtime.Source != "docker-cli" {
		t.Fatalf("runtime = %#v, want docker-cli match", got.Runtime)
	}
	if got.Port != 55432 {
		t.Fatalf("port = %d, want 55432", got.Port)
	}
	if msg != "Runtime: docker • docker-cli • container pg-live" {
		t.Fatalf("message = %q", msg)
	}
}

func TestDetectRuntimeProfileFallsBackToComposeWhenDockerHasNoMatch(t *testing.T) {
	origDocker := dockerRuntimeProfileDetector
	origCompose := composeRuntimeProfileDetector
	defer func() {
		dockerRuntimeProfileDetector = origDocker
		composeRuntimeProfileDetector = origCompose
	}()

	dockerRuntimeProfileDetector = func(input config.Profile, dir string) (config.Profile, string, error) {
		return input, "No running Docker database containers matched the current form values; checking compose files.", nil
	}
	composeRuntimeProfileDetector = func(input config.Profile, dir string) (config.Profile, string, error) {
		input.Runtime = &config.RuntimeProfile{Type: "docker", Source: "docker-compose", ServiceName: "postgres"}
		input.Database = "app"
		return input, "Runtime: docker • docker-compose • service postgres", nil
	}

	input := config.Profile{Driver: "postgres", Host: "localhost", Port: 5432, User: "postgres"}
	got, msg, err := detectRuntimeProfile(input, ".", nil)
	if err != nil {
		t.Fatalf("detectRuntimeProfile error: %v", err)
	}
	if got.Runtime == nil || got.Runtime.Source != "docker-compose" {
		t.Fatalf("runtime = %#v, want docker-compose fallback", got.Runtime)
	}
	if msg != "Runtime: docker • docker-compose • service postgres" {
		t.Fatalf("message = %q", msg)
	}
}
