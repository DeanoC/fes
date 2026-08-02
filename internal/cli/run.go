package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/protocol"
)

type Library interface {
	Games() []host.Game
	Health(context.Context) (protocol.Health, error)
	Status(context.Context) (protocol.Status, error)
	Launch(context.Context, string) (protocol.Status, error)
	Stop(context.Context) (protocol.Status, error)
}

type OpenLibrary func(configPath string) (Library, error)

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, open OpenLibrary) int {
	home, err := os.UserHomeDir()
	if err != nil {
		writeOperationError(stderr, err)
		return 1
	}
	flags := flag.NewFlagSet("misterctl", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", filepath.Join(home, ".config", "mister-remote", "config.toml"), "host configuration path")
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		writeUsage(stderr)
		return 2
	}
	commandArgs := flags.Args()
	if !validCommand(commandArgs) {
		writeUsage(stderr)
		return 2
	}
	library, err := open(*configPath)
	if err != nil {
		writeOperationError(stderr, err)
		return 1
	}
	switch commandArgs[0] {
	case "games":
		games := library.Games()
		if *jsonOutput {
			return encodeJSON(stdout, stderr, games)
		}
		writer := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		for _, game := range games {
			_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\n", game.ID, systemLabel(game.System), game.Title)
		}
		if err := writer.Flush(); err != nil {
			writeOperationError(stderr, err)
			return 1
		}
		return 0
	case "health":
		health, err := library.Health(ctx)
		if err != nil {
			writeOperationError(stderr, err)
			return 1
		}
		if *jsonOutput {
			if exit := encodeJSON(stdout, stderr, health); exit != 0 {
				return exit
			}
		} else if health.Ready {
			_, _ = fmt.Fprintln(stdout, "ready")
		} else {
			_, _ = fmt.Fprintln(stdout, "not ready")
		}
		if !health.Ready {
			return 1
		}
		return 0
	case "status":
		status, err := library.Status(ctx)
		if err != nil {
			writeOperationError(stderr, err)
			return 1
		}
		if *jsonOutput {
			return encodeJSON(stdout, stderr, status)
		}
		writeHumanStatus(stdout, status, "")
		return 0
	case "launch":
		gameID := commandArgs[1]
		status, err := library.Launch(ctx, gameID)
		if err != nil {
			writeOperationError(stderr, err)
			return 1
		}
		if *jsonOutput {
			return encodeJSON(stdout, stderr, status)
		}
		writeHumanStatus(stdout, status, gameID)
		return 0
	case "stop":
		status, err := library.Stop(ctx)
		if err != nil {
			writeOperationError(stderr, err)
			return 1
		}
		if *jsonOutput {
			return encodeJSON(stdout, stderr, status)
		}
		writeHumanStatus(stdout, status, "")
		return 0
	default:
		writeUsage(stderr)
		return 2
	}
}

func validCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "games", "health", "status", "stop":
		return len(args) == 1
	case "launch":
		return len(args) == 2
	default:
		return false
	}
}

func encodeJSON(stdout, stderr io.Writer, value any) int {
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		writeOperationError(stderr, err)
		return 1
	}
	return 0
}

func systemLabel(system protocol.System) string {
	if system == protocol.SystemMegaDrive {
		return "Mega Drive"
	}
	return "SNES"
}

func writeHumanStatus(output io.Writer, status protocol.Status, fallbackGameID string) {
	switch status.State {
	case protocol.StateIdle:
		_, _ = fmt.Fprintln(output, "idle")
	case protocol.StateActive:
		gameID := fallbackGameID
		if status.GameID != nil {
			gameID = *status.GameID
		}
		coreName := coreName(status)
		if gameID != "" && coreName != "" {
			_, _ = fmt.Fprintf(output, "active: %s (%s)\n", gameID, coreName)
		} else if coreName != "" {
			_, _ = fmt.Fprintf(output, "active core=%s\n", coreName)
		} else {
			_, _ = fmt.Fprintln(output, "active")
		}
	case protocol.StateFailed:
		_, _ = fmt.Fprint(output, "failed")
		if coreName := coreName(status); coreName != "" {
			_, _ = fmt.Fprintf(output, " core=%s", coreName)
		}
		if status.LastError != nil {
			_, _ = fmt.Fprintf(output, " error=%s", status.LastError.Code)
		}
		_, _ = fmt.Fprintln(output)
	default:
		_, _ = fmt.Fprintln(output, status.State)
	}
}

func coreName(status protocol.Status) string {
	if status.ObservedCore != nil {
		return *status.ObservedCore
	}
	if status.ExpectedCore != nil {
		return *status.ExpectedCore
	}
	return ""
}

func writeOperationError(stderr io.Writer, err error) {
	var apiErr *protocol.APIError
	if errors.As(err, &apiErr) {
		_, _ = fmt.Fprintf(stderr, "%s: %s\n", apiErr.Code, apiErr.Message)
		return
	}
	_, _ = fmt.Fprintf(stderr, "error: %v\n", err)
}

func writeUsage(stderr io.Writer) {
	_, _ = fmt.Fprintln(stderr, "usage: misterctl [--config path] [--json] {games|health|status|launch <game-id>|stop}")
}
