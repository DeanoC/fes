//go:build !linux

package rebootguard

import (
	"context"
	"errors"
	"time"
)

var ErrUnsupported = errors.New("reboot backstop requires Linux")

type Config struct {
	Device                                     string
	WatchdogTimeout, MinTimeout, FallbackDelay time.Duration
	Marker                                     string
}
type Result struct {
	Armed         *Armed
	ActualTimeout time.Duration
	Reason        string
}
type Armed struct{}

func Arm(context.Context, Config) (Result, error) {
	return Result{Reason: "unsupported platform"}, ErrUnsupported
}
func (*Armed) Cancel() error   { return nil }
func DisarmStale(Config, bool) {}
