package agent

import (
	"context"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type readHoldRuntime struct {
	inRuntime   atomic.Int32
	overlapSeen atomic.Bool

	onInspect func(ctx context.Context)
	onStop    func(ctx context.Context)
	onLaunch  func(ctx context.Context)
	observe   func(ctx context.Context) (*protocol.RuntimeInputIdentity, error)
}

func (r *readHoldRuntime) enter() func() {
	if r.inRuntime.Add(1) > 1 {
		r.overlapSeen.Store(true)
	}
	return func() { r.inRuntime.Add(-1) }
}

func (r *readHoldRuntime) Health(string) protocol.Health { return protocol.Health{Ready: true} }
func (r *readHoldRuntime) Reconcile(context.Context) protocol.Status {
	return protocol.Status{State: protocol.StateIdle}
}
func (r *readHoldRuntime) LoadDevelopmentRBF(ctx context.Context, _ int64, _ io.Reader) (string, bool, *protocol.APIError) {
	defer r.enter()()
	if r.onLaunch != nil {
		r.onLaunch(ctx)
	}
	return "fes.pong", true, nil
}
func (r *readHoldRuntime) RecoverDevelopment(context.Context) (string, *protocol.APIError) {
	return "MENU", nil
}
func (r *readHoldRuntime) Stop(ctx context.Context) (string, *protocol.APIError) {
	defer r.enter()()
	if r.onStop != nil {
		r.onStop(ctx)
	}
	return "MENU", nil
}
func (r *readHoldRuntime) InspectMediaRecord(ctx context.Context, _ protocol.MediaRecordIdentity) (protocol.MediaRecord, *protocol.APIError) {
	defer r.enter()()
	if r.onInspect != nil {
		r.onInspect(ctx)
	}
	return protocol.MediaRecord{State: protocol.MediaRecordAbsent, Revision: "absent"}, nil
}
func (r *readHoldRuntime) DiscardMediaRecord(context.Context, protocol.MediaRecordDiscard) (protocol.MediaRecord, *protocol.APIError) {
	defer r.enter()()
	return protocol.MediaRecord{State: protocol.MediaRecordAbsent, Revision: "absent"}, nil
}
func (r *readHoldRuntime) ExportMediaRecord(ctx context.Context, _ protocol.MediaRecordDiscard) ([]byte, *protocol.APIError) {
	defer r.enter()()
	if r.onInspect != nil {
		r.onInspect(ctx)
	}
	return []byte("disk"), nil
}
func (r *readHoldRuntime) InspectCoreData(ctx context.Context, _ int64, _ io.Reader, _ string) (protocol.CoreDataInspection, *protocol.APIError) {
	defer r.enter()()
	if r.onInspect != nil {
		r.onInspect(ctx)
	}
	return protocol.CoreDataInspection{}, nil
}
func (r *readHoldRuntime) UpdateCoreSettings(context.Context, int64, io.Reader, protocol.CoreSettingsUpdate) (protocol.CoreDataInspection, *protocol.APIError) {
	defer r.enter()()
	return protocol.CoreDataInspection{}, nil
}
func (r *readHoldRuntime) LoadLibraryMachineCoreOwned(context.Context, context.Context, context.Context, int64, io.Reader, string, protocol.LibraryMachineBinding) (misterruntime.CoreActivation, bool, *protocol.APIError) {
	defer r.enter()()
	return misterruntime.CoreActivation{}, true, nil
}
func (r *readHoldRuntime) InspectGameCoreData(ctx context.Context, _ int64, _ io.Reader, _, _ string) (protocol.CoreDataInspection, *protocol.APIError) {
	defer r.enter()()
	if r.onInspect != nil {
		r.onInspect(ctx)
	}
	return protocol.CoreDataInspection{}, nil
}
func (r *readHoldRuntime) ObserveRuntimeInput(ctx context.Context) (*protocol.RuntimeInputIdentity, error) {
	if r.observe != nil {
		return r.observe(ctx)
	}
	return nil, nil
}

func sampleMediaRecordID() protocol.MediaRecordIdentity {
	return protocol.MediaRecordIdentity{
		CoreID:      "fes.pcxt-dos",
		GameID:      "game-1",
		BaseMediaID: strings.Repeat("b", 64),
		Unit:        0,
	}
}

func TestStopAndLaunchWaitOutInFlightReadOnlyRoutes(t *testing.T) {
	cases := []struct {
		name      string
		startRead func(*Coordinator) *protocol.APIError
		runTrans  func(*Coordinator) *protocol.APIError
	}{
		{
			name: "stop_during_inspect_media_record",
			startRead: func(c *Coordinator) *protocol.APIError {
				_, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID())
				return err
			},
			runTrans: func(c *Coordinator) *protocol.APIError {
				_, err := c.Stop(context.Background())
				return err
			},
		},
		{
			name: "launch_during_inspect_media_record",
			startRead: func(c *Coordinator) *protocol.APIError {
				_, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID())
				return err
			},
			runTrans: func(c *Coordinator) *protocol.APIError {
				_, err := c.LoadDevelopmentRBF(context.Background(), 4, strings.NewReader("data"))
				return err
			},
		},
		{
			name: "stop_during_export_media_record",
			startRead: func(c *Coordinator) *protocol.APIError {
				_, err := c.ExportMediaRecord(context.Background(), protocol.MediaRecordDiscard{
					MediaRecordIdentity: sampleMediaRecordID(),
					ExpectedRevision:    strings.Repeat("a", 64),
				})
				return err
			},
			runTrans: func(c *Coordinator) *protocol.APIError {
				_, err := c.Stop(context.Background())
				return err
			},
		},
		{
			name: "stop_during_inspect_core_data",
			startRead: func(c *Coordinator) *protocol.APIError {
				_, err := c.InspectCoreData(context.Background(), 1, strings.NewReader("p"), strings.Repeat("a", 64))
				return err
			},
			runTrans: func(c *Coordinator) *protocol.APIError {
				_, err := c.Stop(context.Background())
				return err
			},
		},
		{
			name: "stop_during_inspect_game_core_data",
			startRead: func(c *Coordinator) *protocol.APIError {
				_, err := c.InspectGameCoreData(context.Background(), 1, strings.NewReader("p"), strings.Repeat("a", 64), "game-1")
				return err
			},
			runTrans: func(c *Coordinator) *protocol.APIError {
				_, err := c.Stop(context.Background())
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			readStarted := make(chan struct{})
			readRelease := make(chan struct{})
			rt := &readHoldRuntime{
				onInspect: func(context.Context) {
					close(readStarted)
					<-readRelease
				},
			}
			c := New(rt, time.Second, time.Second)
			name := "fes.pong"
			c.set(protocol.Status{State: protocol.StateActive, ObservedCore: &name})

			readDone := make(chan *protocol.APIError, 1)
			go func() { readDone <- tc.startRead(c) }()
			<-readStarted

			transDone := make(chan *protocol.APIError, 1)
			go func() { transDone <- tc.runTrans(c) }()

			// Wait until the transition has claimed transitionActive before releasing the read.
			deadline := time.Now().Add(time.Second)
			for {
				c.mu.RLock()
				waiting := c.transitionActive
				c.mu.RUnlock()
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("transition did not claim transitionActive while waiting on read")
				}
				time.Sleep(time.Millisecond)
			}
			// No new read may sneak in ahead of the waiting transition.
			if _, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID()); err == nil || err.Code != protocol.CodeBusy {
				t.Fatalf("new read was not rejected while transition waited: %v", err)
			}

			close(readRelease)
			if err := <-readDone; err != nil {
				t.Fatalf("in-flight read failed: %v", err)
			}
			if err := <-transDone; err != nil {
				t.Fatalf("transition failed during read: %v", err)
			}
			if rt.overlapSeen.Load() {
				t.Fatal("read and transition overlapped in runtime")
			}
		})
	}
}

