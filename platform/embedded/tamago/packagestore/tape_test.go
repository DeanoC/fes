// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

func TestTapeRetentionClosesFilesAndSurvivesCardChangeAndContainment(t *testing.T) {
	body, request, _ := basic(t)
	image := bytes.Repeat([]byte{0x12, 0x34, 0x56}, 513)
	want := bytes.Clone(image)
	request.Tape = TapeRequest{Path: "GAMES/TEST.P", SHA256: hash(image), Size: int64(len(image))}
	l := lease()
	files := fstest.MapFS{request.Path: &fstest.MapFile{Data: body}, request.Tape.Path: &fstest.MapFile{Data: image}}
	s, tracked := open(t, files, l)
	selection, err := s.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	clear(image)
	clear(selection.Tape())
	delete(files, request.Tape.Path)
	if !bytes.Equal(selection.Tape(), want) || tracked.handles.Load() != 0 || l.stops != 0 {
		t.Fatal("tape was not privately retained with all files closed")
	}
	if _, err := s.Contain(selection); err != nil {
		t.Fatal(err)
	}
	reads, opens := tracked.reads.Load(), tracked.opens.Load()
	if !bytes.Equal(selection.Tape(), want) || tracked.reads.Load() != reads || tracked.opens.Load() != opens {
		t.Fatal("tape access consulted contained SD")
	}
	if err := s.Resume(); err != nil {
		t.Fatal(err)
	}
	if next, err := s.Retain(context.Background(), request); err == nil || next != nil || l.stops != 1 {
		t.Fatal("new launch reused old tape after it disappeared from the card")
	}
}

func TestRejectedTapeNeverContainsStorageOrReturnsPartialSelection(t *testing.T) {
	for _, fault := range []string{"path-only", "digest-only", "size-only", "traversal", "hash", "zero", "oversize", "short", "changed", "missing", "close", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			body, request, _ := basic(t)
			image := []byte{1, 2, 3}
			request.Tape = TapeRequest{Path: "GAMES/TEST.P", SHA256: hash(image), Size: 3}
			switch fault {
			case "path-only":
				request.Tape = TapeRequest{Path: request.Tape.Path}
			case "digest-only":
				request.Tape = TapeRequest{SHA256: request.Tape.SHA256}
			case "size-only":
				request.Tape = TapeRequest{Size: 3}
			case "traversal":
				request.Tape.Path = "../game.p"
			case "hash":
				request.Tape.SHA256 = strings.Repeat("a", 64)
			case "zero":
				request.Tape.Size = 0
			case "oversize":
				request.Tape.Size = MaxRetainedTapeBytes + 1
			case "short":
				request.Tape.Size = 4
			case "changed":
				image[0]++
			}
			files := fstest.MapFS{request.Path: &fstest.MapFile{Data: body}, "GAMES/TEST.P": &fstest.MapFile{Data: image}}
			if fault == "missing" {
				delete(files, "GAMES/TEST.P")
			}
			l := lease()
			s, tracked := open(t, files, l)
			if fault == "close" {
				tracked.closeErr = errors.New("close failed")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "cancel" {
				cancel()
			}
			if selection, err := s.Retain(ctx, request); err == nil || selection != nil {
				t.Fatal("bad tape published a selection")
			}
			if !l.r.Active || !l.r.Prepared || l.r.Generation != 3 || l.stops != 0 || tracked.handles.Load() != 0 {
				t.Fatal("rejected tape changed storage ownership or leaked a file")
			}
		})
	}
}
