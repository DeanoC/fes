package misterruntime_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/internal/misterruntime"
	"github.com/DeanoC/FogCast/protocol"
)

type streamControl struct {
	mediaControl
	stream func(context.Context, string, string, uint64, uint32) (misterruntime.Protocol2Response, error)
}

func (c *streamControl) LoadMediaStream(ctx context.Context, path, id string, generation uint64, size uint32) (misterruntime.Protocol2Response, error) {
	return c.stream(ctx, path, id, generation, size)
}

func streamResponse() misterruntime.Protocol2Response {
	r := mediaResponse()
	r.Capabilities.ActiveInterfaces = append(r.Capabilities.ActiveInterfaces, misterruntime.Protocol2Interface{ID: "fes.media.blob-stream", Major: 1})
	r.Capabilities.MediaStream = &protocol.MediaStreamCapability{Interface: protocol.MediaStreamInterface(), MinBytes: 1, MaxBytes: 32768, ChunkBytes: 512}
	return r
}

type cancelMediaReader struct{ cancel context.CancelFunc }

func (r cancelMediaReader) Read(p []byte) (int, error) { r.cancel(); return 0, context.Canceled }

func TestMediaStreamStagingRecheckAndOneShotDispatch(t *testing.T) {
	for _, name := range []string{"success", "lost reply", "short", "long", "oversize", "cancel staging", "missing second capability", "invalid second capability", "changed generation"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("TMPDIR", root)
			before, after := streamResponse(), streamResponse()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			size := int64(32768)
			payload := bytes.Repeat([]byte{7}, int(size))
			var body io.Reader = bytes.NewReader(payload)
			switch name {
			case "short":
				body = bytes.NewReader(payload[:len(payload)-1])
			case "long":
				body = bytes.NewReader(append(payload, 1))
			case "oversize":
				size++
			case "cancel staging":
				body = cancelMediaReader{cancel}
			case "missing second capability":
				after.Capabilities.MediaStream = nil
			case "invalid second capability":
				after.Capabilities.MediaStream.ChunkBytes = 256
			case "changed generation":
				*after.Generation++
			}
			calls := 0
			control := &streamControl{mediaControl: mediaControl{packageControl: packageControl{status2Script: []protocol2StatusResult{{response: before}, {response: after}}}}}
			control.loadMedia = func(context.Context, string) (misterruntime.Protocol2Response, error) {
				t.Fatal("legacy dispatch used")
				return before, nil
			}
			control.stream = func(op context.Context, path, id string, generation uint64, count uint32) (misterruntime.Protocol2Response, error) {
				calls++
				if id != strings.Repeat("a", 64) || generation != 9 || count != 32768 {
					t.Fatalf("wrong binding %s %d %d", id, generation, count)
				}
				deadline, ok := op.Deadline()
				if !ok || time.Until(deadline) < 120*time.Second {
					t.Fatal("operation deadline shorter than daemon budget")
				}
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, payload) {
					t.Fatalf("snapshot: %v", err)
				}
				if name == "lost reply" {
					return misterruntime.Protocol2Response{}, io.EOF
				}
				return after, nil
			}
			runtime := misterruntime.NewRuntime(control, "", 0, 0)
			err := runtime.LoadDevelopmentMediaOwned(ctx, context.Background(), size, body, protocol.DevelopmentMediaBinding{PackageID: strings.Repeat("a", 64), Generation: 9, Stream: true})
			wantCalls := 0
			if name == "success" || name == "lost reply" {
				wantCalls = 1
			}
			if calls != wantCalls || (err == nil) != (name == "success") {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
			if name == "cancel staging" && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation cause lost: %v", err)
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("staging retained: %v %v", entries, readErr)
			}
		})
	}
}
