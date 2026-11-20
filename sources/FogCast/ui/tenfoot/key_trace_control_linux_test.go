//go:build linux

package tenfoot

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestKeyTraceControlConsumesOnlyFreshPrivateOwnPIDRequest(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "request")
	pid := os.Getpid()
	uid := os.Geteuid()
	start, err := keyTraceProcessStart()
	if err != nil {
		t.Fatal(err)
	}
	write := func(value string, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), mode); err != nil {
			t.Fatal(err)
		}
	}
	write(fmt.Sprintf("%d %s\n", pid, start), 0o600)
	if !consumeKeyTraceRequest(path, pid, start, uid, time.Now().Add(time.Second)) {
		di, _ := os.Lstat(dir)
		fi, _ := os.Lstat(path)
		data, _ := os.ReadFile(path)
		t.Fatalf("valid request not consumed: parent=%+v file=%+v content=%q pid=%d start=%q uid=%d", di, fi, data, pid, start, uid)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("consumed request remains", err)
	}
	write(fmt.Sprintf("%d %s\n", pid+1, start), 0o600)
	if consumeKeyTraceRequest(path, pid, start, uid, time.Now().Add(time.Second)) {
		t.Fatal("wrong PID accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	write(fmt.Sprintf("%d wrong-start\n", pid), 0o600)
	if consumeKeyTraceRequest(path, pid, start, uid, time.Now().Add(time.Second)) {
		t.Fatal("reused PID accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	write(fmt.Sprintf("%d %s\n", pid, start), 0o644)
	if consumeKeyTraceRequest(path, pid, start, uid, time.Now().Add(time.Second)) {
		t.Fatal("insecure mode accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	write(fmt.Sprintf("%d %s\n", pid, start), 0o600)
	if consumeKeyTraceRequest(path, pid, start, uid, time.Now().Add(11*time.Second)) {
		t.Fatal("stale request accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte(fmt.Sprintf("%d %s\n", pid, start)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if consumeKeyTraceRequest(path, pid, start, uid, time.Now().Add(time.Second)) {
		t.Fatal("symlink accepted")
	}
	if _, err := os.Lstat(target); err != nil {
		t.Fatal("symlink target touched", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	write(fmt.Sprintf("%d %s\n", pid, start), 0o600)
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if consumeKeyTraceRequest(path, pid, start, uid, time.Now().Add(time.Second)) {
		t.Fatal("insecure directory accepted")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("insecure request was removed", err)
	}
}
