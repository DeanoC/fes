// SPDX-License-Identifier: GPL-3.0-or-later
package nativefabric

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

type choiceTestFS struct {
	fs.FS
	closes, reads int
	fault         string
	cancel        context.CancelFunc
}

func (f *choiceTestFS) Open(name string) (fs.File, error) {
	file, err := f.FS.Open(name)
	if err != nil {
		return nil, err
	}
	return &choiceTestFile{file, f}, nil
}

type choiceTestFile struct {
	fs.File
	owner *choiceTestFS
}

func (f *choiceTestFile) Close() error {
	f.owner.closes++
	err := f.File.Close()
	if f.owner.fault == "close" {
		return errors.Join(err, errors.New("lost selection close"))
	}
	return err
}
func (f *choiceTestFile) Read(out []byte) (int, error) {
	f.owner.reads++
	if f.owner.fault == "stall" {
		return 0, nil
	}
	if f.owner.fault == "count" {
		return len(out) + 1, nil
	}
	n, err := f.File.Read(out)
	if f.owner.fault == "cancel" {
		f.owner.cancel()
	}
	return n, err
}

func TestProgramSelectionReadsOnlyBoundedCompleteRequestsAndClosesFiles(t *testing.T) {
	choice := programChoice{Path: "FES/ONE.BIN", PackageID: strings.Repeat("a", 64), MetadataPath: "FES/ONE.JSN", MetadataSHA256: strings.Repeat("b", 64)}
	for _, fault := range []string{"pass", "empty", "too-many", "path", "package", "metadata", "unknown", "duplicate", "missing", "oversized", "stall", "count", "cancel", "close"} {
		t.Run(fault, func(t *testing.T) {
			selected := programChoices{1, "nano-program-selection-v1", []programChoice{choice, choice}}
			switch fault {
			case "empty":
				selected.Programs = nil
			case "too-many":
				selected.Programs = make([]programChoice, 17)
			case "path":
				selected.Programs[0].Path = "../outside"
			case "package":
				selected.Programs[0].PackageID = "bad"
			case "metadata":
				selected.Programs[0].MetadataSHA256 = ""
			}
			data := programJSON(t, selected)
			switch fault {
			case "unknown":
				data = bytes.Replace(data, []byte(`"format":1`), []byte(`"extra":0,"format":1`), 1)
			case "duplicate":
				data = bytes.Replace(data, []byte(`"format":1`), []byte(`"format":1,"format":1`), 1)
			case "missing":
				data = bytes.Replace(data, []byte(`"scope":"nano-program-selection-v1"`), []byte(`"other":"nano-program-selection-v1"`), 1)
			case "oversized":
				data = make([]byte, 8193)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			files := &choiceTestFS{FS: fstest.MapFS{"FES/CORESEL.JSN": &fstest.MapFile{Data: data}}, fault: fault, cancel: cancel}
			requests, err := ReadProgramSelections(ctx, files, "FES/CORESEL.JSN")
			if (err == nil) != (fault == "pass") || files.closes != 1 || files.reads > 100 {
				t.Fatal(requests, err, files.closes, files.reads)
			}
			if fault == "pass" && (len(requests) != 2 || requests[0].PackageID != choice.PackageID || requests[1].MetadataSHA256 != choice.MetadataSHA256) {
				t.Fatal(requests)
			}
			if fault != "pass" && requests != nil {
				t.Fatal("failed read published partial choices")
			}
		})
	}
}
