package input

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/bridge"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/localhid"
	"github.com/DeanoC/FogCast/internal/zx81keys"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/remoteinput"
)

func TestReaderRetriesFailedDepartureWithoutFurtherInput(t *testing.T) {
	f := newV2MatrixFixture(t, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	obs := CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
	if err := f.c.ports.setObservationContext(context.Background(), obs); err != nil {
		t.Fatal(err)
	}
	var fail atomic.Bool
	posts := make(chan uint64, 8)
	f.c.ports.keys.SetPoster(func(_ context.Context, post localhid.MatrixPost, v uint64) error {
		posts <- v
		if fail.Load() {
			return errors.New("ambiguous neutral")
		}
		if post.NeutralOf != nil && !post.NeutralOf.Valid() {
			t.Error("invalid neutral witness")
		}
		return nil
	})
	if err := f.c.ports.apply(sourceRemote, keyboardFrameFor(zx81keys.Letter('J'), remoteinput.ActionPress)); err != nil {
		t.Fatal(err)
	}
	<-posts
	reader, writer := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer writer.Close()
	done := make(chan struct{})
	go func() { defer close(done); f.c.readLocal(ctx, reader, false) }()
	send := func(frame protocol.InputFrame, seq uint32) {
		t.Helper()
		frame.Seq = seq
		if err := bridge.WriteFrame(writer, frame); err != nil {
			t.Fatal(err)
		}
	}
	send(gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0), 1)
	<-posts
	send(gamepad(1, remoteinput.ButtonDPadDown, remoteinput.ActionPress, 0), 2)
	<-posts
	fail.Store(true)
	send(localPlayerDeparture(0), 3)
	select {
	case <-posts:
	case <-time.After(time.Second):
		t.Fatal("departure was not attempted")
	}
	fail.Store(false)
	select {
	case got := <-posts:
		want := zx81keys.Matrix(map[remoteinput.Code]bool{zx81keys.Letter('J'): true, zx81keys.Letter('X'): true})
		if got != want {
			t.Fatalf("retry lost unrelated contributors or replayed departed key: %#x", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no neutral retry without further input")
	}
	cancel()
	_ = writer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reader did not stop")
	}
}

func TestDepartureRetryLeaseCleanupProof(t *testing.T) {
	for _, confirmed := range []bool{true, false} {
		t.Run(fmt.Sprintf("confirmed-%v", confirmed), func(t *testing.T) {
			f := newV2MatrixFixture(t, strings.Repeat("b", 64))
			var callbacks atomic.Int32
			var fail atomic.Bool
			manager := kitlease.New(time.Minute, func(ctx context.Context) error {
				if callbacks.Add(1) == 1 {
					return nil
				}
				if !confirmed {
					return errors.New("cleanup failed")
				}
				fail.Store(false)
				if err := f.c.ReleaseAll(ctx); err != nil {
					return err
				}
				kitlease.ConfirmInputCleanup(ctx)
				return nil
			})
			t.Cleanup(manager.Close)
			f.c.SetLocalLeaseManager(manager)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wait, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			if _, err := manager.Settled(wait); err != nil {
				t.Fatal(err)
			}
			lease, err := manager.ClaimLocal()
			if err != nil {
				t.Fatal(err)
			}
			f.req.LeaseGeneration = lease.Status.Generation
			testEnableRecovery(t, f.c)
			f.c.localV2Guard.Store(true)
			obs := CoreObservation{Active: true, Keyboard: true, CoreID: "fes.sms", RuntimeInstance: f.req.RuntimeInstance, PackageID: f.req.PackageID, Generation: f.req.CoreGeneration}
			if err := f.c.ports.setObservationContext(ctx, obs); err != nil {
				t.Fatal(err)
			}
			posts := make(chan uint64, 16)
			f.c.ports.keys.SetPoster(func(_ context.Context, _ localhid.MatrixPost, v uint64) error {
				posts <- v
				if fail.Load() {
					return errors.New("unconfirmed")
				}
				return nil
			})
			reader, writer := net.Pipe()
			defer writer.Close()
			done := make(chan struct{})
			go func() { defer close(done); f.c.readLocal(ctx, reader, true) }()
			testRecoverConnection(t, f.c, writer)
			press := gamepad(0, remoteinput.ButtonA, remoteinput.ActionPress, 0)
			if err := bridge.WriteFrame(writer, press); err != nil {
				t.Fatal(err)
			}
			<-posts
			fail.Store(true)
			departure := localPlayerDeparture(0)
			departure.Seq = 2
			if err := bridge.WriteFrame(writer, departure); err != nil {
				t.Fatal(err)
			}
			select {
			case <-posts:
			case <-wait.Done():
				t.Fatal("departure not attempted")
			}
			if _, err := manager.Release(lease.Token); err != nil {
				t.Fatal(err)
			}
			if _, err := manager.Settled(wait); err != nil {
				t.Fatal(err)
			}
			if confirmed {
				select {
				case <-done:
				case <-wait.Done():
					t.Fatal("confirmed cleanup did not retire reader")
				}
				next, peer := net.Pipe()
				defer next.Close()
				nextDone := make(chan struct{})
				go func() { defer close(nextDone); f.c.readLocal(ctx, peer, true) }()
				testRecoverConnection(t, f.c, next)
				_ = next.Close()
				select {
				case <-nextDone:
				case <-wait.Done():
					t.Fatal("successor did not drain")
				}
			} else {
				select {
				case <-done:
					t.Fatal("failed cleanup retired reader")
				case <-time.After(650 * time.Millisecond):
				}
				cancel()
				_ = writer.Close()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Fatal("reader ignored cancellation")
				}
			}
		})
	}
}
