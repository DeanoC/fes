//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package catalog

import (
	"os"
	"testing"
)

func TestScanLeaseDirectoryModeRequiresPrivateUnixPermissions(t *testing.T) {
	if !scanLeaseDirectoryModeIsPrivate(os.FileMode(0o700)) {
		t.Fatal("0700 directory mode was rejected")
	}
	if scanLeaseDirectoryModeIsPrivate(os.FileMode(0o777)) {
		t.Fatal("0777 directory mode was accepted")
	}
}
