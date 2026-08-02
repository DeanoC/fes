package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast-POC/internal/agent"
	"github.com/DeanoC/FogCast-POC/internal/agentconfig"
	"github.com/DeanoC/FogCast-POC/internal/core"
	"github.com/DeanoC/FogCast-POC/internal/httpapi"
	"github.com/DeanoC/FogCast-POC/internal/mister"
	"github.com/DeanoC/FogCast-POC/internal/version"
)

func run(ctx context.Context, configPath string, logger *slog.Logger) error {
	cfg, err := agentconfig.Load(configPath)
	if err != nil {
		return errors.New("target configuration could not be loaded")
	}
	paths := mister.Paths{
		MiSTerProcessComm: cfg.MiSTerProcessComm,
		CommandPipe:       cfg.CommandPipe,
		CoreNameFile:      cfg.CoreNameFile,
		MenuRBF:           cfg.MenuRBF,
		MGLDirectory:      cfg.MGLDirectory,
	}
	registry := core.DefaultRegistry()
	runtime := mister.NewRuntime(paths, registry, mister.FileCommandWriter{Path: cfg.CommandPipe}, mister.ProcProcessChecker{Root: "/proc"}, 25*time.Millisecond)
	coordinator := agent.New(runtime, registry, 10*time.Second, 5*time.Second)
	startup, cancel := context.WithTimeout(ctx, 40*time.Second)
	coordinator.Initialize(startup)
	cancel()
	handler := httpapi.New(coordinator, cfg.Token, version.Version, logger)
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func main() {
	configPath := flag.String("config", "/media/fat/mister-remote/agent.toml", "target configuration path")
	flag.Parse()
	if flag.NArg() != 0 {
		_, _ = fmt.Fprintln(os.Stderr, "usage: mister-agent [--config path]")
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *configPath, logger); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "mister-agent: startup failed")
		os.Exit(1)
	}
}
