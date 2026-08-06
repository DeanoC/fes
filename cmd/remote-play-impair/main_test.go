package main

import (
	"context"
	"os"
	"testing"
)

func TestImpairmentStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := run(ctx, nil, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("cancelled impairment: %v", err)
	}
}

func TestImpairmentRejectsInvalidDropConfiguration(t *testing.T) {
	if err := run(context.Background(), []string{"--drop-every", "1"}, os.Stdout, os.Stderr); err == nil {
		t.Fatal("drop-every=1 accepted")
	}
}

func TestImpairmentRejectsOversizedReorderWindow(t *testing.T) {
	if err := run(context.Background(), []string{"--reorder-window", "65"}, os.Stdout, os.Stderr); err == nil {
		t.Fatal("reorder-window=65 accepted")
	}
}
