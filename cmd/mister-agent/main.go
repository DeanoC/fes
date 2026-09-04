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

	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/agentconfig"
	"github.com/DeanoC/FogCast/internal/cast"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/input"
	"github.com/DeanoC/FogCast/internal/mister"
	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/internal/targetcache"
	"github.com/DeanoC/FogCast/internal/version"
)

const (
	targetCacheRoot         = "/media/fat/fogcast/cache"
	targetCacheActiveRecord = "/run/fogcast-active.json"
	developmentRBFPath      = "/tmp/fogcast-development/core.rbf"
	rebootCommand           = "/sbin/reboot"
	bootIDFile              = "/proc/sys/kernel/random/boot_id"
	castShutdownTimeout     = 2 * time.Second
)

var errCastShutdown = errors.New("cast controller could not be stopped")

type runtimeBackend string

const (
	runtimeMain   runtimeBackend = "main"
	runtimeNative runtimeBackend = "native"
)

type runDependencies struct {
	openCache             func(targetcache.Config, core.Registry, ...targetcache.Option) (agent.ContentStore, error)
	newRuntime            func(agentconfig.Config, core.Registry) agent.Runtime
	newInput              func(agentconfig.Config) (httpapi.InputController, error)
	inputBeforeInitialize bool
	newCast               func(agentconfig.Config) (httpapi.CastController, error)
	serve                 func(*http.Server) error
}

func run(ctx context.Context, configPath string, backend runtimeBackend, logger *slog.Logger) error {
	dependencies, err := productionRunDependencies(backend)
	if err != nil {
		return err
	}
	return runWithDependencies(ctx, configPath, logger, dependencies)
}

func productionRunDependencies(backend runtimeBackend) (runDependencies, error) {
	return runtimeDependencies(backend, nil)
}

func runtimeDependencies(backend runtimeBackend, nativeControl misterruntime.Control) (runDependencies, error) {
	if backend != runtimeMain && backend != runtimeNative {
		return runDependencies{}, errors.New("target runtime backend is invalid")
	}
	dependencies := runDependencies{
		openCache: func(config targetcache.Config, registry core.Registry, options ...targetcache.Option) (agent.ContentStore, error) {
			return targetcache.Open(config, registry, options...)
		},
		newInput: func(cfg agentconfig.Config) (httpapi.InputController, error) {
			return input.NewTargetControllerWithConfig(cfg.InputListenAddress, cfg.InputUInputPath), nil
		},
		newCast: func(cfg agentconfig.Config) (httpapi.CastController, error) {
			return cast.New(cast.Config{
				Binary: cfg.CastBinary, RTPAddress: cfg.CastRTPAddress, Control: cfg.CastControlAddress,
				Framebuffer: cfg.CastFramebuffer, NativeCmd: cfg.CastNativeCmd, NativeMode: cfg.CastNativeMode,
				TokenFile:   cfg.CastTokenFile,
				Generation:  cfg.CastGeneration,
				StopTimeout: 2 * time.Second,
			}, nil)
		},
		serve: func(server *http.Server) error { return server.ListenAndServe() },
	}
	if backend == runtimeMain {
		dependencies.newRuntime = func(cfg agentconfig.Config, registry core.Registry) agent.Runtime {
			paths := mister.Paths{
				MiSTerProcessComm: cfg.MiSTerProcessComm,
				CommandPipe:       cfg.CommandPipe,
				CoreNameFile:      cfg.CoreNameFile,
				BootIDFile:        bootIDFile,
				MenuRBF:           cfg.MenuRBF,
				MGLDirectory:      cfg.MGLDirectory,
				DevelopmentRBF:    developmentRBFPath,
				RebootCommand:     rebootCommand,
			}
			return mister.NewRuntime(paths, registry, mister.FileCommandWriter{Path: cfg.CommandPipe}, mister.ProcProcessChecker{Root: "/proc"}, 25*time.Millisecond)
		}
		return dependencies, nil
	}
	if nativeControl == nil {
		nativeControl = misterruntime.NewClient(misterruntime.DefaultSocketPath)
	}
	dependencies.newRuntime = func(agentconfig.Config, core.Registry) agent.Runtime {
		return misterruntime.NewRuntime(nativeControl, bootIDFile, 25*time.Millisecond, 250*time.Millisecond)
	}
	dependencies.newInput = func(cfg agentconfig.Config) (httpapi.InputController, error) {
		return input.NewNativeTargetControllerWithConfig(cfg.InputListenAddress, cfg.InputUInputPath)
	}
	dependencies.inputBeforeInitialize = true
	return dependencies, nil
}

func runWithDependencies(ctx context.Context, configPath string, logger *slog.Logger, dependencies runDependencies) (resultErr error) {
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
	var inputController httpapi.InputController
	if dependencies.inputBeforeInitialize && dependencies.newInput != nil {
		inputController, err = dependencies.newInput(cfg)
		if err != nil || inputController == nil {
			return errors.New("remote input controller could not be configured")
		}
		defer inputController.Close()
	}
	runtime := dependencies.newRuntime(cfg, registry)
	coordinator := agent.New(runtime, registry, 10*time.Second, 5*time.Second, agent.WithOperationContext(ctx))
	content := agent.NewContentController(coordinator, cache)
	startup, cancel := context.WithTimeout(ctx, 40*time.Second)
	coordinator.Initialize(startup)
	cancel()
	options := []httpapi.Option{httpapi.WithContent(content), httpapi.WithDevelopment(coordinator)}
	if cfg.CastBinary != "" {
		if dependencies.newCast == nil {
			return errors.New("cast controller could not be configured")
		}
		castController, castErr := dependencies.newCast(cfg)
		if castErr != nil {
			return errors.New("cast controller could not be configured")
		}
		options = append(options, httpapi.WithCast(castController))
		defer func() {
			status := castController.Status(context.Background())
			if status.State != cast.Active {
				return
			}
			shutdown, cancel := context.WithTimeout(context.Background(), castShutdownTimeout)
			stopErr := castController.Stop(shutdown, status.Session, status.Generation)
			cancel()
			if stopErr == nil {
				return
			}

			retry, retryCancel := context.WithTimeout(context.Background(), castShutdownTimeout)
			_ = castController.Stop(retry, status.Session, status.Generation)
			retryCancel()
			resultErr = errCastShutdown
		}()
	}
	if !dependencies.inputBeforeInitialize && dependencies.newInput != nil {
		inputController, err = dependencies.newInput(cfg)
		if err != nil || inputController == nil {
			return errors.New("remote input controller could not be configured")
		}
		defer inputController.Close()
	}
	if inputController != nil {
		options = append(options, httpapi.WithInput(inputController))
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
	configPath := flag.String("config", "/media/fat/fogcast/agent.toml", "target configuration path")
	runtimeValue := flag.String("runtime", string(runtimeMain), "target runtime backend (main or native)")
	flag.Parse()
	backend := runtimeBackend(*runtimeValue)
	if flag.NArg() != 0 || (backend != runtimeMain && backend != runtimeNative) {
		_, _ = fmt.Fprintln(os.Stderr, "usage: mister-agent [--config path] [--runtime main|native]")
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *configPath, backend, logger); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "mister-agent: startup failed")
		os.Exit(1)
	}
}
