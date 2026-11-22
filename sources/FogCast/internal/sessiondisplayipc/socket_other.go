//go:build !linux

package sessiondisplayipc

import "context"

type DisplayGuard struct{}

func AcquireDisplayGuard(context.Context, string) (*DisplayGuard, error) { return nil, ErrUnsupported }
func (*DisplayGuard) Release() error                                     { return ErrUnsupported }

type Prepared struct{}

func Prepare(context.Context, string, Transition) (*Prepared, bool, error) {
	return nil, false, ErrUnsupported
}
func (*Prepared) Finish(context.Context, Outcome) error { return ErrUnsupported }
func ListenLauncher(ctx context.Context, _ string, _ uint32, _ func(context.Context, Transition) (func(context.Context, Outcome) error, error), ready chan<- error) error {
	if ready != nil {
		select {
		case ready <- ErrUnsupported:
		case <-ctx.Done():
		}
	}
	return ErrUnsupported
}
