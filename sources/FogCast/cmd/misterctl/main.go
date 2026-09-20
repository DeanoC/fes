package main

import (
	"context"
	"net/http"
	"os"

	"github.com/DeanoC/FogCast/host"
	"github.com/DeanoC/FogCast/internal/cli"
)

func main() {
	open := func(path string) (cli.Library, error) {
		return host.Open(path, (*http.Client)(nil))
	}
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, open))
}
