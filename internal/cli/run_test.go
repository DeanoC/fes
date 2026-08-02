package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/internal/cli"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type fakeLibrary struct {
	games    []host.Game
	health   protocol.Health
	status   protocol.Status
	launchID string
	err      error
}

func (f *fakeLibrary) Games() []host.Game {
	return f.games
}

func (f *fakeLibrary) Health(context.Context) (protocol.Health, error) {
	return f.health, f.err
}

func (f *fakeLibrary) Status(context.Context) (protocol.Status, error) {
	return f.status, f.err
}

func (f *fakeLibrary) Launch(_ context.Context, id string) (protocol.Status, error) {
	f.launchID = id
	return f.status, f.err
}

func (f *fakeLibrary) Stop(context.Context) (protocol.Status, error) {
	return f.status, f.err
}

func TestGamesHumanAndJSONOutput(t *testing.T) {
	t.Parallel()
	games := []host.Game{
		{ID: "megadrive-test", Title: "Mega Drive test game", System: protocol.SystemMegaDrive},
		{ID: "snes-test", Title: "SNES test game", System: protocol.SystemSNES},
	}
	for _, tt := range []struct {
		name string
		args []string
		json bool
	}{
		{name: "human", args: []string{"--config", "ignored.toml", "games"}},
		{name: "json", args: []string{"--config", "ignored.toml", "--json", "games"}, json: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := cli.Run(context.Background(), tt.args, &stdout, &stderr, func(string) (cli.Library, error) {
				return &fakeLibrary{games: games}, nil
			})
			if exit != 0 || stderr.Len() != 0 {
				t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
			}
			if tt.json {
				var decoded []host.Game
				if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil || len(decoded) != 2 {
					t.Fatalf("JSON=%q error=%v", stdout.String(), err)
				}
				if strings.Count(stdout.String(), "\n") != 1 {
					t.Fatalf("JSON is not one line: %q", stdout.String())
				}
				return
			}
			want := "megadrive-test  Mega Drive  Mega Drive test game\nsnes-test       SNES        SNES test game\n"
			if stdout.String() != want {
				t.Fatalf("output=%q want=%q", stdout.String(), want)
			}
		})
	}
}

func TestRunOperationAndErrorPaths(t *testing.T) {
	t.Parallel()
	activeCore := "MegaDrive"
	active := protocol.Status{State: protocol.StateActive, ObservedCore: &activeCore}
	failedCore := "SNES"
	failed := protocol.Status{State: protocol.StateFailed, ObservedCore: &failedCore, LastError: &protocol.APIError{Code: protocol.CodeCoreTimeout, Message: "/private/rom/path timed out"}}
	tests := []struct {
		name         string
		args         []string
		library      *fakeLibrary
		wantExit     int
		wantStdout   string
		wantStderr   string
		wantLaunchID string
	}{
		{name: "health", args: []string{"health"}, library: &fakeLibrary{health: protocol.Health{Ready: true}}, wantExit: 0, wantStdout: "ready\n"},
		{name: "health not ready", args: []string{"health"}, library: &fakeLibrary{health: protocol.Health{Ready: false}}, wantExit: 1, wantStdout: "not ready\n"},
		{name: "status JSON", args: []string{"--json", "status"}, library: &fakeLibrary{status: active}, wantExit: 0},
		{name: "failed status", args: []string{"status"}, library: &fakeLibrary{status: failed}, wantExit: 0, wantStdout: "failed core=SNES error=CORE_TIMEOUT\n"},
		{name: "launch", args: []string{"launch", "megadrive-test"}, library: &fakeLibrary{status: active}, wantExit: 0, wantStdout: "active: megadrive-test (MegaDrive)\n", wantLaunchID: "megadrive-test"},
		{name: "stop", args: []string{"stop"}, library: &fakeLibrary{status: protocol.Status{State: protocol.StateIdle}}, wantExit: 0, wantStdout: "idle\n"},
		{name: "usage", args: []string{"launch"}, library: &fakeLibrary{}, wantExit: 2, wantStderr: "usage:"},
		{name: "unknown command", args: []string{"scan"}, library: &fakeLibrary{}, wantExit: 2, wantStderr: "usage:"},
		{name: "API error", args: []string{"status"}, library: &fakeLibrary{err: &protocol.APIError{Code: protocol.CodeUnauthorized, Message: "bad token"}}, wantExit: 1, wantStderr: "UNAUTHORIZED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := cli.Run(context.Background(), tt.args, &stdout, &stderr, func(string) (cli.Library, error) {
				return tt.library, nil
			})
			if exit != tt.wantExit {
				t.Fatalf("exit=%d want=%d", exit, tt.wantExit)
			}
			if tt.wantStdout != "" && stdout.String() != tt.wantStdout {
				t.Fatalf("stdout=%q", stdout.String())
			}
			if tt.wantStderr != "" && !strings.Contains(stderr.String(), tt.wantStderr) {
				t.Fatalf("stderr=%q", stderr.String())
			}
			if tt.library.launchID != tt.wantLaunchID {
				t.Fatalf("launch ID=%q", tt.library.launchID)
			}
			if tt.name == "status JSON" {
				var status protocol.Status
				if err := json.Unmarshal(stdout.Bytes(), &status); err != nil || status.State != protocol.StateActive {
					t.Fatalf("status JSON=%q error=%v", stdout.String(), err)
				}
			}
			if tt.name == "failed status" && strings.Contains(stdout.String(), "/private/rom/path") {
				t.Fatalf("failed status leaked daemon message: %q", stdout.String())
			}
		})
	}
}

func TestUsageDoesNotOpenLibrary(t *testing.T) {
	t.Parallel()
	opened := false
	exit := cli.Run(context.Background(), []string{"launch"}, io.Discard, io.Discard, func(string) (cli.Library, error) {
		opened = true
		return nil, errors.New("must not open")
	})
	if exit != 2 || opened {
		t.Fatalf("exit=%d opened=%v", exit, opened)
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var openedPath string
	exit := cli.Run(context.Background(), []string{"games"}, io.Discard, io.Discard, func(path string) (cli.Library, error) {
		openedPath = path
		return &fakeLibrary{}, nil
	})
	if exit != 0 || openedPath != filepath.Join(home, ".config", "mister-remote", "config.toml") {
		t.Fatalf("exit=%d config path=%q", exit, openedPath)
	}
}
