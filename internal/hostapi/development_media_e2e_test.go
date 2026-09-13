package hostapi_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/internal/agent"
	"github.com/DeanoC/FogCast/internal/core"
	"github.com/DeanoC/FogCast/internal/corepackage"
	"github.com/DeanoC/FogCast/internal/fogcastcli"
	"github.com/DeanoC/FogCast/internal/hostapi"
	"github.com/DeanoC/FogCast/internal/httpapi"
	"github.com/DeanoC/FogCast/internal/kitlease"
	"github.com/DeanoC/FogCast/internal/misterruntime"
)

// Only the hardware daemon is simulated: CLI, host API/service, target client,
// lease manager, target HTTP/controller, staging and Unix protocol are real.
func TestDevelopmentMediaEndToEndFromRunningHostCLI(t *testing.T) {
	dir := t.TempDir()
	read := func(path string) []byte {
		t.Helper()
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	write := func(path string, b []byte) {
		t.Helper()
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	transform := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "fes.simple-game", "fes.simple-computer"), "fes.gamepad", "fes.media.blob")
	}
	manifest := []byte(transform(string(read("../corepackage/testdata/core-bundle-v2/manifests/valid-basic.toml"))))
	payload := read("../corepackage/testdata/core-bundle-v2/payloads/fes-fixture.rbf")
	var archive bytes.Buffer
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.toml", manifest}, {"core.rbf", payload}} {
		header := make([]byte, 512)
		copy(header, entry.name)
		copy(header[100:108], "0000644\x00")
		copy(header[108:116], "0000000\x00")
		copy(header[116:124], "0000000\x00")
		copy(header[124:136], fmt.Sprintf("%011o\x00", len(entry.data)))
		copy(header[136:148], "00000000000\x00")
		for i := 148; i < 156; i++ {
			header[i] = ' '
		}
		header[156] = '0'
		copy(header[257:263], "ustar\x00")
		copy(header[263:265], "00")
		sum := 0
		for _, b := range header {
			sum += int(b)
		}
		copy(header[148:156], fmt.Sprintf("%06o\x00 ", sum))
		archive.Write(header)
		archive.Write(entry.data)
		archive.Write(make([]byte, (512-len(entry.data)%512)%512))
	}
	archive.Write(make([]byte, 1024))
	packagePath := filepath.Join(dir, "test.fcore")
	write(packagePath, archive.Bytes())
	inspection, err := corepackage.InspectPackage(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(read("../misterruntime/testdata/protocol-v2.jsonl"))), "\n")
	reply := func(index int) string {
		return strings.ReplaceAll(transform(lines[index]), "b131f98291e946c63d94a4b73f13f7ef9efe1bafda9f96ae1a13a2bff5f2a2f0", inspection.PackageID) + "\n"
	}
	// Use a short socket directory to stay within sockaddr_un on every test host.
	socketDir, err := os.MkdirTemp("", "fc-media-wire-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	socket := filepath.Join(socketDir, "runtime.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	defer func() { listener.Close(); <-done }()
	var mediaCalls atomic.Int32
	media := bytes.Repeat([]byte{0, 255, 17, 42}, 4096)
	go func() {
		defer close(done)
		active := false
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			line, err := bufio.NewReader(conn).ReadBytes('\n')
			if err != nil {
				conn.Close()
				continue
			}
			var request map[string]json.RawMessage
			if err := json.Unmarshal(line, &request); err != nil {
				t.Error(err)
				conn.Close()
				continue
			}
			var op string
			json.Unmarshal(request["operation"], &op)
			result := reply(1)
			if active {
				result = reply(5)
			}
			switch op {
			case "status":
			case "inspect_core":
				result = reply(3)
			case "load_core":
				active = true
				result = reply(5)
			case "load_media":
				mediaCalls.Add(1)
				var path string
				json.Unmarshal(request["path"], &path)
				if len(request) != 3 || string(request["protocol"]) != "2" || !filepath.IsAbs(path) {
					t.Errorf("bad media request: %s", line)
				}
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
					t.Errorf("unsafe staging %v %v", info, err)
				}
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, media) {
					t.Errorf("wrong staged bytes: %v", err)
				}
			case "stop":
				active = false
				result = reply(9)
			default:
				t.Errorf("unexpected native operation: %s", line)
			}
			io.WriteString(conn, result)
			conn.Close()
		}
	}()
	if err := os.Mkdir(filepath.Join(dir, "packages"), 0700); err != nil {
		t.Fatal(err)
	}
	native := misterruntime.NewRuntime(misterruntime.NewClient(socket), "", time.Millisecond, time.Second, misterruntime.WithCorePackageRoot(filepath.Join(dir, "packages")))
	coordinator := agent.New(native, core.DefaultRegistry(), time.Second, time.Second)
	coordinator.Initialize(context.Background())
	manager := kitlease.New(time.Minute, func(context.Context) error { return nil })
	defer manager.Close()
	target := httptest.NewServer(httpapi.New(coordinator, "test-token", "test", nil, httpapi.WithDevelopment(coordinator), httpapi.WithKitLease(manager)))
	defer target.Close()
	config := filepath.Join(dir, "config.toml")
	write(config, []byte(fmt.Sprintf("base_url=%q\ntoken='test-token'\nrequest_timeout_seconds=5\nupload_timeout_seconds=30\n", target.URL)))
	service, err := fogcast.Open(context.Background(), fogcast.Paths{Config: config, Index: filepath.Join(dir, "index.sqlite3"), Staging: filepath.Join(dir, "staging")}, target.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	api := httptest.NewServer(hostapi.New(service))
	defer api.Close()
	run := func(command, path string) int {
		t.Helper()
		var out, stderr bytes.Buffer
		code := fogcastcli.Run(context.Background(), []string{"--api", api.URL, "--json", command, path}, &out, &stderr, func(context.Context, fogcast.Paths) (fogcastcli.Service, error) {
			t.Fatal("independent service opened")
			return nil, nil
		})
		if code != 0 {
			t.Logf("%s exit=%d out=%s stderr=%s", command, code, &out, &stderr)
		}
		return code
	}
	mediaPath := filepath.Join(dir, "coleco.rom")
	write(mediaPath, media)
	if run("core-media", mediaPath) == 0 || mediaCalls.Load() != 0 {
		t.Fatal("inactive media admitted")
	}
	if run("core-load", packagePath) != 0 {
		t.Fatal("package activation failed")
	}
	if run("core-media", mediaPath) != 0 || mediaCalls.Load() != 1 {
		t.Fatal("media upload failed")
	}
	response, err := http.Post(api.URL+"/api/v1/session/stop", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("Stop: %d %s", response.StatusCode, body)
	}
}
