package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/DeanoC/FogCast-POC/host"
	"github.com/DeanoC/FogCast-POC/internal/hil"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, input io.Reader, output, stderr io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "mister-hil: cannot determine the default configuration path")
		return 1
	}
	flags := flag.NewFlagSet("mister-hil", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", filepath.Join(home, ".config", "mister-remote", "config.toml"), "host configuration path")
	reportPath := flags.String("output", filepath.Join("artifacts", "hil", "poc1a.json"), "acceptance report path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}

	connection, err := host.LoadConnection(*configPath)
	if err != nil {
		fmt.Fprintln(stderr, "mister-hil: configuration load failed")
		return 1
	}
	manifest, err := host.LoadManifest(connection.ManifestPath)
	if err != nil {
		fmt.Fprintln(stderr, "mister-hil: manifest load failed")
		return 1
	}
	httpClient := &http.Client{Timeout: connection.RequestTimeout}
	runner := hil.Runner{
		API:             host.NewClient(connection.BaseURL, connection.Token, httpClient),
		UnauthorizedAPI: host.NewClient(connection.BaseURL, "deliberately-invalid-poc-token", httpClient),
		Games:           manifest.Games,
		Prompt:          newTerminalPrompter(input, output),
	}
	report, runErr := runner.Run(ctx)
	if err := writeReport(*reportPath, report); err != nil {
		fmt.Fprintln(stderr, "mister-hil: could not persist the acceptance report")
		return 1
	}
	fmt.Fprintf(output, "Acceptance report: %s\n", *reportPath)
	if runErr != nil {
		fmt.Fprintln(stderr, "mister-hil: acceptance sequence interrupted")
		return 1
	}
	if !report.Passed {
		fmt.Fprintln(stderr, "mister-hil: one or more acceptance checks failed")
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

func writeReport(path string, report hil.Report) (err error) {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".mister-hil-*.json")
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
