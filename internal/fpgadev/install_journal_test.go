//go:build fpgadev

package fpgadev

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/hardwareowner"
)

func TestInstallJournalCanonicalTerminalRoundTripAndMaintenanceGate(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uint32(os.Getuid()))
	record := testTerminalInstallJournal()
	if err := replaceInstallJournalForTest(journal, record); err != nil {
		t.Fatal(err)
	}
	loaded, exists, err := journal.Load()
	if err != nil || !exists || loaded.State != InstallStateTerminal {
		t.Fatalf("Load() = %#v exists=%v err=%v", loaded, exists, err)
	}
	status, unlock, err := NewMaintenanceGate(journal, NewInstallLocker(filepath.Join(root, "install.lock"), uint32(os.Getuid()))).Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if unlock == nil || status.TerminalJournalSHA256 == "" || status.Inventory.MainExecutable.Path == "" {
		t.Fatalf("status = %#v unlock=%v", status, unlock)
	}
	if !status.Inventory.Equal(record.Inventory) {
		t.Fatalf("status inventory changed terminal inventory: %#v != %#v", status.Inventory, record.Inventory)
	}
	if err := unlock.Unlock(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallJournalRejectsReorderedUnknownDuplicateAndOversizedRecords(t *testing.T) {
	record := testTerminalInstallJournal()
	raw, err := record.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	bad := []string{
		strings.Replace(string(raw), `"schema":1,`, `"state":"terminal","schema":1,`, 1),
		strings.Replace(string(raw), `"sources":[`, `"unexpected":true,"sources":[`, 1),
		strings.Replace(string(raw), `"schema":1,`, `"schema":1,"schema":1,`, 1),
	}
	for _, input := range bad {
		if _, err := ParseInstallJournal([]byte(input)); err == nil {
			t.Fatalf("hostile journal accepted: %s", input)
		}
	}
	if ProtectedRegularMaxBytes <= InstallJournalMaxBytes {
		t.Fatalf("protected regular bound=%d must exceed journal bound=%d", ProtectedRegularMaxBytes, InstallJournalMaxBytes)
	}
	if _, err := ParseInstallJournal([]byte(strings.Repeat("x", InstallJournalMaxBytes+1))); err == nil {
		t.Fatal("oversized journal accepted")
	}
}

func TestProtectedRegularBoundCoversStagedMisterAgent(t *testing.T) {
	const stagedMisterAgentBytes = 7471266
	if ProtectedRegularMaxBytes != 8<<20 {
		t.Fatalf("protected regular bound=%d want=%d", ProtectedRegularMaxBytes, 8<<20)
	}
	if stagedMisterAgentBytes >= ProtectedRegularMaxBytes {
		t.Fatalf("staged mister-agent bytes=%d must be below protected regular bound=%d", stagedMisterAgentBytes, ProtectedRegularMaxBytes)
	}
}

func TestInstallJournalStoreRejectsOversizedFileAtJournalCap(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewInstallJournal(filepath.Join(root, "journal.json"), uint32(os.Getuid()))
	if err := os.Mkdir(store.BackupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, []byte(strings.Repeat("x", InstallJournalMaxBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := store.Load(); err == nil || !exists {
		t.Fatalf("oversized journal file load exists=%v err=%v", exists, err)
	}
}

func TestPreJournalRecoveryAuthorityCanonicalRoundTrip(t *testing.T) {
	authority := PreJournalRecoveryAuthority{
		Schema:                 1,
		StagePath:              "/var/lib/fogcast/fpgadev-staging/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		StageManifestSHA256:    strings.Repeat("a", 64),
		DispatcherPath:         "/media/fat/linux/fogcast-dev-start.sh",
		DispatcherBackupPath:   "/var/lib/fogcast/fpgadev-install-v1-backups/00-fogcast-dev-start.sh",
		DispatcherBackupSHA256: strings.Repeat("b", 64),
		DispatcherMode:         0o755,
		RecoveryHelperSHA256:   strings.Repeat("c", 64),
	}
	raw, err := authority.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":1,"stage_path":"/var/lib/fogcast/fpgadev-staging/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","stage_manifest_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","dispatcher_path":"/media/fat/linux/fogcast-dev-start.sh","dispatcher_backup_path":"/var/lib/fogcast/fpgadev-install-v1-backups/00-fogcast-dev-start.sh","dispatcher_backup_sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","dispatcher_mode":493,"recovery_helper_sha256":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}` + "\n"
	if string(raw) != want {
		t.Fatalf("authority bytes=%q want=%q", raw, want)
	}
	parsed, err := ParsePreJournalRecoveryAuthority(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != authority {
		t.Fatalf("parsed authority=%#v want=%#v", parsed, authority)
	}
	bad := [][]byte{
		[]byte(strings.Replace(string(raw), `"schema":1,`, `"stage_path":"/other","schema":1,`, 1)),
		[]byte(strings.Replace(string(raw), `"schema":1,`, `"schema":1,"extra":true,`, 1)),
		[]byte(strings.Replace(string(raw), `"dispatcher_mode":493`, `"dispatcher_mode":1024`, 1)),
	}
	for _, input := range bad {
		if _, err := ParsePreJournalRecoveryAuthority(input); err == nil {
			t.Fatalf("malformed pre-journal authority accepted: %q", input)
		}
	}
}

func TestInstallJournalNonterminalBlocksMaintenanceAndAbsentRecordDoesNotAdopt(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := NewInstallJournal(filepath.Join(root, "journal.json"), uint32(os.Getuid()))
	lock := NewInstallLocker(filepath.Join(root, "install.lock"), uint32(os.Getuid()))
	if _, _, err := NewMaintenanceGate(journal, lock).Enter(context.Background()); err == nil {
		t.Fatal("absent journal admitted maintenance")
	}
	record := testTerminalInstallJournal()
	record.State = InstallStatePrepared
	if err := journal.Replace(record); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewMaintenanceGate(journal, lock).Enter(context.Background()); err == nil {
		t.Fatal("nonterminal journal admitted maintenance")
	}
}

func TestInstallJournalTerminalExceptionsKeepImmutablePayload(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	uid := uint32(os.Getuid())
	store := NewInstallJournal(filepath.Join(root, "journal.json"), uid)
	record := testTerminalInstallJournal()
	if err := replaceInstallJournalForTest(store, record); err != nil {
		t.Fatal(err)
	}

	changed := record
	changed.State = InstallStateUninstalling
	changed.PackageSHA256 = strings.Repeat("0", 64)
	if err := store.Replace(changed); err == nil {
		t.Fatal("terminal -> uninstalling accepted a changed package binding")
	}
	if err := store.Replace(func() InstallJournalRecord {
		valid := record
		valid.State = InstallStateUninstalling
		return valid
	}()); err != nil {
		t.Fatal(err)
	}
	restored := record
	restored.State = InstallStateRestored
	restored.Inventory.MainExecutable.SHA256 = strings.Repeat("1", 64)
	if err := store.Replace(restored); err == nil {
		t.Fatal("uninstalling -> restored accepted a changed inventory")
	}
}

func TestFailStopJournalAcceptsOnlyCoarseInstallStates(t *testing.T) {
	record := testTerminalInstallJournal()
	raw, err := record.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	for _, legacy := range []string{"trampoline_installed", "owner_initialized", "sources_disabled", "supervisor_installed"} {
		input := strings.Replace(string(raw), `"terminal"`, `"`+legacy+`"`, 1)
		if _, err := ParseInstallJournal([]byte(input)); err == nil {
			t.Fatalf("legacy journal state %q was accepted", legacy)
		}
	}
}

func TestFailStopJournalAllowsInstalledAndCoarseTransitions(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store := NewInstallJournal(filepath.Join(root, "journal.json"), uint32(os.Getuid()))
	record := testTerminalInstallJournal()
	record.State = InstallStatePrepared
	if err := store.Replace(record); err != nil {
		t.Fatal(err)
	}
	record.State = InstallState("installed")
	if err := store.Replace(record); err != nil {
		t.Fatalf("prepared -> installed: %v", err)
	}
	record.State = InstallStateTerminal
	if err := store.Replace(record); err != nil {
		t.Fatalf("installed -> terminal: %v", err)
	}
	record.State = InstallStateUninstalling
	if err := store.Replace(record); err != nil {
		t.Fatalf("terminal -> uninstalling: %v", err)
	}
	record.State = InstallStateRestored
	if err := store.Replace(record); err != nil {
		t.Fatalf("uninstalling -> restored: %v", err)
	}
}

func TestFailStopJournalRejectsNonPreparedInitialState(t *testing.T) {
	for _, state := range []InstallState{InstallStateInstalled, InstallStateTerminal, InstallStateUninstalling, InstallStateRestored} {
		t.Run(string(state), func(t *testing.T) {
			root := t.TempDir()
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			store := NewInstallJournal(filepath.Join(root, "journal.json"), uint32(os.Getuid()))
			record := testTerminalInstallJournal()
			record.State = state
			if err := store.Replace(record); err == nil {
				t.Fatalf("initial %s journal was accepted", state)
			}
			if _, exists, err := store.Load(); err != nil || exists {
				t.Fatalf("rejected initial %s left journal: exists=%v err=%v", state, exists, err)
			}
		})
	}
}

func testTerminalInstallJournal() InstallJournalRecord {
	source := SourceRecord{
		Path: "/etc/init.d/S01fogcast", Kind: "regular", Mode: 0o755,
		SHA256: strings.Repeat("d", 64), BackupPath: "/var/lib/fogcast/fpgadev-install-v1-backups/S01fogcast",
		BackupSHA256: strings.Repeat("e", 64), DisabledState: "approved_trampoline",
		DisabledSHA256: strings.Repeat("f", 64),
	}
	return InstallJournalRecord{
		Schema: 1, State: InstallStateTerminal,
		InstallBootID: "00000000-0000-0000-0000-000000000001",
		PackageSHA256: strings.Repeat("a", 64), PreviousConfigSHA256: strings.Repeat("b", 64),
		Inventory: InventoryV1{
			Schema:         1,
			MainExecutable: PathExpectation{Path: "/usr/bin/Main_MiSTer", Kind: "regular", SHA256: strings.Repeat("c", 64)},
			MainFIFO:       PathExpectation{Path: "/dev/MiSTer_cmd", Kind: "fifo"},
			StartSources:   []SourceRecord{source},
		},
		Sources: []SourceRecord{source},
	}
}

func replaceInstallJournalForTest(store *InstallJournalStore, record InstallJournalRecord) error {
	target := record.State
	record.State = InstallStatePrepared
	if err := store.Replace(record); err != nil {
		return err
	}
	if target == InstallStatePrepared {
		return nil
	}
	for _, state := range []InstallState{InstallStateInstalled, InstallStateTerminal, InstallStateUninstalling, InstallStateRestored} {
		record.State = state
		if err := store.Replace(record); err != nil {
			return err
		}
		if state == target {
			return nil
		}
	}
	return errors.New("unsupported test journal state")
}

var _ hardwareowner.NormalGate = (*hardwareowner.Gate)(nil)
