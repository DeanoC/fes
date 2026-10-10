package corepackage

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
)

func TestROMInputStageRejectsDeclaredLengthMismatch(t *testing.T) {
	data, err := WriteROMInput(linkedROMFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, length := range []int64{int64(len(data)) - 1, int64(len(data)) + 1} {
		root := t.TempDir()
		if staged, err := StageROMInput(context.Background(), root, length, bytes.NewReader(data)); err == nil {
			_ = staged.Cleanup()
			t.Fatal("accepted ROM envelope with mismatched declared length")
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 0 {
			t.Fatalf("length mismatch published files: %v %v", entries, err)
		}
	}
}

func TestROMInputStageCancelsDuringReceiptMapDecode(t *testing.T) {
	data, err := WriteROMInput(linkedROMFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	bodyDone := false
	ctx := &romStageCancelContext{Context: context.Background(), bodyDone: &bodyDone}
	root := t.TempDir()
	staged, err := StageROMInput(ctx, root, int64(len(data)), &markAtEOFReader{data: data, done: &bodyDone})
	if err == nil {
		_ = staged.Cleanup()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("receipt map decoding ignored cancellation: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("canceled receipt validation published files: %v %v", entries, err)
	}
}
