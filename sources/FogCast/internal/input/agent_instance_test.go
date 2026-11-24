package input

import (
	"bytes"
	"strings"
	"testing"
)

func TestLocalAgentInstanceIsPerControllerAndFailsClosed(t *testing.T) {
	a := &TargetController{localEntropy: bytes.NewReader(bytes.Repeat([]byte{1}, 16))}
	b := &TargetController{localEntropy: bytes.NewReader(bytes.Repeat([]byte{2}, 16))}
	first, err := a.LocalAgentInstance()
	if err != nil || first != strings.Repeat("01", 16) {
		t.Fatalf("first %q %v", first, err)
	}
	repeat, err := a.LocalAgentInstance()
	if err != nil || repeat != first {
		t.Fatalf("unstable %q %v", repeat, err)
	}
	next, err := b.LocalAgentInstance()
	if err != nil || next == first {
		t.Fatalf("reused %q %v", next, err)
	}
	for _, bad := range [][]byte{nil, make([]byte, 16)} {
		c := &TargetController{localEntropy: bytes.NewReader(bad)}
		if value, err := c.LocalAgentInstance(); err == nil || value != "" {
			t.Fatalf("invalid identity %q %v", value, err)
		}
	}
}
