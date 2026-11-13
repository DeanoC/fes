package fogcast

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/libraryuser"
	"github.com/DeanoC/FogCast/protocol"
)

func failUserCleanup(t *testing.T, path string) func() {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TRIGGER fail_game_cleanup BEFORE DELETE ON game_state BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if _, err := db.Exec(`DROP TRIGGER fail_game_cleanup`); err != nil {
			t.Fatal(err)
		}
	}
}

func removeEntryFixture(t *testing.T) (*Service, *catalog.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	store, err := catalog.Open(filepath.Join(root, "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	userPath := filepath.Join(root, "user.sqlite")
	users, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	t.Cleanup(func() { _ = users.Close() })
	entry, err := store.CreateCoreEntry(context.Background(), "ZX81", "fes.zx81", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return &Service{catalog: store, users: users, plays: map[string]targetPlay{}}, store, userPath, entry.GameID
}

func TestRemoveCoreEntryRejectsAnyActiveKitAndCleansReferences(t *testing.T) {
	ctx := context.Background()
	s, store, _, id := removeEntryFixture(t)
	if err := s.SetFavorite(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordPlay(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.users.UpsertCollection(ctx, "mine", "Mine"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCollectionMember(ctx, "mine", id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetEditionPreference(ctx, "test", "fpga", id); err != nil {
		t.Fatal(err)
	}
	s.plays["other-kit"] = targetPlay{execution: ExecutionFPGANative, gameID: id}
	var api *protocol.APIError
	if err := s.RemoveCoreEntry(ctx, id); !errors.As(err, &api) || api.Code != protocol.CodeBusy {
		t.Fatalf("active removal = %v", err)
	}
	if _, err := store.CoreEntry(ctx, id); err != nil {
		t.Fatalf("busy removed entry: %v", err)
	}
	delete(s.plays, "other-kit")
	if err := s.RemoveCoreEntry(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCoreEntry(ctx, id); err != nil {
		t.Fatalf("retry: %v", err)
	}
	state, err := s.users.State(ctx, id)
	if err != nil || state.Favorite || state.PlayCount != 0 {
		t.Fatalf("user state %+v %v", state, err)
	}
	ids, err := s.users.CollectionGameIDs(ctx, "mine")
	if err != nil || len(ids) != 0 {
		t.Fatalf("collection %v %v", ids, err)
	}
	if _, err := s.users.EditionPreference(ctx, "test", "fpga"); err == nil {
		t.Fatal("edition preference survived")
	}
	if err := s.RecordPlay(ctx, id); err != nil {
		t.Fatal(err)
	}
	state, _ = s.users.State(ctx, id)
	if state.PlayCount != 0 {
		t.Fatal("late play recreated deleted history")
	}
	if _, err := s.SetEditionPreference(ctx, "test", "fpga", id); err == nil {
		t.Fatal("retained title accepted as edition")
	}
}

func TestRemoveCoreEntryCleanupFailureCanBeRetried(t *testing.T) {
	ctx := context.Background()
	s, store, userPath, id := removeEntryFixture(t)
	if err := s.users.SetFavorite(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	clearFailure := failUserCleanup(t, userPath)
	if err := s.RemoveCoreEntry(ctx, id); err == nil {
		t.Fatal("cleanup failure returned success")
	}
	if _, err := store.CoreEntry(ctx, id); !errors.Is(err, catalog.ErrCoreEntryNotFound) {
		t.Fatalf("entry still visible: %v", err)
	}
	clearFailure()
	reopened, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s.users = reopened
	state, err := s.users.State(ctx, id)
	if err != nil || !state.Favorite {
		t.Fatalf("expected retained state before retry: %+v %v", state, err)
	}
	if err := s.RemoveCoreEntry(ctx, id); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	state, err = s.users.State(ctx, id)
	if err != nil || state.Favorite {
		t.Fatalf("retry left favorite: %+v %v", state, err)
	}
}

func TestRecreatingRetainedEntryClearsInterruptedUserCleanup(t *testing.T) {
	ctx := context.Background()
	s, _, entry, inspection := newCoreEntryLaunchFixture(t, libraryPackageFixture(t, "0.1.0"), "Retained Pong")
	userPath := filepath.Join(t.TempDir(), "user.sqlite")
	users, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	s.users = users
	if err := users.SetFavorite(ctx, entry.GameID, true); err != nil {
		t.Fatal(err)
	}
	clearFailure := failUserCleanup(t, userPath)
	if err := s.RemoveCoreEntry(ctx, entry.GameID); err == nil {
		t.Fatal("expected cleanup failure")
	}
	clearFailure()
	reopened, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s.users = reopened
	recreated, err := s.CreateCoreEntry(ctx, "Retained Pong", inspection.PackageID)
	if err != nil || recreated.GameID != entry.GameID {
		t.Fatalf("recreate %+v %v", recreated, err)
	}
	state, err := reopened.State(ctx, entry.GameID)
	if err != nil || state.Favorite {
		t.Fatalf("stale favorite after recreate: %+v %v", state, err)
	}
}

func TestCreateCoreEntryWithTruncatedSlugSeparator(t *testing.T) {
	ctx := context.Background()
	s, _, _, inspection := newCoreEntryLaunchFixture(t, libraryPackageFixture(t, "0.1.0"), "Existing Pong")
	users, err := libraryuser.Open(filepath.Join(t.TempDir(), "user.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	s.users = users

	const title = "SG-1000 controller diagnostic codex-sg1000-1755-dc0aca159c6e-1872748"
	entry, err := s.CreateCoreEntry(ctx, title, inspection.PackageID)
	if err != nil {
		t.Fatalf("create entry with truncated slug: %v", err)
	}
	if err := protocol.ValidateGameID(entry.GameID); err != nil {
		t.Fatalf("created invalid game ID %q: %v", entry.GameID, err)
	}
	if entry.Title != title || entry.PackageID != inspection.PackageID {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	got, err := s.catalog.(*catalog.Store).CoreEntry(ctx, entry.GameID)
	if err != nil || got != entry {
		t.Fatalf("stored entry = %+v, %v; want %+v", got, err, entry)
	}
}

func TestRemoveCoreEntrySerializesWithUserWrites(t *testing.T) {
	ctx := context.Background()
	s, store, userPath, id := removeEntryFixture(t)
	secondUsers, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	defer secondUsers.Close()
	// A separately opened Service models CLI writes against the same files.
	other := &Service{catalog: store, users: secondUsers}
	if _, err := s.users.UpsertCollection(ctx, "mine", "Mine"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, write := range []func(){
		func() { _ = other.SetFavorite(ctx, id, true) },
		func() { _ = other.RecordPlay(ctx, id) },
		func() { _ = other.SetCollectionMember(ctx, "mine", id, true) },
		func() { _, _ = other.SetEditionPreference(ctx, "test", "fpga", id) },
	} {
		wg.Add(1)
		go func(write func()) {
			defer wg.Done()
			<-start
			for i := 0; i < 20; i++ {
				write()
			}
		}(write)
	}
	close(start)
	if err := s.RemoveCoreEntry(ctx, id); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	state, err := s.users.State(ctx, id)
	if err != nil || state.Favorite || state.PlayCount != 0 {
		t.Fatalf("state after racing deletion %+v %v", state, err)
	}
	ids, err := s.users.CollectionGameIDs(ctx, "mine")
	if err != nil || len(ids) != 0 {
		t.Fatalf("membership after racing deletion %v %v", ids, err)
	}
	if _, err := s.users.EditionPreference(ctx, "test", "fpga"); err == nil {
		t.Fatal("edition recreated after deletion")
	}
}

func TestRemoveCoreEntryWaitsForLaunchAdmission(t *testing.T) {
	ctx := context.Background()
	s, store, _, id := removeEntryFixture(t)
	release, err := s.acquireLifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.RemoveCoreEntry(ctx, id) }()
	s.executionMu.Lock()
	s.plays["other-kit"] = targetPlay{execution: ExecutionFPGANative, gameID: id}
	s.executionMu.Unlock()
	release()
	var api *protocol.APIError
	if err := <-done; !errors.As(err, &api) || api.Code != protocol.CodeBusy {
		t.Fatalf("removal during launch = %v", err)
	}
	if _, err := store.CoreEntry(ctx, id); err != nil {
		t.Fatalf("active title removed: %v", err)
	}
}

func TestRemoteRefreshAfterInterruptedRemovalClearsReferencesAndRetainsSave(t *testing.T) {
	ctx := context.Background()
	f := newRemoteSourceFixture(t, []byte("source tape"), nil)
	userPath := filepath.Join(t.TempDir(), "users.sqlite")
	users, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	f.service.users = users
	if err := users.SetFavorite(ctx, f.entry.GameID, true); err != nil {
		t.Fatal(err)
	}
	saved, _, err := f.store.ImportCoreMedia(ctx, []byte{0, 10, 0x76})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.InsertZX81Save(ctx, f.entry.GameID, saved.MediaID, "Saved program"); err != nil {
		t.Fatal(err)
	}
	clearFailure := failUserCleanup(t, userPath)
	if err := f.service.RemoveCoreEntry(ctx, f.entry.GameID); err == nil {
		t.Fatal("expected user cleanup failure")
	}
	clearFailure()
	reopened, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	f.service.users = reopened
	refresh, err := f.service.RefreshLibrarySource(ctx, "home")
	if err != nil || len(refresh.Entries) != 1 || refresh.Entries[0].GameID != f.entry.GameID {
		t.Fatalf("refresh %+v %v", refresh, err)
	}
	state, err := reopened.State(ctx, f.entry.GameID)
	if err != nil || state.Favorite {
		t.Fatalf("stale favorite %+v %v", state, err)
	}
	saves, err := f.store.ListZX81Saves(ctx, f.entry.GameID)
	if err != nil || len(saves) != 1 || saves[0].Name != "Saved program" || saves[0].MediaID != saved.MediaID {
		t.Fatalf("saved tape %+v %v", saves, err)
	}
}

func TestSetupRecreationClearsInterruptedRemovalReferences(t *testing.T) {
	ctx := context.Background()
	s, pkg := catalogServiceFixture(t)
	userPath := filepath.Join(t.TempDir(), "user.sqlite")
	users, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	s.users = users
	req := CoreSetupRequest{LibrarySourceID: "test-library", SourceID: "fes-first-party", CoreID: "fes.pong", PackageID: pkg, Title: "Retained setup"}
	first, err := s.CreateCoreSetupEntry(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.SetFavorite(ctx, first.GameID, true); err != nil {
		t.Fatal(err)
	}
	clearFailure := failUserCleanup(t, userPath)
	if err := s.RemoveCoreEntry(ctx, first.GameID); err == nil {
		t.Fatal("expected cleanup failure")
	}
	clearFailure()
	reopened, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	s.users = reopened
	second, err := s.CreateCoreSetupEntry(ctx, req)
	if err != nil || second.GameID != first.GameID {
		t.Fatalf("setup recreate %+v %v", second, err)
	}
	state, err := reopened.State(ctx, first.GameID)
	if err != nil || state.Favorite {
		t.Fatalf("stale favorite after setup recreate: %+v %v", state, err)
	}
}

func TestRemoveCoreEntrySerializesIndependentServiceDatabaseWriters(t *testing.T) {
	ctx := context.Background()
	s, store, userPath, id := removeEntryFixture(t)
	secondUsers, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	defer secondUsers.Close()
	other := &Service{catalog: store, users: secondUsers}
	entered := make(chan struct{})
	resume := make(chan struct{})
	removed := make(chan error, 1)
	go func() {
		removed <- s.users.MutateGame(ctx, func(m *libraryuser.GameMutation) error {
			if err := store.RemoveCoreEntry(ctx, id); err != nil {
				return err
			}
			close(entered)
			<-resume
			return m.RemoveGameReferences(ctx, id)
		})
	}()
	<-entered
	favorite := make(chan error, 1)
	go func() { favorite <- other.SetFavorite(ctx, id, true) }()
	close(resume)
	if err := <-removed; err != nil {
		t.Fatal(err)
	}
	if err := <-favorite; err == nil {
		t.Fatal("independent service favorited a removed title")
	}
	state, err := s.users.State(ctx, id)
	if err != nil || state.Favorite {
		t.Fatalf("favorite survived removal: %+v %v", state, err)
	}
}

func TestRecreateLegacyEntryAfterCleanupFailurePreservesItsIdentityAndSave(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, _, entry, inspection := newCoreEntryLaunchFixtureAt(t, root, libraryPackageFixture(t, "0.1.0"), "Legacy Pong")
	legacyID := "fpga-legacy-pong"
	db, err := sql.Open("sqlite", filepath.Join(root, "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `UPDATE core_entries SET game_id=? WHERE game_id=?`, legacyID, entry.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `UPDATE games SET game_id=?, relative_path=?, group_key=? WHERE game_id=?`, legacyID, "fes.pong", legacyID, entry.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `COMMIT`); err != nil {
		t.Fatal(err)
	}
	store := s.catalog.(*catalog.Store)
	media, _, err := store.ImportCoreMedia(ctx, []byte{0, 10, 0x76})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertZX81Save(ctx, legacyID, media.MediaID, "Legacy save"); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(root, "users.sqlite")
	users, err := libraryuser.Open(userPath)
	if err != nil {
		t.Fatal(err)
	}
	defer users.Close()
	s.users = users
	if err := users.SetFavorite(ctx, legacyID, true); err != nil {
		t.Fatal(err)
	}
	clearFailure := failUserCleanup(t, userPath)
	if err := s.RemoveCoreEntry(ctx, legacyID); err == nil {
		t.Fatal("cleanup failure returned success")
	}
	clearFailure()
	recreated, err := s.CreateCoreEntry(ctx, "Legacy Pong", inspection.PackageID)
	if err != nil || recreated.GameID != legacyID {
		t.Fatalf("recreate %+v %v", recreated, err)
	}
	state, err := users.State(ctx, legacyID)
	if err != nil || state.Favorite {
		t.Fatalf("stale reference %+v %v", state, err)
	}
	saves, err := store.ListZX81Saves(ctx, legacyID)
	if err != nil || len(saves) != 1 || saves[0].Name != "Legacy save" || saves[0].MediaID != media.MediaID {
		t.Fatalf("legacy saved tapes %+v %v", saves, err)
	}
}

func TestRemoveCoreEntryReconcilesTargetsAfterRestart(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     protocol.Status
		statusErr  error
		targetGame bool
		want       protocol.ErrorCode
	}{
		{name: "active title after restart", status: protocol.Status{State: protocol.StateActive}, targetGame: true, want: protocol.CodeBusy},
		{name: "unknown active identity", status: protocol.Status{State: protocol.StateActive}, want: protocol.CodeBusy},
		{name: "stopping", status: protocol.Status{State: protocol.StateStopping}, want: protocol.CodeBusy},
		{name: "recovery", status: protocol.Status{State: protocol.StateIdle, Recovery: protocol.RecoveryRebootRequired}, want: protocol.CodeBusy},
		{name: "idle retained system", status: protocol.Status{State: protocol.StateIdle, System: systemPtr(protocol.System("coleco"))}, want: protocol.CodeBusy},
		{name: "idle retained expected core", status: protocol.Status{State: protocol.StateIdle, ExpectedCore: stringPtr("retained-core")}, want: protocol.CodeBusy},
		{name: "idle retained observed core", status: protocol.Status{State: protocol.StateIdle, ObservedCore: stringPtr("retained-core")}, want: protocol.CodeBusy},
		{name: "idle retained package", status: protocol.Status{State: protocol.StateIdle, CorePackage: &protocol.CorePackageStatus{}}, want: protocol.CodeBusy},
		{name: "offline", statusErr: errors.New("offline"), want: protocol.CodeMiSTerUnavailable},
		{name: "different active title", status: protocol.Status{State: protocol.StateActive, GameID: stringPtr("other-game")}},
		{name: "idle", status: protocol.Status{State: protocol.StateIdle}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, store, _, id := removeEntryFixture(t)
			if tc.targetGame {
				tc.status.GameID = stringPtr(id)
			}
			client := &fakeServiceClient{statusResult: tc.status, statusErr: tc.statusErr}
			s.targets = []TargetConfig{{Name: "kit", Enabled: true}}
			s.targetClients = map[string]serviceClient{"kit": client}
			s.requestTimeout = time.Second
			err := s.RemoveCoreEntry(ctx, id)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("removal: %v", err)
				}
				if _, err := store.CoreEntry(ctx, id); !errors.Is(err, catalog.ErrCoreEntryNotFound) {
					t.Fatalf("entry remains: %v", err)
				}
			} else {
				var api *protocol.APIError
				if !errors.As(err, &api) || api.Code != tc.want {
					t.Fatalf("removal error=%v want %v", err, tc.want)
				}
				if _, err := store.CoreEntry(ctx, id); err != nil {
					t.Fatalf("rejected removal changed entry: %v", err)
				}
			}
			if client.statusCalls != 1 {
				t.Fatalf("status calls=%d", client.statusCalls)
			}
		})
	}
}

func TestRemoveCoreEntryChecksEveryEnabledTarget(t *testing.T) {
	ctx := context.Background()
	s, store, _, id := removeEntryFixture(t)
	first := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateIdle}}
	second := &fakeServiceClient{statusResult: protocol.Status{State: protocol.StateActive, GameID: stringPtr(id)}}
	s.targets = []TargetConfig{{Name: "kit-a", Enabled: true}, {Name: "kit-b", Enabled: true}, {Name: "disabled", Enabled: false}}
	s.targetClients = map[string]serviceClient{"kit-a": first, "kit-b": second}
	s.requestTimeout = time.Second
	var api *protocol.APIError
	if err := s.RemoveCoreEntry(ctx, id); !errors.As(err, &api) || api.Code != protocol.CodeBusy {
		t.Fatalf("removal = %v", err)
	}
	if first.statusCalls != 1 || second.statusCalls != 1 {
		t.Fatalf("status calls %d, %d", first.statusCalls, second.statusCalls)
	}
	if _, err := store.CoreEntry(ctx, id); err != nil {
		t.Fatalf("entry changed: %v", err)
	}
}

func TestRemoveCoreEntryTargetStatusIsBounded(t *testing.T) {
	s, store, _, id := removeEntryFixture(t)
	client := &fakeServiceClient{statusFn: func(ctx context.Context) (protocol.Status, error) { <-ctx.Done(); return protocol.Status{}, ctx.Err() }}
	s.targets = []TargetConfig{{Name: "kit", Enabled: true}}
	s.targetClients = map[string]serviceClient{"kit": client}
	s.requestTimeout = 20 * time.Millisecond
	var api *protocol.APIError
	if err := s.RemoveCoreEntry(context.Background(), id); !errors.As(err, &api) || api.Code != protocol.CodeMiSTerUnavailable {
		t.Fatalf("removal=%v", err)
	}
	if _, err := store.CoreEntry(context.Background(), id); err != nil {
		t.Fatalf("entry changed: %v", err)
	}
}
