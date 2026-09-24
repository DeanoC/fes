package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/fogcastcli"
)

func run(ctx context.Context, args []string, stdout, stderr io.Writer, open fogcastcli.OpenService) int {
	return fogcastcli.Run(ctx, args, stdout, stderr, open)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	open := func(ctx context.Context, paths fogcast.Paths) (fogcastcli.Service, error) {
		opened, err := fogcast.Open(ctx, paths, (*http.Client)(nil))
		if err != nil {
			return nil, err
		}
		opened.EnableMeshContent()
		return opened, nil
	}
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr, open))
}
