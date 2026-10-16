package kitsoftware

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeProbeAuthenticatesPeerAndOnlyReadsStatus(t *testing.T) {
	for _, scenario := range []string{"idle", "reboot_required", "wrong-pid", "wrong-uid", "wrong-inode", "wrong-start", "protocol1", "missing-ok", "active-idle", "oversized", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			socket := filepath.Join(t.TempDir(), "runtime.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			received := make(chan string, 1)
			go func() {
				connection, err := listener.Accept()
				if err != nil {
					received <- ""
					return
				}
				defer connection.Close()
				line, err := bufio.NewReader(connection).ReadString('\n')
				if err != nil {
					received <- ""
					return
				}
				received <- line
				response := `{"protocol":2,"ok":true,"state":"idle","execution":"none","version":"fixture","error":null}`
				switch scenario {
				case "reboot_required":
					response = `{"protocol":2,"ok":true,"state":"reboot_required","execution":"none","version":"fixture","error":{"code":"idle_failed","phase":"recovery","message":"native failure"}}`
				case "protocol1":
					response = strings.Replace(response, `"protocol":2`, `"protocol":1`, 1)
				case "missing-ok":
					response = strings.Replace(response, `"ok":true,`, "", 1)
				case "active-idle":
					response = strings.Replace(response, `"error":null`, `"error":null,"generation":1`, 1)
				case "oversized":
					response = strings.Repeat("x", 65537)
				case "cancel":
					_, _ = io.Copy(io.Discard, connection)
					return
				}
				_, _ = io.WriteString(connection, response+"\n")
			}()
			pid := os.Getpid()
			uid := uint32(os.Getuid())
			start, err := processStartTime(pid)
			if err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "wrong-pid":
				pid++
			case "wrong-uid":
				uid++
			case "wrong-inode":
				exe = "/bin/sh"
			case "wrong-start":
				start = "not-starttime"
			}
			ctx := context.Background()
			if scenario == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 25*time.Millisecond)
				defer cancel()
			}
			status, err := readNativeStatusPeer(ctx, socket, pid, uid, start, exe)
			wantOK := scenario == "idle" || scenario == "reboot_required"
			if (err == nil) != wantOK {
				t.Fatalf("%s: %+v %v", scenario, status, err)
			}
			line := <-received
			if strings.HasPrefix(scenario, "wrong-") {
				if line != "" {
					t.Fatal("unverified peer received a request", line)
				}
			} else {
				var request map[string]any
				if err := json.Unmarshal([]byte(line), &request); err != nil {
					t.Fatal(err)
				}
				if len(request) != 2 || request["protocol"] != float64(2) || request["operation"] != "status" {
					t.Fatal("native mutation attempted", line)
				}
			}
		})
	}
}
