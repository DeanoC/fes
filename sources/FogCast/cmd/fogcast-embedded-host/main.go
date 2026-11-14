//go:build !wasip1 && !tamago

// Command fogcast-embedded-host serves a private, scanned FogCast catalog for
// the native embedded prototype. It uses the installed host's service and HTTP
// handlers. The prototype runner supplies a configuration with targets disabled.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/DeanoC/FogCast/fogcast"
)

func run() (result error) {
	if os.Getenv("FES_EXPERIMENTAL_DE25_EMBEDDED") != "1" {
		return fmt.Errorf("embedded catalog fixture requires FES_EXPERIMENTAL_DE25_EMBEDDED=1")
	}
	config := flag.String("config", "", "private catalog configuration")
	readyFile := flag.String("ready-file", "", "write listener address as JSON when ready")
	flag.Parse()
	if *config == "" || *readyFile == "" {
		return fmt.Errorf("--config and --ready-file are required")
	}
	paths, err := fogcast.PathsForConfig(*config)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	host, err := serveCatalog(ctx, paths)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, host.Close()) }()
	data, err := json.Marshal(struct {
		Base string `json:"base"`
	}{host.Base})
	if err != nil {
		return err
	}
	if err := publishReady(*readyFile, append(data, '\n')); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-host.errors:
		if err == nil {
			return fmt.Errorf("embedded catalog listener stopped")
		}
		return err
	}
}

func publishReady(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".ready-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
