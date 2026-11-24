package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type localIdentityReceiver struct {
	target, boot string
	calls        int
	err          error
}

func (r *localIdentityReceiver) SetLocalTargetIdentity(target, boot string) error {
	r.calls++
	r.target, r.boot = target, boot
	return r.err
}

func TestLocalTargetIdentityUsesResolvedTargetAndBoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "boot-id")
	if err := os.WriteFile(path, []byte("boot-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &localIdentityReceiver{}
	if err := configureLocalTargetIdentity(r, "kit-target", path); err != nil || r.calls != 1 || r.target != "kit-target" || r.boot != "boot-value" {
		t.Fatalf("receiver=%+v err=%v", r, err)
	}
	r.err = errors.New("invalid")
	if err := configureLocalTargetIdentity(r, "kit-target", path); !errors.Is(err, r.err) {
		t.Fatalf("validation error: %v", err)
	}
	r.calls = 0
	if err := configureLocalTargetIdentity(r, "kit-target", filepath.Join(t.TempDir(), "missing")); err == nil || r.calls != 0 {
		t.Fatalf("missing boot accepted: %v", err)
	}
}
