// Command fogcast-tenfoot is the native SDL3 10-foot FogCast launcher.
//
// It is a host API client. It does not talk to the target agent or
// /dev/MiSTer_cmd.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/DeanoC/FogCast/host/tenfoot"
)

func init() {
	runtime.LockOSThread()
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	_ = stdout
	opts, err := parseArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := tenfoot.Run(ctx, opts); err != nil {
		fmt.Fprintf(stderr, "fogcast-tenfoot: %v\n", err)
		return 1
	}
	return 0
}

func parseArgs(args []string) (tenfoot.Options, error) {
	fs := flag.NewFlagSet("fogcast-tenfoot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	api := fs.String("api", envOr("FOGCAST_API", tenfoot.DefaultAPIBase), "FogCast host API base URL")
	width := fs.Int("width", 1280, "window width")
	height := fs.Int("height", 720, "window height")
	fullscreen := fs.Bool("fullscreen", false, "run fullscreen")
	smoke := fs.Bool("smoke", false, "run the Mac proof and exit")
	maxGames := fs.Int("max-games", 0, "optional catalog cap")
	timeout := fs.Duration("smoke-timeout", 45*time.Second, "smoke deadline")
	safeArea := fs.Float64("safe-area", tenfoot.DefaultSafeAreaPct, "TV overscan inset as a fraction of each edge (0-0.2)")
	noAttract := fs.Bool("no-attract", envTruthy("FOGCAST_TENFOOT_NO_ATTRACT"), "disable attract mode")
	if err := fs.Parse(args); err != nil {
		return tenfoot.Options{}, err
	}
	safeAreaSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "safe-area" {
			safeAreaSet = true
		}
	})
	return tenfoot.Options{
		APIBase:      *api,
		Width:        *width,
		Height:       *height,
		Fullscreen:   *fullscreen,
		Smoke:        *smoke,
		MaxGames:     *maxGames,
		SmokeTimeout: *timeout,
		SafeAreaPct:  *safeArea,
		SafeAreaSet:  safeAreaSet,
		NoAttract:    *noAttract,
	}, nil
}

func envTruthy(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
