package testsock

import (
	"net"
	"strings"
	"testing"
)

func TestPathFitsUnderALongTMPDIR(t *testing.T) {
	t.Setenv("TMPDIR", "/tmp/"+strings.Repeat("long-temporary-directory-", 4))
	path := Path(t, "a-socket-name-that-is-also-long.sock")
	if len(path) > maxPath {
		t.Fatalf("path %d bytes", len(path))
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	_ = l.Close()
}
