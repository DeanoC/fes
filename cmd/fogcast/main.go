package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/DeanoC/FogCast-POC/fogcast"
	"github.com/DeanoC/FogCast-POC/internal/fogcastcli"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer, open fogcastcli.OpenService) int {
	return fogcastcli.Run(ctx, args, stdout, stderr, open)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	open := func(ctx context.Context, paths fogcast.Paths) (fogcastcli.Service, error) {
		return fogcast.Open(ctx, paths, (*http.Client)(nil))
	}
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, open))
}
