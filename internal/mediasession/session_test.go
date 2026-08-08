package mediasession

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeComponent struct {
	name     string
	order    *[]string
	startErr error

	stopErr         error
	stopBlock       <-chan struct{}
	stopCtxCanceled bool
	stopRemaining   time.Duration
	mu              sync.Mutex
	starts          int
	stops           int
}

func (f *fakeComponent) Start(_ context.Context, gameID string) (ComponentHandle, error) {
	f.mu.Lock()
	f.starts++
	if f.order != nil {
		*f.order = append(*f.order, "start:"+f.name+":"+gameID)
	}
	f.mu.Unlock()
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &fakeHandle{component: f}, nil
}

func (f *fakeComponent) stop(ctx context.Context) error {
	f.mu.Lock()
	f.stops++
	f.stopCtxCanceled = ctx.Err() != nil
	if deadline, ok := ctx.Deadline(); ok {
		f.stopRemaining = time.Until(deadline)
	}
	if f.order != nil {
		*f.order = append(*f.order, "stop:"+f.name)
	}
	f.mu.Unlock()
	if f.stopBlock != nil {
		select {
		case <-f.stopBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.stopErr
}

type fakeHandle struct{ component *fakeComponent }

func (h *fakeHandle) Stop(ctx context.Context) error { return h.component.stop(ctx) }

func TestStartStartsReceiverBeforeSenderAndStopReversesOrder(t *testing.T) {
	order := []string{}
	receiver := &fakeComponent{name: "receiver", order: &order}
	sender := &fakeComponent{name: "sender", order: &order}
	session := New(sender, receiver)

	handle, err := session.Start(context.Background(), "game-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := "start:receiver:game-1,start:sender:game-1,stop:sender,stop:receiver"
	if got := strings.Join(order, ","); got != want {
		t.Fatalf("lifecycle order = %q, want %q", got, want)
	}
}

func TestStartFailureStopsAlreadyStartedComponentAndSanitizesError(t *testing.T) {
	order := []string{}
	secret := "token=super-secret /private/capture"
	receiver := &fakeComponent{name: "receiver", order: &order}
	sender := &fakeComponent{name: "sender", order: &order, startErr: errors.New(secret)}
	session := New(sender, receiver, WithStopTimeout(100*time.Millisecond))

	_, err := session.Start(context.Background(), "game-1")
	if err == nil {
		t.Fatal("Start returned nil error")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "/private") {
		t.Fatalf("Start leaked component detail: %v", err)
	}
	if got := strings.Join(order, ","); got != "start:receiver:game-1,start:sender:game-1,stop:receiver" {
		t.Fatalf("failure cleanup order = %q", got)
	}
}

func TestHandleStopRetriesOnlyFailedComponentsAndRemainsBounded(t *testing.T) {
	never := make(chan struct{})
	receiver := &fakeComponent{name: "receiver", stopBlock: never}
	sender := &fakeComponent{name: "sender", stopBlock: never}
	session := New(sender, receiver, WithStopTimeout(20*time.Millisecond))
	handle, err := session.Start(context.Background(), "game-1")
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	err = handle.Stop(context.Background())
	if err == nil {
		t.Fatal("Stop returned nil for timed-out components")
	}
	if elapsed := time.Since(started); elapsed > 200*time.Millisecond {
		t.Fatalf("Stop was not bounded: %s", elapsed)
	}
	if secondErr := handle.Stop(context.Background()); !errors.Is(secondErr, ErrComponentStop) {
		t.Fatalf("second Stop = %v, want retryable component stop error", secondErr)
	}
	sender.mu.Lock()
	senderStops := sender.stops
	sender.mu.Unlock()
	receiver.mu.Lock()
	receiverStops := receiver.stops
	receiver.mu.Unlock()
	if senderStops != 2 || receiverStops != 2 {
		t.Fatalf("stop counts sender=%d receiver=%d, want two retries each", senderStops, receiverStops)
	}
}

func TestStartCleanupFailureReturnsRetryablePartialHandle(t *testing.T) {
	receiver := &fakeComponent{name: "receiver", stopErr: errors.New("cleanup failed")}
	sender := &fakeComponent{name: "sender", startErr: errors.New("start failed")}
	session := New(sender, receiver, WithStopTimeout(100*time.Millisecond))
	handle, err := session.Start(context.Background(), "game-1")
	if !errors.Is(err, ErrComponentStart) || handle == nil {
		t.Fatalf("Start = handle %v err %v, want retryable partial handle", handle, err)
	}
	receiver.mu.Lock()
	receiver.stopErr = nil
	receiver.mu.Unlock()
	if err := handle.Stop(context.Background()); err != nil {
		t.Fatalf("retry partial cleanup = %v", err)
	}
	receiver.mu.Lock()
	stops := receiver.stops
	receiver.mu.Unlock()
	if stops != 2 {
		t.Fatalf("receiver stops = %d, want initial cleanup plus retry", stops)
	}
}

func TestStartFailureCleansUpWhenCallerContextIsCanceled(t *testing.T) {
	order := []string{}
	receiver := &fakeComponent{name: "receiver", order: &order}
	sender := &fakeComponent{name: "sender", order: &order, startErr: errors.New("private sender detail")}
	session := New(sender, receiver, WithStopTimeout(100*time.Millisecond))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := session.Start(ctx, "game-1")
	if err == nil {
		t.Fatal("Start returned nil error")
	}
	if got := strings.Join(order, ","); got != "start:receiver:game-1,start:sender:game-1,stop:receiver" {
		t.Fatalf("canceled-context cleanup order = %q", got)
	}
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	if receiver.stopCtxCanceled {
		t.Fatal("startup cleanup used the canceled caller context")
	}
}

func TestStartRejectsEmptyGameIDWithoutStartingComponents(t *testing.T) {
	receiver := &fakeComponent{name: "receiver"}
	sender := &fakeComponent{name: "sender"}
	_, err := New(sender, receiver).Start(context.Background(), " ")
	if err == nil || !strings.Contains(err.Error(), "game id is required") {
		t.Fatalf("Start error = %v", err)
	}
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	if receiver.starts != 0 {
		t.Fatalf("receiver started %d times", receiver.starts)
	}
}

func TestStartRejectsNilComponents(t *testing.T) {
	if _, err := New(nil, &fakeComponent{name: "receiver"}).Start(context.Background(), "game"); err == nil {
		t.Fatal("nil sender accepted")
	}
	if _, err := New(&fakeComponent{name: "sender"}, nil).Start(context.Background(), "game"); err == nil {
		t.Fatal("nil receiver accepted")
	}
}

func TestStopGivesEachComponentAFreshCleanupDeadline(t *testing.T) {
	never := make(chan struct{})
	receiver := &fakeComponent{name: "receiver"}
	sender := &fakeComponent{name: "sender", stopBlock: never}
	handle, err := New(sender, receiver, WithStopTimeout(30*time.Millisecond)).Start(context.Background(), "game-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(context.Background()); err == nil {
		t.Fatal("Stop returned nil for timed-out sender")
	}
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	if receiver.stopCtxCanceled || receiver.stopRemaining < 15*time.Millisecond {
		t.Fatalf("receiver cleanup context canceled=%v remaining=%v, want a fresh deadline", receiver.stopCtxCanceled, receiver.stopRemaining)
	}
}
