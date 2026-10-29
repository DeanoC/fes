package corepackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRetainArchiveSharedConformance(t *testing.T) {
	for _, version := range []string{"2", "3", "4"} {
		root := filepath.Join("testdata", "core-bundle-v"+version)
		index, err := os.ReadFile(filepath.Join(root, "cases.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cases []struct {
			Name, Manifest, Payload string
			Map                     string `json:"rom_map"`
			Valid                   bool
			PackageID               string   `json:"package_id"`
			Members                 []string `json:"archive_members"`
		}
		if err = json.Unmarshal(index, &cases); err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			t.Run("v"+version+"/"+c.Name, func(t *testing.T) {
				read := func(path string) []byte {
					t.Helper()
					v, err := os.ReadFile(filepath.Join(root, path))
					if err != nil {
						t.Fatal(err)
					}
					return v
				}
				manifest, payload := read(c.Manifest), read(c.Payload)
				members := map[string][]byte{"manifest.toml": manifest, "core.rbf": payload}
				order := []string{"manifest.toml", "core.rbf"}
				if c.Map != "" {
					members["rom-map.json"] = read(c.Map)
					order = append(order, "rom-map.json")
				}
				if c.Members != nil {
					order = c.Members
				}
				var archive []byte
				for _, name := range order {
					value, known := members[name]
					if !known {
						value = []byte("unexpected member")
					}
					archive = append(archive, canonicalHeader(name, int64(len(value)))...)
					archive = append(archive, value...)
					archive = append(archive, make([]byte, (-len(value))&511)...)
				}
				archive = append(archive, make([]byte, 1024)...)

				retained, err := RetainArchive(context.Background(), int64(len(archive)), bytes.NewReader(archive))
				if !c.Valid {
					if err == nil {
						t.Fatal("invalid shared case accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				before := retained.Inspection()
				if before.PackageID != c.PackageID {
					t.Fatalf("identity %s want %s", before.PackageID, c.PackageID)
				}
				// All caller-owned inputs and returned metadata are independently mutable.
				clear(archive)
				edited := retained.Inspection()
				edited.Descriptor.Interfaces[0].ID = "changed"
				if edited.Descriptor.ROM != nil {
					edited.Descriptor.ROM.ID = "changed"
				}
				if edited.Descriptor.ROMMap != nil {
					edited.Descriptor.ROMMap.File = "changed"
				}
				if len(edited.Descriptor.ROMs) > 0 {
					edited.Descriptor.ROMs[0].ID = "changed"
				}
				if !reflect.DeepEqual(before, retained.Inspection()) {
					t.Fatal("returned descriptor changed retained inspection")
				}
				reader := retained.Payload()
				if _, ok := reader.(io.WriterTo); ok {
					t.Fatal("payload reader exposes direct WriteTo access")
				}
				got, err := io.ReadAll(reader)
				if err != nil || !bytes.Equal(got, payload) {
					t.Fatalf("retained payload: %v", err)
				}
				clear(got)
				got, err = io.ReadAll(retained.Payload())
				if err != nil || !bytes.Equal(got, payload) {
					t.Fatal("caller buffer changed retained payload")
				}
			})
		}
	}
}

type archiveReaderFunc func([]byte) (int, error)

func (f archiveReaderFunc) Read(p []byte) (int, error) { return f(p) }

func TestRetainArchiveInputFailures(t *testing.T) {
	manifest, payload := fixtureBytes(t, loadCases(t)[0])
	archive := canonicalArchive(manifest, payload)
	wantErr := errors.New("source read failed")
	for name, reader := range map[string]io.Reader{
		"no progress":     archiveReaderFunc(func([]byte) (int, error) { return 0, nil }),
		"negative count":  archiveReaderFunc(func([]byte) (int, error) { return -1, nil }),
		"oversized count": archiveReaderFunc(func(p []byte) (int, error) { return len(p) + 1, nil }),
		"source error":    archiveReaderFunc(func([]byte) (int, error) { return 0, wantErr }),
		"short body":      bytes.NewReader(archive[:len(archive)-1]),
		"extra body":      bytes.NewReader(append(bytes.Clone(archive), 0)),
	} {
		t.Run(name, func(t *testing.T) {
			if retained, err := RetainArchive(context.Background(), int64(len(archive)), reader); err == nil || retained != nil {
				t.Fatalf("retained=%v error=%v", retained, err)
			}
		})
	}
	for _, length := range []int64{-1, 0, MaxArchiveSize + 1} {
		calls := 0
		_, err := RetainArchive(context.Background(), length, archiveReaderFunc(func([]byte) (int, error) { calls++; return 0, nil }))
		if err == nil || calls != 0 {
			t.Fatalf("invalid length read input: %d/%d/%v", length, calls, err)
		}
	}
	if _, err := RetainArchive(nil, 1, bytes.NewReader(archive)); err == nil {
		t.Fatal("nil context accepted")
	}
	if _, err := RetainArchive(context.Background(), 1, nil); err == nil {
		t.Fatal("nil reader accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RetainArchive(ctx, int64(len(archive)), bytes.NewReader(archive)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	if _, err := RetainArchive(ctx, int64(len(archive)), &cancelAtEOFReader{data: archive, cancel: cancel}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation at EOF: %v", err)
	}
}