func TestShelfBatchYieldsToStopWithoutBusy(t *testing.T) {
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	stopStarted := make(chan struct{})
	stopRelease := make(chan struct{})
	var inspectCount atomic.Int32
	rt := &readHoldRuntime{
		onInspect: func(context.Context) {
			if inspectCount.Add(1) == 1 {
				close(firstStarted)
				<-firstRelease
			}
		},
		onStop: func(context.Context) {
			close(stopStarted)
			<-stopRelease
		},
	}
	c := New(rt, time.Second, time.Second)
	name := "fes.pong"
	c.set(protocol.Status{State: protocol.StateActive, ObservedCore: &name})

	batchErr := make(chan *protocol.APIError, 1)
	go func() {
		for i := 0; i < 16; i++ {
			if _, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID()); err != nil {
				batchErr <- err
				return
			}
		}
		batchErr <- nil
	}()
	<-firstStarted

	stopErr := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.Stop(context.Background())
		stopErr <- err
	}()
	deadline := time.Now().Add(time.Second)
	for {
		c.mu.RLock()
		waiting := c.transitionActive
		c.mu.RUnlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stop did not reserve transition slot")
		}
		time.Sleep(time.Millisecond)
	}

	close(firstRelease)
	<-stopStarted
	// The batch's next item must yield immediately with BUSY while Stop runs.
	if err := <-batchErr; err == nil || err.Code != protocol.CodeBusy {
		t.Fatalf("expected batch to yield with BUSY during stop, got %v", err)
	}
	close(stopRelease)
	if err := <-stopErr; err != nil {
		t.Fatalf("stop failed during shelf batch: %v", err)
	}
	if rt.overlapSeen.Load() {
		t.Fatal("shelf inspect overlapped Stop in runtime")
	}
}

