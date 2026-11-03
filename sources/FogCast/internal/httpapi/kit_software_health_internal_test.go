package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/DeanoC/FogCast/internal/testsock"
	"github.com/DeanoC/FogCast/protocol"
)

func validHealthStatus() string {
	return `{"version":1,"generation":0,"selected":"` + strings.Repeat("a", 64) + `","receipts":{},"active_bundle":"` + strings.Repeat("b", 64) + `","base_identity":"base"}`
}

func TestSoftwareHealthUTF8AcrossReadBoundaries(t *testing.T) {
	valid := strings.Replace(validHealthStatus(), `"version":1`, `"future":"é€😀","version":1`, 1)
	invalid := strings.Replace(validHealthStatus(), `"version":1`, `"future":"`+string([]byte{0xe2, 0x82})+`","version":1`, 1)
	for _, tc := range []struct {
		name string
		data string
		ok   bool
	}{
		{"valid split runes", valid, true},
		{"incomplete rune", invalid, false},
		{"invalid continuation", strings.Replace(valid, "€", string([]byte{0xe2, 0x28, 0xa1}), 1), false},
		{"stray continuation", strings.Replace(valid, "é", string([]byte{0x80}), 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := decodeSoftwareHealth(iotest.OneByteReader(strings.NewReader(tc.data)))
			if ok != tc.ok {
				t.Fatalf("validity got %v, want %v", ok, tc.ok)
			}
		})
	}
}

func TestDecodeSoftwareHealthCompleteEnvelope(t *testing.T) {
	valid := validHealthStatus()
	selected := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name string
		data string
		ok   bool
	}{
		{"valid zero generation", valid, true},
		{"unknown additive field", strings.Replace(valid, `"version":1`, `"future":null,"version":1`, 1), true},
		{"empty object", `{}`, false},
		{"null", `null`, false},
		{"truncated", valid[:len(valid)-1], false},
		{"trailing object", valid + `{}`, false},
		{"trailing garbage", valid + `garbage`, false},
		{"bad utf8", strings.Replace(valid, `"version":1`, `"future":"`+string([]byte{0xff})+`","version":1`, 1), false},
		{"duplicate selection", strings.Replace(valid, `"selected":`, `"selected":"`+selected+`","selected":`, 1), false},
		{"case alias", strings.Replace(valid, `"selected":`, `"Selected":`, 1), false},
		{"unicode alias duplicate", strings.Replace(valid, `"selected":`, `"ſelected":"`+selected+`","selected":`, 1), false},
		{"missing generation", strings.Replace(valid, `"generation":0,`, ``, 1), false},
		{"null generation", strings.Replace(valid, `"generation":0`, `"generation":null`, 1), false},
		{"bad generation", strings.Replace(valid, `"generation":0`, `"generation":"bad"`, 1), false},
		{"negative generation", strings.Replace(valid, `"generation":0`, `"generation":-1`, 1), false},
		{"bad version", strings.Replace(valid, `"version":1`, `"version":2`, 1), false},
		{"missing selection", strings.Replace(valid, `"selected":"`+selected+`",`, ``, 1), false},
		{"invalid selection", strings.Replace(valid, selected, `invalid`, 1), false},
		{"missing receipts", strings.Replace(valid, `"receipts":{},`, ``, 1), false},
		{"null receipts", strings.Replace(valid, `"receipts":{}`, `"receipts":null`, 1), false},
		{"wrong receipts type", strings.Replace(valid, `"receipts":{}`, `"receipts":[]`, 1), false},
		{"malformed nested receipts", strings.Replace(valid, `"receipts":{}`, `"receipts":{"a":`, 1), false},
		{"missing active", strings.Replace(valid, `"active_bundle":"`+strings.Repeat("b", 64)+`",`, ``, 1), false},
		{"null base", strings.Replace(valid, `"base_identity":"base"`, `"base_identity":null`, 1), false},
		{"storage error", strings.Replace(valid, `"version":1`, `"storage_error":"unavailable","version":1`, 1), false},
		{"null storage error", strings.Replace(valid, `"version":1`, `"storage_error":null,"version":1`, 1), false},
		{"invalid storage error", strings.Replace(valid, `"version":1`, `"storage_error":1,"version":1`, 1), false},
		{"bad optional type", strings.Replace(valid, `"version":1`, `"previous":[],"version":1`, 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity, ok := decodeSoftwareHealth(strings.NewReader(tc.data))
			if ok != tc.ok {
				t.Fatalf("validity: got %v, want %v; identity=%+v", ok, tc.ok, identity)
			}
			if ok && (identity.Selected != selected || identity.Generation != 0 || identity.BaseIdentity != "base") {
				t.Fatalf("identity changed: %+v", identity)
			}
		})
	}
	oversize := append([]byte(valid), bytes.Repeat([]byte(" "), maxSoftwareControlResponse)...)
	if _, ok := decodeSoftwareHealth(bytes.NewReader(oversize)); ok {
		t.Fatal("oversized trailing whitespace was accepted")
	}
}

