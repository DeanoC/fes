package fpgadev

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDesignationVerifyFailsClosedForEveryOperatorRecordFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "absent", err: errors.New("designation absent")},
		{name: "ambiguous", err: errors.New("designation ambiguous")},
		{name: "mismatch", err: errors.New("target identity mismatch")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			designation := newFixtureDesignation(tt.err)
			if err := designation.Verify(context.Background()); !errors.Is(err, tt.err) {
				t.Fatalf("Verify() error = %v, want %v", err, tt.err)
			}
		})
	}
}

func TestDesignationVerifyAcceptsOnlyTheExactFixtureIdentity(t *testing.T) {
	designation := newFixtureDesignation(nil)
	if err := designation.Verify(context.Background()); err != nil {
		t.Fatalf("Verify() = %v", err)
	}
}

func TestDesignationVerifyHonorsCancellationBeforeReadingPrivateState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	designation := newFixtureDesignation(nil)
	if err := designation.Verify(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify() error = %v, want context.Canceled", err)
	}
}

func TestDesignationProductionFixtureRequiresCanonicalMatchingPrivateRecords(t *testing.T) {
	root := t.TempDir()
	designationPath := filepath.Join(root, "designation")
	identityPath := filepath.Join(root, "identity")
	writeRecord := func(path string, value privateDesignationRecord) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRecord(designationPath, privateDesignationRecord{Schema: 1, TargetID: "kit-a", Designated: true})
	writeRecord(identityPath, privateDesignationRecord{Schema: 1, TargetID: "kit-a"})
	if err := newFixtureFileDesignation(designationPath, identityPath, uint32(os.Getuid())).Verify(context.Background()); err != nil {
		t.Fatalf("matching records rejected: %v", err)
	}
	writeRecord(identityPath, privateDesignationRecord{Schema: 1, TargetID: "kit-b"})
	if err := newFixtureFileDesignation(designationPath, identityPath, uint32(os.Getuid())).Verify(context.Background()); !errors.Is(err, ErrDesignation) {
		t.Fatalf("mismatched records error = %v", err)
	}
	if err := os.Remove(identityPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Base(designationPath), identityPath); err != nil {
		t.Fatal(err)
	}
	if err := newFixtureFileDesignation(designationPath, identityPath, uint32(os.Getuid())).Verify(context.Background()); !errors.Is(err, ErrDesignation) {
		t.Fatalf("symlink identity error = %v", err)
	}
}

func TestDesignationRejectsDuplicateAndNoncanonicalPrivateRecords(t *testing.T) {
	root := t.TempDir()
	designationPath := filepath.Join(root, "designation")
	identityPath := filepath.Join(root, "identity")
	if err := os.WriteFile(designationPath, []byte(`{"schema":1,"target_id":"kit-a","target_id":"kit-a","designated":true}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(identityPath, []byte(`{"schema":1,"target_id":"kit-a","designated":false}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := newFixtureFileDesignation(designationPath, identityPath, uint32(os.Getuid())).Verify(context.Background()); !errors.Is(err, ErrDesignation) {
		t.Fatalf("duplicate designation record error = %v, want ErrDesignation", err)
	}
	if err := os.WriteFile(designationPath, []byte(" {\"schema\":1,\"target_id\":\"kit-a\",\"designated\":true}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := newFixtureFileDesignation(designationPath, identityPath, uint32(os.Getuid())).Verify(context.Background()); !errors.Is(err, ErrDesignation) {
		t.Fatalf("noncanonical designation record error = %v, want ErrDesignation", err)
	}
}

func TestDesignationRejectsSpecialPermissionBits(t *testing.T) {
	root := t.TempDir()
	designationPath := filepath.Join(root, "designation")
	identityPath := filepath.Join(root, "identity")
	writeRecord := func(path string, value privateDesignationRecord) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeRecord(designationPath, privateDesignationRecord{Schema: 1, TargetID: "kit-a", Designated: true})
	writeRecord(identityPath, privateDesignationRecord{Schema: 1, TargetID: "kit-a"})
	if err := os.Chmod(designationPath, 0o600|os.ModeSetuid); err != nil {
		t.Fatal(err)
	}
	if err := NewDesignation(designationPath, identityPath).Verify(context.Background()); !errors.Is(err, ErrDesignation) {
		t.Fatalf("setuid designation error = %v, want ErrDesignation", err)
	}
}
