package tenfoot

import (
	"context"
	"strings"
	"time"
)

// Options configure the native launcher window.
type Options struct {
	APIBase      string
	Width        int
	Height       int
	Fullscreen   bool
	Hidden       bool
	Smoke        bool
	MaxGames     int
	SmokeTimeout time.Duration
}

func (o Options) normalized() Options {
	if strings.TrimSpace(o.APIBase) == "" {
		o.APIBase = DefaultAPIBase
	}
	if o.Width <= 0 {
		o.Width = 1280
	}
	if o.Height <= 0 {
		o.Height = 720
	}
	if o.MaxGames <= 0 {
		o.MaxGames = defaultMaxGames
	}
	if o.Smoke && o.SmokeTimeout <= 0 {
		o.SmokeTimeout = 45 * time.Second
	}
	if o.Smoke {
		o.Hidden = true
		if o.MaxGames > 400 {
			o.MaxGames = 400
		}
	}
	return o
}

// Run starts the native launcher. SDL3 builds use the window backend.
func Run(ctx context.Context, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return runWindow(ctx, opts.normalized())
}
