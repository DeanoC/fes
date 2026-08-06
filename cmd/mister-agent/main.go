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
	"github.com/DeanoC/FogCast-POC/internal/input"
	"github.com/DeanoC/FogCast-POC/internal/mister"
	"github.com/DeanoC/FogCast-POC/internal/targetcache"
	"github.com/DeanoC/FogCast-POC/internal/version"
)

const (
	targetCacheRoot         = "/media/fat/fogcast/cache"
	targetCacheActiveRecord = "/run/fogcast-active.json"
)

type runDependencies struct {
	openCache  func(targetcache.Config, core.Registry, ...targetcache.Option) (agent.ContentStore, error)
	newRuntime func(agentconfig.Config, core.Registry) agent.Runtime
	newInput   func(agentconfig.Config) httpapi.InputController
	serve      func(*http.Server) error
}

func run(ctx context.Context, configPath string, logger *slog.Logger) error {
	return runWithDependencies(ctx, configPath, logger, productionRunDependencies())
}

func productionRunDependencies() runDependencies {
	return runDependencies{
		openCache: func(config targetcache.Config, registry core.Registry, options ...targetcache.Option) (agent.ContentStore, error) {
			return targetcache.Open(config, registry, options...)
		},
		newRuntime: func(cfg agentconfig.Config, registry core.Registry) agent.Runtime {
			paths := mister.Paths{
				MiSTerProcessComm: cfg.MiSTerProcessComm,
				CommandPipe:       cfg.CommandPipe,
				CoreNameFile:      cfg.CoreNameFile,
				MenuRBF:           cfg.MenuRBF,
				MGLDirectory:      cfg.MGLDirectory,
			}
			return mister.NewRuntime(paths, registry, mister.FileCommandWriter{Path: cfg.CommandPipe}, mister.ProcProcessChecker{Root: "/proc"}, 25*time.Millisecond)
		},
		newInput: func(cfg agentconfig.Config) httpapi.InputController {
			return input.NewTargetControllerWithConfig(cfg.InputListenAddress, cfg.InputUInputPath)
		},
		serve: func(server *http.Server) error { return server.ListenAndServe() },
	}
}

func runWithDependencies(ctx context.Context, configPath string, logger *slog.Logger, dependencies runDependencies) error {
	cfg, err := agentconfig.Load(configPath)
	if err != nil {
		return errors.New("target configuration could not be loaded")
	}
	registry := core.DefaultRegistry()
	cache, err := dependencies.openCache(targetcache.Config{
		Root:         targetCacheRoot,
		ActiveRecord: targetCacheActiveRecord,
		MaxBytes:     cfg.CacheMaxBytes,
	}, registry, targetcache.WithLogger(logger))
	if err != nil {
		return errors.New("target cache could not be opened")
	}
	runtime := dependencies.newRuntime(cfg, registry)
	coordinator := agent.New(runtime, registry, 10*time.Second, 5*time.Second)
	content := agent.NewContentController(coordinator, cache)
	startup, cancel := context.WithTimeout(ctx, 40*time.Second)
	coordinator.Initialize(startup)
	cancel()
	options := []httpapi.Option{httpapi.WithContent(content)}
	var inputController httpapi.InputController
	if dependencies.newInput != nil {
		inputController = dependencies.newInput(cfg)
		if inputController != nil {
			options = append(options, httpapi.WithInput(inputController))
			defer inputController.Close()
		}
	}
	handler := httpapi.New(coordinator, cfg.Token, version.Version, logger, options...)
	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       75 * time.Second,
		WriteTimeout:      75 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	err = dependencies.serve(server)
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
