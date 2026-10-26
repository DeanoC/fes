// fes-bluetooth is the base-image pairing agent. It is not part of the
// replaceable kit-software bundle: a kit gains it when a new OS image boots.
package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/DeanoC/fes/platform/internal/btagent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	var lastLog time.Time
	var lastErr string
	for ctx.Err() == nil {
		err := btagent.Serve(ctx)
		if ctx.Err() != nil {
			return
		}
		msg := err.Error()
		if msg != lastErr || time.Since(lastLog) >= 30*time.Second {
			log.Print(err)
			lastLog = time.Now()
			lastErr = msg
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
