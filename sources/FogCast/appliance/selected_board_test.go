package appliance

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type unreadManifest struct{ read bool }

func (r *unreadManifest) Read([]byte) (int, error) { r.read = true; return 0, io.EOF }

func TestSelectedReleaseBoard(t *testing.T) {
	base, _, err := DecodeManifest(strings.NewReader(validJSON()))
	if err != nil {
		t.Fatal(err)
	}
	for _, board := range []string{Board, BoardDE25} {
		t.Run(board, func(t *testing.T) {
			m := base
			m.Board = board
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			got, _, err := DecodeManifestForBoard(strings.NewReader(string(raw)), board)
			if err != nil || got != m {
				t.Fatalf("selected board decode: %+v %v", got, err)
			}
			if err = m.CompatibleForBoard(m.KernelSHA256, board); err != nil {
				t.Fatal(err)
			}
			if err = m.CompatibleForBoard(strings.Repeat("f", 64), board); err == nil {
				t.Fatal("selected board bypassed the kernel pin")
			}
			other := BoardDE25
			if board == BoardDE25 {
				other = Board
				if _, _, err = DecodeManifest(strings.NewReader(string(raw))); err == nil || m.Validate() == nil || m.Compatible(m.KernelSHA256) == nil {
					t.Fatal("DE10 default admitted DE25")
				}
			}
			if _, _, err = DecodeManifestForBoard(strings.NewReader(string(raw)), other); err == nil {
				t.Fatal("foreign board decoded")
			}
			if m.CompatibleForBoard(m.KernelSHA256, other) == nil {
				t.Fatal("foreign board compatible")
			}
		})
	}
	for _, board := range []string{"", "de25_nano", "de25-nano\n", "other"} {
		r := &unreadManifest{}
		if _, _, err := DecodeManifestForBoard(r, board); err == nil || r.read {
			t.Fatalf("unknown target consumed input: %q %v", board, err)
		}
	}
}