func TestSoftwareHealthCancellationDuringSupervisorBody(t *testing.T) {
	t.Setenv("FES_SOFTWARE_BUNDLE_ID", strings.Repeat("b", 64))
	t.Setenv("FES_SOFTWARE_BASE_IDENTITY", "base")
	socket := testsock.Path(t, "software.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"version":1,`)
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan *protocol.Identity, 1)
	go func() { result <- kitSoftwareHealth(ctx, socket) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("supervisor response did not start")
	}
	cancel()
	select {
	case identity := <-result:
		if identity.Selected != "" || identity.ActiveBundle != strings.Repeat("b", 64) || identity.BaseIdentity != "base" {
			t.Fatalf("cancelled request exposed a selection: %+v", identity)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled supervisor body did not return")
	}
}

// Compare the existing compact decoder with validation of the same large
// supervisor envelope. The receipt objects are not built by either path.
func BenchmarkSoftwareHealthEnvelope(b *testing.B) {
	for _, count := range []int{65, 690} {
		data := benchmarkHealthStatus(count)
		b.Run(fmt.Sprintf("receipts=%d/baseline", count), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var identity protocol.Identity
				if err := json.NewDecoder(bytes.NewReader(data)).Decode(&identity); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("receipts=%d/validated", count), func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, ok := decodeSoftwareHealth(bytes.NewReader(data)); !ok {
					b.Fatal("valid benchmark fixture rejected")
				}
			}
		})
	}
}

func benchmarkHealthStatus(count int) []byte {
	var ledger strings.Builder
	for i := 0; i < count; i++ {
		if i != 0 {
			ledger.WriteByte(',')
		}
		fmt.Fprintf(&ledger, `"%064x":{"id":"%064x","candidate":"%064x","requester":"bench","expected_generation":%d,"result":"accepted"}`, i, i, i+1, i)
	}
	status := strings.Replace(validHealthStatus(), `"generation":0`, fmt.Sprintf(`"generation":%d`, count), 1)
	return []byte(strings.Replace(status, `"receipts":{}`, `"receipts":{`+ledger.String()+`}`, 1))
}

// Benchmark the supervisor socket and compact health path, including body
// reads. The baseline is the #1445 implementation and the fixture is shared.
func BenchmarkSoftwareHealthSocket(b *testing.B) {
	for _, count := range []int{65, 690} {
		data := benchmarkHealthStatus(count)
		socket := testsock.Path(b, "software.sock")
		listener, err := net.Listen("unix", socket)
		if err != nil {
			b.Fatal(err)
		}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(data)
		})}
		go func() { _ = server.Serve(listener) }()
		b.Cleanup(func() { _ = server.Close() })
		for _, tc := range []struct {
			name string
			read func(context.Context, string) *protocol.Identity
		}{
			{"baseline", benchmarkBaselineSoftwareHealth},
			{"validated", kitSoftwareHealth},
		} {
			b.Run(fmt.Sprintf("receipts=%d/%s", count, tc.name), func(b *testing.B) {
				b.SetBytes(int64(len(data)))
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if got := tc.read(context.Background(), socket); got.Selected != strings.Repeat("a", 64) {
						b.Fatalf("health unavailable or identity changed: %+v", got)
					}
				}
			})
		}
	}
}

func benchmarkBaselineSoftwareHealth(parent context.Context, socket string) *protocol.Identity {
	ctx, cancel := context.WithTimeout(parent, 250*time.Millisecond)
	defer cancel()
	result := &protocol.Identity{}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://software/v1/kit/software", nil)
	response, err := softwareSupervisorClient(socket).Do(request)
	if err != nil {
		return result
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return result
	}
	status := *result
	if err := json.NewDecoder(io.LimitReader(response.Body, maxSoftwareControlResponse)).Decode(&status); err != nil {
		return result
	}
	return &status
}
