//go:build linux

package kitsoftware

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Build cmd/fogcast-api natively as fogcast-kit and set FES_CATALOG_CLI to
// its absolute path to exercise the production subprocess protocol. The small
// temporary driver uses the real FogCast catalog/media APIs without adding a
// SQLite or full FogCast dependency to the platform module.
func TestCatalogCLIIntegration(t *testing.T) {
	helper := os.Getenv("FES_CATALOG_CLI")
	if helper == "" {
		t.Skip("set FES_CATALOG_CLI to native fogcast-kit built from this checkout")
	}
	if !filepath.IsAbs(helper) {
		t.Fatal("FES_CATALOG_CLI must be absolute")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	base := t.TempDir()
	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fogcast := filepath.Clean(filepath.Join(working, "..", "..", "..", "sources", "FogCast"))
	fixtureSource := filepath.Join(base, "catalog-fixture.go")
	if err := os.WriteFile(fixtureSource, []byte(catalogCLIFixtureSource), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureBinary := filepath.Join(base, "catalog-fixture")
	build := exec.CommandContext(ctx, "go", "build", "-o", fixtureBinary, fixtureSource)
	build.Dir = fogcast
	build.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=auto")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real catalog fixture: %v\n%s", err, output)
	}
	fixture := func(action, database string) map[string]any {
		t.Helper()
		output, err := exec.CommandContext(ctx, fixtureBinary, action, database).CombinedOutput()
		if err != nil {
			t.Fatalf("real catalog %s: %v\n%s", action, err, output)
		}
		var result map[string]any
		if err := json.Unmarshal(output, &result); err != nil {
			t.Fatalf("real catalog result %q: %v", output, err)
		}
		return result
	}
	root := filepath.Join(base, "helper-release")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(helper, filepath.Join(root, "bin", "fogcast-kit")); err != nil {
		t.Fatal(err)
	}
	helperID := strings.Repeat("e", 64)
	var resolutions int
	repository, err := OpenRepository(filepath.Join(base, "control"), testFactoryID)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	manager, err := OpenCatalogTransitions(repository, filepath.Join(base, "fogcast-host"), helperID, func(_ context.Context, id string) (string, error) {
		// Authentication is the production resolver's responsibility. This
		// fixture restricts identities and runs the actual subprocess protocol.
		if id != helperID {
			return "", errors.New("unexpected helper identity")
		}
		resolutions++
		return root, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if schema, err := manager.supportedSchema(ctx, helperID); err != nil || schema != 22 {
		t.Fatalf("real helper schema: %d %v", schema, err)
	}
	original := fixture("seed21", manager.database)
	if original["schema"] != float64(21) || original["roms"] != float64(1) || len(original["titles"].([]any)) != 1 {
		t.Fatalf("real schema21 fixture: %+v", original)
	}
	settings := filepath.Join(manager.hostRoot, "state", "library-settings.json")
	if err := os.WriteFile(settings, []byte("retained settings bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	operation := func(id, candidate string) Operation {
		t.Helper()
		state, err := repository.Read()
		if err != nil {
			t.Fatal(err)
		}
		if err := Queue(repository, Request{ID: strings.Repeat(id, 64), Candidate: candidate, Requester: "real-catalog-cli-test", Generation: state.Generation}); err != nil {
			t.Fatal(err)
		}
		state, err = repository.Read()
		if err != nil {
			t.Fatal(err)
		}
		op := *state.Pending
		if err := transition(repository, op, "queued", "applying", ""); err != nil {
			t.Fatal(err)
		}
		return op
	}
	finish := func(op Operation, phase, result string) {
		t.Helper()
		if err := complete(repository, op, phase, result, ""); err != nil {
			t.Fatal(err)
		}
		if err := repository.Update(func(state *State) error { state.Unreleased = nil; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	upgrade := operation("1", testCandidateID)
	if err := manager.Apply(ctx, upgrade, upgrade.Previous); err != nil {
		t.Fatal(err)
	}
	newer := fixture("upgrade22", manager.database)
	if newer["schema"] != float64(22) || newer["remote"] != float64(1) || len(newer["titles"].([]any)) != 2 || len(newer["media"].(map[string]any)) != len(original["media"].(map[string]any))+1 {
		t.Fatalf("real migration/new embedded media: %+v", newer)
	}
	finish(upgrade, "applying", "accepted")
	rollback := operation("2", testFactoryID)
	if err := manager.Apply(ctx, rollback, rollback.Previous); err != nil {
		t.Fatal(err)
	}
	if got := fixture("inspect", manager.database); !reflect.DeepEqual(got, original) {
		t.Fatalf("known baseline lost original title/ROM/media: got=%+v want=%+v", got, original)
	}
	retained := filepath.Join(manager.opRoot(rollback.ID), "rollback-retain", "catalog.sqlite3")
	if got := fixture("inspect", retained); !reflect.DeepEqual(got, newer) {
		t.Fatalf("newer title/media not retained: got=%+v want=%+v", got, newer)
	}
	// Model the old bundle failing startup after its catalog was restored.
	// Recovery must restore this return operation's newer checkpoint.
	if err := transition(repository, rollback, "applying", "recovering", "old baseline startup failed"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Recover(ctx, rollback, rollback.Previous); err != nil {
		t.Fatal(err)
	}
	if got := fixture("inspect", manager.database); !reflect.DeepEqual(got, newer) {
		t.Fatalf("failed-return recovery used wrong catalog: got=%+v want=%+v", got, newer)
	}
	if err := manager.Recover(ctx, rollback, rollback.Previous); err != nil {
		t.Fatalf("real helper recovery replay: %v", err)
	}
	finish(rollback, "recovering", "recovered")
	final := operation("3", testFactoryID)
	if err := manager.Apply(ctx, final, final.Previous); err != nil {
		t.Fatal(err)
	}
	if got := fixture("inspect", manager.database); !reflect.DeepEqual(got, original) {
		t.Fatalf("final baseline restoration: got=%+v want=%+v", got, original)
	}
	finish(final, "applying", "accepted")
	if data, err := os.ReadFile(settings); err != nil || string(data) != "retained settings bytes" {
		t.Fatalf("catalog restoration changed settings: %q %v", data, err)
	}
	t.Logf("real helper schema22; actual catalog21→22; baseline21 retains newer embedded media; failed-return recovers22; final baseline21; %d helper invocations", resolutions)
}

const catalogCLIFixtureSource = `package main
import (
 "bytes"
 "context"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "os"
 "strings"
 "github.com/DeanoC/FogCast/catalog"
 _ "modernc.org/sqlite"
)
func must(err error) { if err != nil { panic(err) } }
func main() {
 action, path := os.Args[1], os.Args[2]
 ctx := context.Background()
 if action == "seed21" || action == "upgrade22" {
  store, err := catalog.Open(path); must(err)
  pkg := strings.Repeat("a",64)
  if action == "seed21" {
   entry, err := store.CreateCoreEntry(ctx,"Original title","fes.zx81",pkg); must(err)
   rom,_,err := store.ImportCoreMedia(ctx,bytes.Repeat([]byte{0x5a},8192)); must(err)
   _,err = store.SelectCoreEntryROM(ctx,entry.GameID,pkg,"machine-rom",rom.Size,"",rom.MediaID); must(err)
   media,_,err := store.ImportCoreMedia(ctx,[]byte("original cassette bytes")); must(err)
   _,err = store.SelectCoreEntryMedia(ctx,entry.GameID,pkg,"","cassette",media.MediaID); must(err)
  } else {
   media,_,err := store.ImportCoreMedia(ctx,[]byte("new remotely cached cassette bytes")); must(err)
   _,err = store.CreateRemoteCoreEntry(ctx,"New remote title","fes.zx81",pkg,catalog.RemoteCoreEntryOrigin{SourceID:"remote-a",SourceGameID:"source-game",MediaID:media.MediaID}); must(err)
  }
  must(store.Close())
 }
 db,err := sql.Open("sqlite",path); must(err); defer db.Close()
 if action == "seed21" { _,err = db.Exec("DROP TRIGGER remote_core_entry_selected; DROP TABLE remote_core_entries; PRAGMA user_version=21"); must(err) }
 var schema,roms,remote int
 must(db.QueryRow("PRAGMA user_version").Scan(&schema))
 var titles []string
 rows,err := db.Query("SELECT games.title FROM core_entries JOIN games USING(game_id) ORDER BY games.title"); must(err)
 for rows.Next() { var title string; must(rows.Scan(&title)); titles=append(titles,title) }; must(rows.Err()); must(rows.Close())
 must(db.QueryRow("SELECT count(*) FROM core_entry_roms").Scan(&roms))
 if schema >= 22 { must(db.QueryRow("SELECT count(*) FROM remote_core_entries").Scan(&remote)) }
 media:=map[string]string{}
 rows,err=db.Query("SELECT media_id,data FROM core_media"); must(err)
 for rows.Next() {
  var id string; var data []byte; must(rows.Scan(&id,&data))
  chunks,err:=db.Query("SELECT data FROM core_media_chunks WHERE media_id=? ORDER BY chunk_index",id); must(err)
  for chunks.Next() { var chunk []byte; must(chunks.Scan(&chunk)); data=append(data,chunk...) }; must(chunks.Err()); must(chunks.Close())
  sum:=sha256.Sum256(data); digest:=hex.EncodeToString(sum[:]); if digest != id { panic(fmt.Sprintf("embedded media mismatch %s",id)) }; media[id]=digest
 }; must(rows.Err()); must(rows.Close())
 must(json.NewEncoder(os.Stdout).Encode(map[string]any{"schema":schema,"titles":titles,"roms":roms,"remote":remote,"media":media}))
}
`
