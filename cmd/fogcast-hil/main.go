// Command fogcast-hil runs the operator-assisted FogCast POC 2 acceptance
// sequence. It never reboots the target, changes mounts, restarts a process,
// or interrupts an upload itself; each such gate is an explicit operator
// confirmation.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/internal/hil"
)

var _ hil.POC2Service = (*fogcast.Service)(nil)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, input io.Reader, output, stderr io.Writer) int {
	if ctx == nil {
		ctx = context.Background()
	}
	defaults, err := fogcast.DefaultPaths()
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-hil: cannot determine default configuration path")
		return 1
	}
	flags := flag.NewFlagSet("fogcast-hil", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", defaults.Config, "FogCast configuration path")
	reportPath := flags.String("output", filepath.Join("artifacts", "hil", "poc2.json"), "acceptance report path")
	sonicID := flags.String("sonic-id", "", "explicit Mega Drive game ID")
	marioID := flags.String("mario-id", "", "explicit SNES game ID")
	uncachedID := flags.String("uncached-id", "", "explicit uncached fixture game ID (required)")
	interruptedID := flags.String("interrupted-id", "", "explicit upload-interruption fixture game ID (required)")
	uploadThrottleMS := flags.Int("upload-throttle-ms", 0, "delay each upload body read by this many milliseconds")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	if strings.TrimSpace(*sonicID) == "" || strings.TrimSpace(*marioID) == "" || strings.TrimSpace(*uncachedID) == "" || strings.TrimSpace(*interruptedID) == "" {
		fmt.Fprintln(stderr, "fogcast-hil: --sonic-id, --mario-id, --uncached-id, and --interrupted-id are required; source names and paths are never embedded")
		return 2
	}
	if *uploadThrottleMS < 0 {
		fmt.Fprintln(stderr, "fogcast-hil: --upload-throttle-ms must not be negative")
		return 2
	}
	service, err := fogcast.Open(ctx, fogcast.Paths{Config: *configPath, Index: defaults.Index, Staging: defaults.Staging}, nil)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-hil: configuration or service load failed")
		return 1
	}
	defer service.Close()
	service.SetUploadReadDelay(time.Duration(*uploadThrottleMS) * time.Millisecond)
	debugLog := log.New(stderr, "fogcast-hil: ", log.LstdFlags)
	service.SetDebug(func(message string) {
		if os.Getenv("FOGCAST_DEBUG") == "1" {
			debugLog.Println(message)
		}
	})
	prompt := newTerminalPrompter(input, output)
	runner := hil.POC2Runner{
		Service: service, Prompt: prompt,
		Sabotage: terminalSabotage{prompt: prompt},
		Debug: func(message string) {
			if os.Getenv("FOGCAST_DEBUG") == "1" {
				debugLog.Println(message)
			}
		},
		SonicID: *sonicID, MarioID: *marioID,
		UncachedID: *uncachedID, InterruptedID: *interruptedID,
	}
	report, runErr := runner.Run(ctx)
	if err := writeReport(*reportPath, report); err != nil {
		fmt.Fprintln(stderr, "fogcast-hil: could not persist the acceptance report")
		return 1
	}
	fmt.Fprintf(output, "Acceptance report: %s\n", *reportPath)
	if runErr != nil {
		fmt.Fprintln(stderr, "fogcast-hil: acceptance sequence interrupted")
		return 1
	}
	if !report.Passed {
		fmt.Fprintln(stderr, "fogcast-hil: one or more acceptance checks failed")
		return 1
	}
	return 0
}

type terminalPrompter struct {
	scanner *bufio.Scanner
	output  io.Writer
}

func newTerminalPrompter(input io.Reader, output io.Writer) *terminalPrompter {
	return &terminalPrompter{scanner: bufio.NewScanner(input), output: output}
}

func (p *terminalPrompter) Confirm(message string) (bool, error) {
	if _, err := fmt.Fprintf(p.output, "%s [y/N]: ", message); err != nil {
		return false, err
	}
	if !p.scanner.Scan() {
		if err := p.scanner.Err(); err != nil {
			return false, err
		}
		return false, io.EOF
	}
	answer := strings.ToLower(strings.TrimSpace(p.scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

func (p *terminalPrompter) ConfirmGate(id, message string) (bool, error) {
	if _, err := fmt.Fprintf(p.output, "GATE %s: %s [y/N]: ", id, message); err != nil {
		return false, err
	}
	if !p.scanner.Scan() {
		if err := p.scanner.Err(); err != nil {
			return false, err
		}
		return false, io.EOF
	}
	answer := strings.ToLower(strings.TrimSpace(p.scanner.Text()))
	if answer != "y" && answer != "yes" && answer != "n" && answer != "no" {
		return false, fmt.Errorf("gate %s requires y, yes, n, or no", id)
	}
	return answer == "y" || answer == "yes", nil
}

type terminalSabotage struct{ prompt *terminalPrompter }

func (s terminalSabotage) RebootTarget(_ context.Context) (bool, error) {
	return s.prompt.Confirm("Perform the target reboot manually, wait for the agent, then confirm")
}
func (s terminalSabotage) RestartAgent(_ context.Context) (bool, error) {
	return s.prompt.Confirm("Restart the target agent manually, then confirm")
}
func (s terminalSabotage) ToggleNAS(_ context.Context) (bool, error) {
	return s.prompt.Confirm("Unmount the two source roots manually, then confirm NAS-offline state")
}
func (s terminalSabotage) RemountNAS(_ context.Context) (bool, error) {
	return s.prompt.Confirm("Remount the two source roots manually, then confirm NAS-online state")
}
func (s terminalSabotage) InterruptUpload(_ context.Context) (bool, error) {
	return s.prompt.Confirm("Interrupt the in-flight upload manually, then confirm")
}

func writeReport(path string, report hil.Report) (err error) {
	encoded, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	if err := validateReportPrivacy(encoded); err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refuse to publish report through a symbolic link")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect report path: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".fogcast-hil-*.json")
	if err != nil {
		return fmt.Errorf("create temporary report: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set report mode: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(report); err != nil {
		return fmt.Errorf("encode report: %w", err)
	}
	if err = temporary.Sync(); err != nil {
		return fmt.Errorf("sync report: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return fmt.Errorf("close report: %w", err)
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish report: %w", err)
	}
	return nil
}

func validateReportPrivacy(encoded []byte) error {
	text := strings.ToLower(string(encoded))
	for _, forbidden := range []string{
		"bearer ", "token =", "/volumes/", "/media/fat/", "sqlite3", "/staging/", "/cache/",
		".sfc", ".smc", ".gen", ".md", ".zip",
	} {
		if strings.Contains(text, forbidden) {
			return errors.New("acceptance report contains private operational data")
		}
	}
	return nil
}
