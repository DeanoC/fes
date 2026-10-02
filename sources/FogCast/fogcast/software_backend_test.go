package fogcast

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/DeanoC/FogCast/protocol"
)

func TestSoftwareBackendsReportObservedCoreIdentityAndAvailability(t *testing.T) {
	corePath := filepath.Join(t.TempDir(), "genesis_plus_gx_libretro.so")
	binaryPath := filepath.Join(t.TempDir(), "retroarch")
	if err := os.WriteFile(binaryPath, []byte("runner"), 0o700); err != nil {
		t.Fatal(err)
	}
	content := []byte("fixture core")
	if err := os.WriteFile(corePath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	s := &Service{hostEmulator: HostEmulatorConfig{Binary: binaryPath, Cores: []HostEmulatorCore{{Platform: protocol.SystemSMS, Core: corePath, ID: "genesis_plus_gx", Version: " c2838c7d", SHA256: hex.EncodeToString(sum[:])}}}}
	backends := s.SoftwareBackends()
	if len(backends) != 1 {
		t.Fatalf("backends = %#v", backends)
	}
	b := backends[0]
	if !b.Available || b.Execution != "native_emu" || b.Emulator != "retroarch" || b.CoreID != "genesis_plus_gx" || b.CoreVersion != " c2838c7d" || b.CoreSHA256 != hex.EncodeToString(sum[:]) || b.System != "sms" {
		t.Fatalf("backend = %#v", b)
	}
	s.hostEmulator.Cores[0].SHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if got := s.SoftwareBackends()[0].Available; got {
		t.Fatal("pinned digest mismatch advertised available")
	}
	s.hostEmulator.Cores[0].SHA256 = hex.EncodeToString(sum[:])
	s.hostEmulator.Binary = filepath.Join(t.TempDir(), "missing-retroarch")
	if got := s.SoftwareBackends()[0].Available; got {
		t.Fatal("missing RetroArch binary advertised available")
	}
	if err := os.WriteFile(s.hostEmulator.Binary, []byte("runner"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.SoftwareBackends()[0].Available; got {
		t.Fatal("non-executable RetroArch binary advertised available")
	}
}

func TestSoftwareBackendsRejectNonRegularCoreWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, "core.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	s := &Service{hostEmulator: HostEmulatorConfig{Binary: filepath.Join(root, "runner"), Cores: []HostEmulatorCore{{Platform: protocol.SystemSMS, Core: fifo}}}}
	if err := os.WriteFile(s.hostEmulator.Binary, []byte("runner"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	backends := s.SoftwareBackendsContext(ctx)
	if len(backends) != 1 || backends[0].Available || backends[0].CoreSHA256 != "" {
		t.Fatalf("FIFO backend = %#v", backends)
	}
	cancel()
	if got := s.SoftwareBackendsContext(ctx)[0].Available; got {
		t.Fatal("canceled health context advertised core available")
	}
}
