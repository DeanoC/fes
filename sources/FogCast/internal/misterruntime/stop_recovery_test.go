package misterruntime_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

func TestLostStopResponseObservesOutcomeWithoutReplay(t *testing.T) {
	malformed := runtimeResponse("idle", "game")
	save := runtimeResponse("running_game", "game")
	system, core := "snes", "SNES"
	save.System, save.Core, save.OK = &system, &core, false
	save.Error = &misterruntime.Protocol2Error{Code: "save_failed", Message: "private save detail"}
	for _, tc := range []struct {
		name     string
		status   misterruntime.Protocol2Response
		code     protocol.ErrorCode
		recovery string
	}{
		{name: "clean idle", status: runtimeResponse("idle", "none")},
		{name: "still running", status: runtimeResponse("running_game", "game"), code: protocol.CodeMiSTerUnavailable},
		{name: "malformed idle", status: malformed, code: protocol.CodeMiSTerUnavailable},
		{name: "retained idle error", status: retainedErrorIdleResponse(), code: protocol.CodeMiSTerUnavailable},
		{name: "reboot required", status: runtimeResponse("reboot_required", "none"), recovery: protocol.RecoveryRebootRequired},
		{name: "save failure", status: save, code: protocol.CodeSaveFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control := &recordingControl{stopErr: io.EOF, statuses: []misterruntime.Protocol2Response{tc.status}}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			_, recovery, apiErr := runtime.StopOwnedWithRecovery(context.Background(), context.Background())
			if recovery != tc.recovery || (tc.code == "" && apiErr != nil) || (tc.code != "" && (apiErr == nil || apiErr.Code != tc.code)) {
				t.Fatalf("recovery=%q error=%#v; want recovery=%q code=%q", recovery, apiErr, tc.recovery, tc.code)
			}
			statusN, stopN := control.calls()
			if statusN != 1 || stopN != 1 {
				t.Fatalf("status=%d stop=%d; want one each", statusN, stopN)
			}
		})
	}
}

func TestLostStopResponseThroughUnixSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	operations := make(chan string, 2)
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 2; i++ {
			conn, err := listener.Accept()
			if err != nil {
				done <- err
				return
			}
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			var request struct {
				Operation string `json:"operation"`
			}
			err = json.NewDecoder(conn).Decode(&request)
			if err == nil {
				operations <- request.Operation
				if request.Operation == "status" {
					err = json.NewEncoder(conn).Encode(runtimeResponse("idle", "none"))
				}
			}
			conn.Close()
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	runtime := misterruntime.NewRuntime(misterruntime.NewClient(path), "", time.Millisecond, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, apiErr := runtime.Stop(ctx)
	if apiErr != nil {
		t.Fatalf("lost successful Stop: %#v", apiErr)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("server did not complete")
	}
	if first, second := <-operations, <-operations; first != "stop" || second != "status" {
		t.Fatalf("operations = %q, %q", first, second)
	}
}

type waitingStopObservation struct {
	recordingControl
	started  chan struct{}
	deadline chan bool
}

func (c *waitingStopObservation) Protocol2Status(ctx context.Context) (misterruntime.Protocol2Response, error) {
	_, bounded := ctx.Deadline()
	c.deadline <- bounded
	close(c.started)
	<-ctx.Done()
	return misterruntime.Protocol2Response{}, ctx.Err()
}

func TestLostStopObservationIsBoundedAndCancelable(t *testing.T) {
	for _, cancelOwner := range []bool{false, true} {
		t.Run(map[bool]string{false: "health deadline", true: "operation canceled"}[cancelOwner], func(t *testing.T) {
			control := &waitingStopObservation{recordingControl: recordingControl{stopErr: io.EOF}, started: make(chan struct{}), deadline: make(chan bool, 1)}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, 20*time.Millisecond)
			owner, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan *protocol.APIError, 1)
			go func() { _, err := runtime.StopOwned(context.Background(), owner); done <- err }()
			select {
			case <-control.started:
			case <-time.After(time.Second):
				t.Fatal("observation did not start")
			}
			if cancelOwner {
				cancel()
			}
			select {
			case err := <-done:
				if err == nil || err.Code != protocol.CodeMiSTerUnavailable {
					t.Fatalf("error=%#v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("observation exceeded its budget")
			}
			if !<-control.deadline {
				t.Fatal("unbounded observation")
			}
			_, stopN := control.calls()
			if stopN != 1 {
				t.Fatalf("Stop replayed %d times", stopN)
			}
		})
	}
}

func TestConfirmIdleRejectsUnsafeObservations(t *testing.T) {
	save := retainedErrorIdleResponse()
	save.Error = &misterruntime.Protocol2Error{Code: "save_failed", Message: "save retained"}
	for _, tc := range []struct {
		name     string
		response misterruntime.Protocol2Response
		want     bool
	}{
		{"clean idle", runtimeResponse("idle", "none"), true},
		{"retained launch failure", retainedErrorIdleResponse(), true},
		{"save failure", save, false},
		{"running", runtimeResponse("running_game", "game"), false},
		{"malformed", runtimeResponse("idle", "game"), false},
		{"reboot required", runtimeResponse("reboot_required", "none"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			control := &recordingControl{statuses: []misterruntime.Protocol2Response{tc.response}}
			runtime := misterruntime.NewRuntime(control, "", time.Millisecond, time.Second)
			if got := runtime.ConfirmIdle(context.Background()); got != tc.want {
				t.Fatalf("ConfirmIdle=%v want %v", got, tc.want)
			}
			reads, stops := control.calls()
			if reads != 1 || stops != 0 {
				t.Fatalf("reads=%d stops=%d", reads, stops)
			}
		})
	}
}
