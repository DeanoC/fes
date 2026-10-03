//go:build !linux

package rebootguard

import (
	"context"
	"errors"
	"time"
)

var ErrUnsupported = errors.New("reboot backstop requires Linux")

type Config struct {
	Device                                               string
	WatchdogTimeout, MinTimeout                          time.Duration
	FallbackStall, FallbackDeadline, FallbackMinDeadline time.Duration
	FallbackSyncWait, FallbackPoll, WatchdogMargin       time.Duration
	Marker                                               string
	BusyWait, BusyPoll                                   time.Duration
}
type Result struct {
	Armed            *Armed
	Fallback         bool
	Watchdog         bool
	ActualTimeout    time.Duration
	FallbackDeadline time.Duration
	Reason           string
}
type Armed struct{}

func Arm(context.Context, Config) (Result, error) {
	return Result{Reason: "unsupported platform"}, ErrUnsupported
}
func (*Armed) Cancel() error   { return nil }
func DisarmStale(Config, bool) {}
