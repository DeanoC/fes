package kitsoftware

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSuperviseUntilSignalRetriesThenStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls int
	var waits []time.Duration
	err := SuperviseUntilSignal(ctx, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("disk timed out")
		}
		cancel()
		return errors.New("shutdown raced the failure")
	}, func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("attempts = %d", calls)
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != 2*time.Second {
		t.Fatalf("backoff = %v", waits)
	}
}

func TestSuperviseUntilSignalCleanExitDoesNotRetry(t *testing.T) {
	err := SuperviseUntilSignal(context.Background(), func(context.Context) error {
		return nil
	}, func(context.Context, time.Duration) error {
		t.Fatal("waited after a clean exit")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestNextSupervisorBackoffCaps(t *testing.T) {
	if NextSupervisorBackoff(0) != time.Second || NextSupervisorBackoff(time.Second) != 2*time.Second {
		t.Fatal(NextSupervisorBackoff(0), NextSupervisorBackoff(time.Second))
	}
	if NextSupervisorBackoff(20*time.Second) != 30*time.Second || NextSupervisorBackoff(30*time.Second) != 30*time.Second {
		t.Fatal("backoff did not stay capped")
	}
}
