// Command fogcast-api serves the local FogCast application API for POC3 clients.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/internal/hostapi"
)

type service interface {
	hostapi.Service
	Close() error
}

type openService func(context.Context, fogcast.Paths) (service, error)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	open := func(ctx context.Context, paths fogcast.Paths) (service, error) {
		return fogcast.Open(ctx, paths, nil)
	}
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, open))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, open openService) int {
	flags := flag.NewFlagSet("fogcast-api", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", "127.0.0.1:8787", "loopback HTTP listen address")
	configPath := flags.String("config", "", "FogCast configuration path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	address, err := normalizeListenAddress(*listen)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: listen address must be loopback")
		return 2
	}
	if open == nil {
		fmt.Fprintln(stderr, "fogcast-api: service opener is unavailable")
		return 1
	}
	paths, err := fogcast.DefaultPaths()
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: cannot determine default paths")
		return 1
	}
	if strings.TrimSpace(*configPath) != "" {
		paths.Config = *configPath
	}
	fogcastService, err := open(ctx, paths)
	if err != nil || fogcastService == nil {
		fmt.Fprintln(stderr, "fogcast-api: service load failed")
		return 1
	}
	defer fogcastService.Close()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		fmt.Fprintln(stderr, "fogcast-api: listen failed")
		return 1
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           hostapi.New(fogcastService),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.Serve(listener) }()
	fmt.Fprintf(stdout, "FogCast API listening on http://%s\n", listener.Addr())

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintln(stderr, "fogcast-api: shutdown failed")
			return 1
		}
		if err := <-serveErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, "fogcast-api: server failed")
			return 1
		}
		return 0
	case err := <-serveErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(stderr, "fogcast-api: server failed")
			return 1
		}
		return 0
	}
}

func normalizeListenAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		return "", errors.New("invalid listen address")
	}
	if strings.EqualFold(host, "localhost") {
		return net.JoinHostPort("127.0.0.1", port), nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return "", errors.New("listen address is not loopback")
	}
	return net.JoinHostPort(ip.String(), port), nil
}
