// SPDX-License-Identifier: GPL-3.0-or-later
package packagestore

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestRetainedMetadataIsClosedPrivateAndIndependentOfCardAfterContainment(t *testing.T) {
	body, request, _ := basic(t)
	metadata := []byte("declared program metadata; interpreted only by native preflight")
	request.MetadataPath, request.MetadataSHA256 = "FES/PROGRAM.JSN", hash(metadata)
	l := lease()
	files := fstest.MapFS{request.Path: &fstest.MapFile{Data: body}, request.MetadataPath: &fstest.MapFile{Data: metadata}}
	s, tracked := open(t, files, l)
	selected, err := s.Retain(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(metadata)
	clear(metadata)
	copy := selected.Metadata()
	clear(copy)
	if !bytes.Equal(selected.Metadata(), want) || tracked.handles.Load() != 0 || l.stops != 0 {
		t.Fatal("metadata ownership or file closure lost")
	}
	if _, err = s.Contain(selected); err != nil {
		t.Fatal(err)
	}
	reads, opens := tracked.reads.Load(), tracked.opens.Load()
	if !bytes.Equal(selected.Metadata(), want) || tracked.reads.Load() != reads || tracked.opens.Load() != opens {
		t.Fatal("metadata consulted contained storage")
	}
}

func TestMetadataFailuresKeepThePreparedStorageLease(t *testing.T) {
	for _, fault := range []string{"path-only", "digest-only", "path", "digest", "wrong-digest", "empty", "oversized", "missing"} {
		t.Run(fault, func(t *testing.T) {
			body, request, _ := basic(t)
			metadata := []byte("metadata")
			request.MetadataPath, request.MetadataSHA256 = "FES/PROGRAM.JSN", hash(metadata)
			switch fault {
			case "path-only":
				request.MetadataSHA256 = ""
			case "digest-only":
				request.MetadataPath = ""
			case "path":
				request.MetadataPath = "../outside"
			case "digest":
				request.MetadataSHA256 = "bad"
			case "wrong-digest":
				request.MetadataSHA256 = strings.Repeat("f", 64)
			case "empty":
				metadata = nil
				request.MetadataSHA256 = hash(metadata)
			case "oversized":
				metadata = bytes.Repeat([]byte{'x'}, (16<<10)+1)
				request.MetadataSHA256 = hash(metadata)
			}
			files := fstest.MapFS{request.Path: &fstest.MapFile{Data: body}, "FES/PROGRAM.JSN": &fstest.MapFile{Data: metadata}}
			if fault == "missing" {
				delete(files, "FES/PROGRAM.JSN")
			}
			l := lease()
			s, tracked := open(t, files, l)
			if selected, err := s.Retain(context.Background(), request); err == nil || selected != nil {
				t.Fatal("invalid metadata retained")
			}
			r, ok := l.LastReceipt()
			if !ok || !r.Active || !r.Prepared || r.Generation != 3 || l.stops != 0 || tracked.handles.Load() != 0 {
				t.Fatal("metadata rejection changed SD ownership or leaked a file")
			}
		})
	}
}
