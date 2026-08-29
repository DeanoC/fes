//go:build linux && fpgadev

package fpgadev

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseInventoryProcNetRowsAcceptsUnrelatedZeroInode(t *testing.T) {
	raw := []byte("sl local_address rem_address st tx_queue tr tm->when retrnsmt uid timeout inode\n" +
		"  0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 0 0 0\n")
	rows, err := parseInventoryProcNetRows(raw)
	if err != nil {
		t.Fatalf("inode-zero row rejected: %v", err)
	}
	if len(rows) != 1 || rows[0].Inode != 0 {
		t.Fatalf("rows=%#v, want one inode-zero row", rows)
	}
}

func TestExpectedAbsentNetworkEvidenceFailsClosedWhenUnavailableOrMalformed(t *testing.T) {
	endpoint := NetworkExpectation{Network: "tcp", Address: "127.0.0.1:8080"}
	if err := verifyInventoryNetworkEndpoint(context.Background(), filepath.Join(t.TempDir(), "missing"), endpoint, true); err == nil {
		t.Fatal("unavailable proc network evidence was accepted")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "net", "tcp"), []byte("malformed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyInventoryNetworkEndpoint(context.Background(), root, endpoint, true); err == nil {
		t.Fatal("malformed proc network evidence was accepted")
	}
}
