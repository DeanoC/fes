package appliance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	release "github.com/DeanoC/FogCast/appliance"
)

type unreadImage struct{ read bool }

func (r *unreadImage) Read([]byte) (int, error) { r.read = true; return 0, io.EOF }

func releaseFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil {
			files[strings.TrimPrefix(path, root)] = fmt.Sprintf("%x", sha256.Sum256(data))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func selectedBoardLifecycle(t *testing.T, board string, original, next []byte) {
	t.Helper()
	ctx := context.Background()
	factory := manifest(original)
	factory.Board = board
	root := filepath.Join(t.TempDir(), "releases")
	s, err := NewForBoard(root, factory, board)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Stage(ctx, factory, factory.ImageSize, bytes.NewReader(original)); err != nil {
		t.Fatal(err)
	}
	candidate := manifest(next)
	candidate.Board = board
	if err = s.Stage(ctx, candidate, candidate.ImageSize, bytes.NewReader(next)); err != nil {
		t.Fatal(err)
	}
	before := releaseFiles(t, root)
	for _, badBoard := range []string{release.Board, release.BoardDE25, "other"} {
		if badBoard == board {
			continue
		}
		foreign := candidate
		foreign.Board = badBoard
		reader := &unreadImage{}
		if err = s.Stage(ctx, foreign, foreign.ImageSize, reader); err == nil || reader.read {
			t.Fatalf("foreign upload reached reader: %q %v", badBoard, err)
		}
		if !reflect.DeepEqual(before, releaseFiles(t, root)) {
			t.Fatal("foreign upload mutated release files")
		}
	}
	if err = s.Activate(candidate.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	choice, err := s.BeginBoot("selected-board-trial")
	if err != nil || !choice.Trial || choice.Manifest != candidate {
		t.Fatalf("wrong trial: %+v %v", choice, err)
	}
	if err = s.Confirm(choice.BootID, candidate.ImageSHA256); err != nil {
		t.Fatal(err)
	}
	s, err = NewForBoard(root, factory, board)
	if err != nil {
		t.Fatal(err)
	}
	choice, err = s.BeginBoot("selected-board-reopen")
	if err != nil || choice.Trial || choice.Manifest != candidate {
		t.Fatalf("wrong durable selection after reopen: %+v %v", choice, err)
	}
	if _, err = s.Verify(factory.ImageSHA256); err != nil {
		t.Fatal("factory was not retained:", err)
	}
	foreign := candidate
	foreign.Board = release.BoardDE25
	if board == release.BoardDE25 {
		foreign.Board = release.Board
	}
	raw, _ := json.Marshal(foreign)
	if err = os.WriteFile(filepath.Join(root, "manifests", candidate.ImageSHA256+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	// Refuse the retained foreign manifest before trying to open image bytes.
	image, _ := s.ImagePath(candidate.ImageSHA256)
	if err = os.Remove(image); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Verify(candidate.ImageSHA256); err == nil || !strings.Contains(err.Error(), "board") {
		t.Fatalf("retained foreign manifest reached image access: %v", err)
	}
}

func TestSelectedBoardReleaseLifecycle(t *testing.T) {
	original := make([]byte, 4096)
	original[1080], original[1081], original[1120] = 0x53, 0xef, 0x40
	next := bytes.Clone(original)
	next[0] = 1
	for _, board := range []string{release.Board, release.BoardDE25} {
		t.Run(board, func(t *testing.T) { selectedBoardLifecycle(t, board, original, next) })
	}
	for _, board := range []string{release.Board, "unknown"} {
		factory := manifest(original)
		factory.Board = release.BoardDE25
		root := filepath.Join(t.TempDir(), "must-not-exist")
		if _, err := NewForBoard(root, factory, board); err == nil {
			t.Fatal("foreign/unknown factory accepted")
		}
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			t.Fatal("rejected constructor created storage")
		}
		if _, err := New(root, factory); err == nil {
			t.Fatal("DE10 default admitted DE25 factory")
		}
	}
}

func TestSelectedBoardRealImages(t *testing.T) {
	first, next := os.Getenv("FES_SELECTED_BOARD_FACTORY"), os.Getenv("FES_SELECTED_BOARD_CANDIDATE")
	if first == "" && next == "" {
		t.Skip("requires two separately constructed regular ext4 image fixtures")
	}
	original, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := os.ReadFile(next)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(original, candidate) {
		t.Fatal("the real image fixtures must differ")
	}
	for _, board := range []string{release.Board, release.BoardDE25} {
		t.Run(board, func(t *testing.T) { selectedBoardLifecycle(t, board, original, candidate) })
	}
}
