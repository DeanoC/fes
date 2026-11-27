// SPDX-License-Identifier: GPL-3.0-or-later

package agentapi_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/fogcast"
	"github.com/DeanoC/FogCast/kitlease"
	"github.com/DeanoC/fes/platform/embedded/tamago/network"
	"golang.org/x/sys/unix"
)

// A bound packet socket supplies the native stack's Ethernet device. The
// host uses its ordinary kernel sockets on the other end of the veth pair.
type vethDevice int

func (fd vethDevice) Receive(buf []byte) (int, error) {
	n, err := unix.Read(int(fd), buf)
	if err == unix.EAGAIN || err == unix.EINTR {
		return 0, nil
	}
	return n, err
}

func (fd vethDevice) Transmit(frame []byte) error {
	_, err := unix.Write(int(fd), frame)
	return err
}

func (vethDevice) Err() error { return nil }

// FES_TEST_NATIVE_MULTICAST=1 go test ./agentapi -run TestHostDiscoversNativeTarget -count=1
// Requires Linux user/network namespaces, unshare, ip and ethtool.
func TestHostDiscoversNativeTarget(t *testing.T) {
	switch os.Getenv("FES_TEST_NATIVE_MULTICAST") {
	case "1":
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), "unshare", "--user", "--map-root-user", "--net", self, "-test.run=^TestHostDiscoversNativeTarget$", "-test.v", "-test.timeout=30s")
		cmd.Env = append(os.Environ(), "FES_TEST_NATIVE_MULTICAST=child")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated multicast test: %v\n%s", err, out)
		}
		return
	case "child":
	default:
		t.Skip("set FES_TEST_NATIVE_MULTICAST=1 to test the desktop resolver in a private network namespace")
	}
	ifaces, err := net.Interfaces()
	if err != nil || len(ifaces) != 1 || ifaces[0].Name != "lo" || ifaces[0].Flags&net.FlagUp != 0 {
		t.Fatalf("expected a new network namespace with only a down loopback; interfaces=%v err=%v", ifaces, err)
	}
	for _, args := range [][]string{
		{"ip", "link", "add", "feshost", "type", "veth", "peer", "name", "fesguest"},
		{"ip", "addr", "add", "192.0.2.20/24", "dev", "feshost"},
		{"ip", "link", "set", "lo", "up"},
		{"ip", "link", "set", "feshost", "up"},
		{"ip", "link", "set", "fesguest", "up"},
		// AF_PACKET delivers the frame to the guest before host TX offload.
		{"ethtool", "-K", "feshost", "tx", "off"},
	} {
		if out, err := exec.CommandContext(t.Context(), args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	guest, err := net.InterfaceByName("fesguest")
	if err != nil {
		t.Fatal(err)
	}
	etherTypes := int(binary.NativeEndian.Uint16([]byte{0, unix.ETH_P_ALL}))
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, etherTypes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Ifindex: guest.Index, Protocol: uint16(etherTypes)}); err != nil {
		t.Fatal(err)
	}
	link, err := network.NewLink(nil, vethDevice(fd), "192.0.2.10/24", guest.HardwareAddr.String(), "192.0.2.20")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	done := make(chan struct{})
	go func() { defer close(done); _ = link.Start(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	ownership := &admissionLease{status: kitlease.Status{State: "held", Owner: "local-console", Generation: "generation-2"}}
	target := startAdmissionTarget(t, ctx, link, guest.HardwareAddr.String(), ownership)
	// Host browsing must work after the target has finished probing and
	// advertising its name, including when it must answer a fresh query.
	for target.Stats().MDNS.Announcements < 2 {
		select {
		case <-ctx.Done():
			t.Fatal("target never completed its startup announcements")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Start from a real, closed endpoint. Only DNS-SD knows the current
	// address. The target already has a provisioned identity and bearer.
	previous, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	previousAddress := previous.Addr().String()
	if err := previous.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	paths := fogcast.Paths{Config: filepath.Join(dir, "config.toml"), Index: filepath.Join(dir, "index.sqlite3"), Staging: filepath.Join(dir, "staging")}
	config := fmt.Sprintf(`selected_target = "native"
request_timeout_seconds = 5
upload_timeout_seconds = 5

[[targets]]
name = "native"
enabled = true
address = %q
target_id = %q
agent = %q
`, "http://"+previousAddress, admissionTargetID, admissionBearer)
	if err := os.WriteFile(paths.Config, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	// The two private interfaces are the only route to the guest.
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	host, err := fogcast.Open(ctx, paths, &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	health, err := host.Health(ctx)
	conn := host.TargetConnection()
	endpoint := "http://" + target.Addr().String()
	if err != nil || !health.Ready || health.APIVersion != "v1" || health.TargetID != admissionTargetID || health.BootID != admissionBootID ||
		conn.Address != endpoint || conn.State != "busy" || conn.Owner != "local-console" || conn.LeaseGeneration != "generation-2" || conn.LeaseOwned {
		t.Fatalf("discovered admission: health=%+v conn=%+v err=%v guest=%+v", health, conn, err, target.Stats())
	}
	ownership.set(kitlease.Status{State: "free"})
	if _, err := host.Health(ctx); err != nil || host.TargetConnection().State != "ready" {
		t.Fatalf("released target: connection=%+v err=%v", host.TargetConnection(), err)
	}
	browse, finishBrowse := context.WithTimeout(ctx, time.Second)
	nodes, err := host.ObserveMesh(browse)
	finishBrowse()
	if err != nil || len(nodes) != 1 || nodes[0].TargetID != admissionTargetID || nodes[0].Address != endpoint || nodes[0].AddressConflict {
		t.Fatalf("desktop DNS-SD inventory = %+v, %v", nodes, err)
	}
	if stats := target.Stats().MDNS; stats.QueriesReceived == 0 || stats.ResponsesSent == 0 {
		t.Fatalf("no multicast exchange recorded by target: %+v", stats)
	}
}
