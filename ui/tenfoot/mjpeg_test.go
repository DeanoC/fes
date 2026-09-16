package tenfoot

import (
	"bytes"
	"context"
	"fmt"
	"github.com/DeanoC/FogCast/ui/shared"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func writeMJPEGPart(w io.Writer, jpeg []byte) {
	_, _ = fmt.Fprintf(w, "--fogcast-frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(jpeg))
	_, _ = w.Write(jpeg)
	_, _ = io.WriteString(w, "\r\n")
}

func TestMJPEGStreamReadsHostJPEGParts(t *testing.T) {
	t.Parallel()
	first := mustJPEG(t, 4, 3, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	second := mustJPEG(t, 5, 2, color.RGBA{R: 200, G: 10, B: 10, A: 255})
	var body bytes.Buffer
	writeMJPEGPart(&body, first)
	writeMJPEGPart(&body, second)
	stream, err := NewMJPEGStream(io.NopCloser(bytes.NewReader(body.Bytes())), "multipart/x-mixed-replace; boundary=fogcast-frame")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got, err := stream.NextJPEG()
	if err != nil || !bytes.Equal(got, first) {
		t.Fatalf("first frame err=%v len=%d", err, len(got))
	}
	got, err = stream.NextJPEG()
	if err != nil || !bytes.Equal(got, second) {
		t.Fatalf("second frame err=%v len=%d", err, len(got))
	}
}

func TestMJPEGStreamRejectsNonMultipart(t *testing.T) {
	t.Parallel()
	_, err := NewMJPEGStream(io.NopCloser(bytes.NewReader(nil)), "image/jpeg")
	if !IsPreviewUnavailable(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientOpenSessionPreviewConsumesMJPEG(t *testing.T) {
	t.Parallel()
	jpeg := mustJPEG(t, 4, 3, color.RGBA{R: 11, G: 22, B: 33, A: 255})
	var accepts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/session/preview" {
			http.NotFound(w, r)
			return
		}
		accepts = append(accepts, r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=fogcast-frame")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeMJPEGPart(w, jpeg)
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := NewClient(server.URL, server.Client()).OpenSessionPreview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	got, err := stream.NextJPEG()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, jpeg) {
		t.Fatalf("frame mismatch len=%d", len(got))
	}
	if len(accepts) != 1 || accepts[0] != previewContentType {
		t.Fatalf("accept = %#v", accepts)
	}
	img, err := shared.DecodeStill(got)
	if err != nil || img == nil {
		t.Fatalf("cpu jpeg decode: %v", err)
	}
}

func TestClientOpenSessionPreviewTreatsMissesAsUnavailable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		code   int
		body   string
		header string
	}{
		{name: "missing-route", code: http.StatusNotFound, body: "404 page not found\n"},
		{name: "inactive", code: http.StatusServiceUnavailable, body: "session preview is inactive\n"},
		{name: "plain-503", code: http.StatusServiceUnavailable, body: "kit down\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, strings.TrimSuffix(tc.body, "\n"), tc.code)
			}))
			t.Cleanup(server.Close)
			_, err := NewClient(server.URL, server.Client()).OpenSessionPreview(context.Background())
			if !IsPreviewUnavailable(err) {
				t.Fatalf("err = %v", err)
			}
			var u PreviewUnavailable
			if !errorsAsPreview(err, &u) || u.Status != tc.code {
				t.Fatalf("unavailable = %#v err=%v", u, err)
			}
			if tc.code == http.StatusNotFound && !previewUnavailablePermanent(err) {
				t.Fatal("404 should be permanent for this session")
			}
			if tc.code == http.StatusServiceUnavailable && previewUnavailablePermanent(err) {
				t.Fatal("503 should be retryable")
			}
		})
	}
}

func TestClientOpenSessionPreviewNetworkIsUnavailable(t *testing.T) {
	t.Parallel()
	client := NewClient("http://127.0.0.1:1", &http.Client{Timeout: 200 * time.Millisecond})
	_, err := client.OpenSessionPreview(context.Background())
	if !IsPreviewUnavailable(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientOpenSessionPreviewCancelClosesBody(t *testing.T) {
	t.Parallel()
	var live atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		live.Add(1)
		defer live.Add(-1)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=fogcast-frame")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		writeMJPEGPart(w, mustJPEG(t, 2, 2, color.RGBA{A: 255}))
		if flusher != nil {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := NewClient(server.URL, server.Client()).OpenSessionPreview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.NextJPEG(); err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = stream.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if live.Load() == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("preview connection still live=%d", live.Load())
}

func errorsAsPreview(err error, dest *PreviewUnavailable) bool {
	if err == nil || dest == nil {
		return false
	}
	u, ok := err.(PreviewUnavailable)
	if !ok {
		return false
	}
	*dest = u
	return true
}