func TestConcurrentReadsSerializeWithoutBusy(t *testing.T) {
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	var count atomic.Int32
	rt := &readHoldRuntime{
		onInspect: func(context.Context) {
			if count.Add(1) == 1 {
				close(firstStarted)
				<-firstRelease
			}
		},
	}
	c := New(rt, time.Second, time.Second)

	firstDone := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID())
		firstDone <- err
	}()
	<-firstStarted

	secondDone := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID())
		secondDone <- err
	}()

	time.Sleep(15 * time.Millisecond)
	close(firstRelease)

	if err := <-firstDone; err != nil {
		t.Fatalf("first read failed: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second concurrent read failed: %v", err)
	}
	if rt.overlapSeen.Load() {
		t.Fatal("concurrent reads overlapped in runtime")
	}
}

func TestReadRejectsImmediatelyDuringActiveTransition(t *testing.T) {
	launchStarted := make(chan struct{})
	launchRelease := make(chan struct{})
	rt := &readHoldRuntime{
		onLaunch: func(context.Context) {
			close(launchStarted)
			<-launchRelease
		},
	}
	c := New(rt, time.Second, time.Second)
	c.readWaitTimeout = 2 * time.Second

	launchDone := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.LoadDevelopmentRBF(context.Background(), 4, strings.NewReader("data"))
		launchDone <- err
	}()
	<-launchStarted

	start := time.Now()
	_, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID())
	elapsed := time.Since(start)
	close(launchRelease)
	<-launchDone

	if err == nil || err.Code != protocol.CodeBusy {
		t.Fatalf("expected BUSY during active launch, got %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("read blocked behind active transition for %v", elapsed)
	}
}

func TestTransitionWaitingOnStuckReadCleansUpOnCancelAndTimeout(t *testing.T) {
	readStarted := make(chan struct{})
	readRelease := make(chan struct{})
	rt := &readHoldRuntime{
		onInspect: func(context.Context) {
			select {
			case <-readStarted:
			default:
				close(readStarted)
				<-readRelease
			}
		},
	}
	c := New(rt, time.Second, time.Second)
	c.readWaitTimeout = 40 * time.Millisecond

	readDone := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID())
		readDone <- err
	}()
	<-readStarted

	// Cancelled caller ctx while waiting releases transitionActive cleanly.
	cancelCtx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, err := c.Stop(cancelCtx); err == nil || err.Code != protocol.CodeBusy {
		t.Fatalf("expected BUSY on cancelled wait, got %v", err)
	}

	// Read-wait timeout while waiting also releases transitionActive cleanly.
	if _, err := c.Stop(context.Background()); err == nil || err.Code != protocol.CodeBusy {
		t.Fatalf("expected BUSY on readWaitTimeout, got %v", err)
	}

	close(readRelease)
	if err := <-readDone; err != nil {
		t.Fatalf("read failed: %v", err)
	}

	// After the stuck read ends, neither transitionActive nor readActive is wedged.
	if _, err := c.Stop(context.Background()); err != nil {
		t.Fatalf("subsequent stop wedged: %v", err)
	}
	if _, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID()); err != nil {
		t.Fatalf("subsequent read wedged: %v", err)
	}
}

func TestReadOnlyInspectDoesNotSuppressRuntimeInputStatus(t *testing.T) {
	readStarted := make(chan struct{})
	readRelease := make(chan struct{})
	identity := &protocol.RuntimeInputIdentity{
		Kind:            protocol.RuntimeInputDevelopment,
		RuntimeInstance: strings.Repeat("a", 32),
		Generation:      4,
	}
	rt := &readHoldRuntime{
		onInspect: func(context.Context) {
			close(readStarted)
			<-readRelease
		},
		observe: func(context.Context) (*protocol.RuntimeInputIdentity, error) {
			return identity, nil
		},
	}
	c := New(rt, time.Second, time.Second)
	c.set(protocol.Status{State: protocol.StateActive, Development: true})

	readDone := make(chan *protocol.APIError, 1)
	go func() {
		_, err := c.InspectMediaRecord(context.Background(), sampleMediaRecordID())
		readDone <- err
	}()
	<-readStarted

	status := c.StatusWithRuntimeInput(context.Background())
	close(readRelease)
	if err := <-readDone; err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if status.RuntimeInput == nil || status.RuntimeInput.Generation != 4 {
		t.Fatalf("read-only inspect suppressed runtime input status: %+v", status)
	}
}
