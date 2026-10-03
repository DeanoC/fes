package kitlauncher

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/hostclient"
	"github.com/DeanoC/FogCast/internal/localcores"
)

func TestLocalROMMatcherHashesZIPMember(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cart.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(f)
	member, err := writer.Create("ROM/cart.sms")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("cart inside zip")
	if _, err := member.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := (&localROMMatcher{}).hash(context.Background(), path)
	want := fmt.Sprintf("%x", sha256.Sum256(body))
	if err != nil || got != want {
		t.Fatalf("ZIP hash = %q, %v", got, err)
	}
}

func TestHostSMSMatchesLocalROMAndLaunchesLocalEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cart.sms")
	body := []byte("same ROM bytes")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	local := smsRow()
	local.ID = "kit-id"
	matcher := &localROMMatcher{}
	resolve := func(_ context.Context, id string) (string, error) {
		if id != local.ID {
			t.Fatalf("resolved %q", id)
		}
		return path, nil
	}
	fetch := func(_ context.Context, id string) (string, error) {
		if id != "host-id" {
			t.Fatalf("fetched %q", id)
		}
		return fmt.Sprintf("%x", sha256.Sum256(body)), nil
	}
	core := &fakeLocalCore{cores: []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}}
	host := hostclient.Game{ID: "host-id"}
	learned, err := launchMatchedLocalGame(context.Background(), core, true, false, "", fetch, resolve, []hostclient.Game{local}, matcher, host)
	if err != nil {
		t.Fatal(err)
	}
	if learned != fmt.Sprintf("%x", sha256.Sum256(body)) {
		t.Fatalf("learned hash %q", learned)
	}
	if core.launchPath != path || core.launchID != strings.Repeat("a", 64) {
		t.Fatalf("launch %q %q", core.launchID, core.launchPath)
	}
	core.launchPath = ""
	if _, err := launchMatchedLocalGame(context.Background(), core, true, false, "", func(context.Context, string) (string, error) { return strings.Repeat("b", 64), nil }, resolve, []hostclient.Game{local}, matcher, host); !errors.Is(err, errNotOnKit) || localCoreMessage(err) != "Not on this kit" {
		t.Fatalf("miss: %v", err)
	}
	if _, err := launchMatchedLocalGame(context.Background(), core, true, false, "", func(context.Context, string) (string, error) { return "", errors.New("offline") }, resolve, []hostclient.Game{local}, matcher, host); !errors.Is(err, errCartridgeCheck) || localCoreMessage(err) != "Could not check this kit's cartridges" {
		t.Fatalf("fetch: %v", err)
	}
	if core.launchPath != "" {
		t.Fatal("unexpected launch on miss or fetch failure")
	}
	if _, err := launchMatchedLocalGame(context.Background(), core, false, false, "", nil, resolve, []hostclient.Game{local}, matcher, hostclient.Game{ID: local.ID}); err != nil || core.launchPath != path {
		t.Fatalf("direct local = %q, %v", core.launchPath, err)
	}
}

func TestOnlineIDCollisionStillMatchesLiveBytesUnlessKitRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collision.sms")
	localBytes := []byte("kit bytes")
	if err := os.WriteFile(path, localBytes, 0600); err != nil {
		t.Fatal(err)
	}
	local := smsRow()
	local.ID = "shared-id"
	host := hostclient.Game{ID: local.ID, Title: "Host title"}
	core := &fakeLocalCore{cores: []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}}
	resolve := func(context.Context, string) (string, error) { return path, nil }
	matcher := &localROMMatcher{}
	fetch := func(context.Context, string) (string, error) {
		return fmt.Sprintf("%x", sha256.Sum256([]byte("different host bytes"))), nil
	}
	if _, err := launchMatchedLocalGame(context.Background(), core, true, false, "", fetch, resolve, []hostclient.Game{local}, matcher, host); !errors.Is(err, errNotOnKit) {
		t.Fatalf("different live bytes: %v", err)
	}
	if core.launchPath != "" {
		t.Fatal("ID collision launched without a digest match")
	}
	fetch = func(context.Context, string) (string, error) {
		return fmt.Sprintf("%x", sha256.Sum256(localBytes)), nil
	}
	if _, err := launchMatchedLocalGame(context.Background(), core, true, false, "", fetch, resolve, []hostclient.Game{local}, matcher, host); err != nil || core.launchPath != path {
		t.Fatalf("matching bytes launch=%q err=%v", core.launchPath, err)
	}
	core.launchPath = ""
	fetchCalls := 0
	fetch = func(context.Context, string) (string, error) {
		fetchCalls++
		return "", errors.New("must not fetch a kit row")
	}
	if _, err := launchMatchedLocalGame(context.Background(), core, true, true, "", fetch, resolve, []hostclient.Game{local}, matcher, host); err != nil || core.launchPath != path || fetchCalls != 0 {
		t.Fatalf("kit row launch=%q fetches=%d err=%v", core.launchPath, fetchCalls, err)
	}
}

func TestOnlineLiveDigestWinsOverStaleCachedDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replacement.sms")
	newBytes := []byte("replacement ROM")
	if err := os.WriteFile(path, newBytes, 0600); err != nil {
		t.Fatal(err)
	}
	newHash := fmt.Sprintf("%x", sha256.Sum256(newBytes))
	oldHash := fmt.Sprintf("%x", sha256.Sum256([]byte("old ROM")))
	local := smsRow()
	local.ID = "kit-replacement"
	resolve := func(context.Context, string) (string, error) { return path, nil }
	fetches := 0
	fetch := func(context.Context, string) (string, error) {
		fetches++
		return newHash, nil
	}
	core := &fakeLocalCore{cores: []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}}
	store := mustOpenStore(t)
	const hostID = "sms-replacement-5f961211d191"
	if err := store.RememberROMHash(hostID, oldHash); err != nil {
		t.Fatal(err)
	}
	host := smsRow()
	host.ID = hostID
	host.ROMSHA256 = oldHash // A stale digest on the row must not be trusted while online.
	learned, err := launchMatchedLocalGame(context.Background(), core, true, false, "", fetch, resolve, []hostclient.Game{local}, &localROMMatcher{}, host)
	if err != nil || learned != newHash {
		t.Fatalf("launch learned=%q err=%v", learned, err)
	}
	if err := store.RememberROMHash(hostID, learned); err != nil {
		t.Fatal(err)
	}
	if store.ROMHash(hostID) != newHash || core.launchPath != path || fetches != 1 {
		t.Fatalf("cached=%q launch=%q fetches=%d", store.ROMHash(hostID), core.launchPath, fetches)
	}

	core.launchPath = ""
	oldPath := filepath.Join(t.TempDir(), "old.sms")
	if err := os.WriteFile(oldPath, []byte("old ROM"), 0600); err != nil {
		t.Fatal(err)
	}
	oldLocal := local
	resolveOld := func(context.Context, string) (string, error) { return oldPath, nil }
	if _, err := launchMatchedLocalGame(context.Background(), core, true, false, oldHash, fetch, resolveOld, []hostclient.Game{oldLocal}, &localROMMatcher{}, host); !errors.Is(err, errNotOnKit) {
		t.Fatalf("old local ROM matched replacement: %v", err)
	}
	if core.launchPath != "" {
		t.Fatal("old local ROM launched for replaced host ROM")
	}
}

func TestOfflineCachedHostSMSRowLaunchesMatchingLocalROM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datastorm.sms")
	body := []byte("data storm cartridge")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(body))
	local := smsRow()
	local.ID = "sms-datastorm-2a1507179e25"
	local.Title = "datastorm"
	host := smsRow()
	host.ID = "sms-datastorm-5f961211d191"
	host.Title = "Data Storm"
	host.ROMSHA256 = hash
	resolve := func(_ context.Context, id string) (string, error) {
		if id != local.ID {
			t.Fatalf("resolved %q", id)
		}
		return path, nil
	}
	fetch := func(context.Context, string) (string, error) {
		t.Fatal("offline launch called the host")
		return "", errors.New("offline")
	}
	core := &fakeLocalCore{cores: []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}}
	learned, err := launchMatchedLocalGame(context.Background(), core, false, false, hash, fetch, resolve, []hostclient.Game{local}, &localROMMatcher{}, host)
	if err != nil || learned != "" {
		t.Fatalf("launch err=%v learned=%q", err, learned)
	}
	if core.launchPath != path || core.launchID != strings.Repeat("a", 64) {
		t.Fatalf("launch %q %q", core.launchID, core.launchPath)
	}

	core.launchPath = ""
	host.ROMSHA256 = strings.Repeat("ab", 32)
	if _, err := launchMatchedLocalGame(context.Background(), core, false, false, host.ROMSHA256, fetch, resolve, []hostclient.Game{local}, &localROMMatcher{}, host); !errors.Is(err, errNotOnKit) || localCoreMessage(err) != "Not on this kit" {
		t.Fatalf("hash miss: %v", err)
	}
	if core.launchPath != "" {
		t.Fatal("hash miss launched by title")
	}
}

func TestOfflineHostSMSRowWithoutHashUsesWeakerTitleMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datastorm.sms")
	if err := os.WriteFile(path, []byte("cart"), 0600); err != nil {
		t.Fatal(err)
	}
	local := smsRow()
	local.ID = "sms-datastorm-2a1507179e25"
	local.Title = "datastorm"
	host := smsRow()
	host.ID = "sms-data-storm-5f961211d191"
	host.Title = "Data Storm"
	resolve := func(_ context.Context, id string) (string, error) {
		if id != local.ID {
			return "", errors.New("missing")
		}
		return path, nil
	}
	offline := func(context.Context, string) (string, error) { return "", errors.New("offline") }
	core := &fakeLocalCore{cores: []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}}
	if _, err := launchMatchedLocalGame(context.Background(), core, false, false, "", offline, resolve, []hostclient.Game{local}, &localROMMatcher{}, host); err != nil || core.launchPath != path {
		t.Fatalf("title match %q %v", core.launchPath, err)
	}

	core.launchPath = ""
	other := local
	other.ID = "sms-other-aaaaaaaaaaaa"
	other.Title = "Other"
	host.Title = "Data Storm"
	host.ID = "sms-datastorm-5f961211d191"
	if _, err := launchMatchedLocalGame(context.Background(), core, false, false, "", offline, resolve, []hostclient.Game{other}, &localROMMatcher{}, host); !errors.Is(err, errNotOnKit) || localCoreMessage(err) != "Not on this kit" {
		t.Fatalf("no match: %v", err)
	}
	if core.launchPath != "" {
		t.Fatal("missing cartridge launched")
	}

	twin := local
	twin.ID = "sms-datastorm-aaaaaaaaaaaa"
	if _, err := launchMatchedLocalGame(context.Background(), core, false, false, "", offline, resolve, []hostclient.Game{local, twin}, &localROMMatcher{}, host); !errors.Is(err, errNeedsHost) || localCoreMessage(err) != "Needs the host" {
		t.Fatalf("ambiguous title: %v", err)
	}
	if core.launchPath != "" {
		t.Fatal("ambiguous title launched")
	}
}

func TestSMSHashFillRemembersDigestForOfflineLaunch(t *testing.T) {
	store := mustOpenStore(t)
	const id = "sms-datastorm-5f961211d191"
	hash := strings.Repeat("ab", 32)
	row := smsRow()
	row.ID = id
	fillSMSROMHashes(context.Background(), func(_ context.Context, got string) (string, error) {
		if got != id {
			t.Fatalf("filled %q", got)
		}
		return hash, nil
	}, store, []hostclient.Game{row, {ID: "pong", Title: "Pong", System: "pong", State: "available", RootOnline: true, Launchable: true}})
	if store.ROMHash(id) != hash {
		t.Fatalf("cached hash %q", store.ROMHash(id))
	}
	again := 0
	fillSMSROMHashes(context.Background(), func(context.Context, string) (string, error) {
		again++
		return strings.Repeat("cd", 32), nil
	}, store, []hostclient.Game{row})
	if again != 1 || store.ROMHash(id) != strings.Repeat("cd", 32) {
		t.Fatalf("refill calls=%d hash=%q", again, store.ROMHash(id))
	}
}

func TestOnlineDigestFailureNeverFallsBackToTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datastorm.sms")
	if err := os.WriteFile(path, []byte("different bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	local := smsRow()
	local.ID = "sms-datastorm-2a1507179e25"
	local.Title = "Data Storm"
	host := smsRow()
	host.ID = "sms-datastorm-5f961211d191"
	host.Title = "Data Storm"
	resolve := func(context.Context, string) (string, error) { return path, nil }
	core := &fakeLocalCore{cores: []localcores.Core{{CoreID: "fes.sms", PackageID: strings.Repeat("a", 64)}}}
	for name, fetch := range map[string]func(context.Context, string) (string, error){
		"error":   func(context.Context, string) (string, error) { return "", errors.New("host 503") },
		"invalid": func(context.Context, string) (string, error) { return "not-a-digest", nil },
	} {
		t.Run(name, func(t *testing.T) {
			core.launchPath = ""
			_, err := launchMatchedLocalGame(context.Background(), core, true, false, strings.Repeat("ab", 32), fetch, resolve, []hostclient.Game{local}, &localROMMatcher{}, host)
			if !errors.Is(err, errCartridgeCheck) || localCoreMessage(err) != "Could not check this kit's cartridges" {
				t.Fatalf("host-up digest failure: %v", err)
			}
			if core.launchPath != "" {
				t.Fatal("host-up digest failure launched a title match")
			}
		})
	}
}

func TestSMSHashFillRefreshesEachIDAtMostOncePerInterval(t *testing.T) {
	var f smsHashFill
	now := time.Now()
	if !f.due("sms-a-000000000000", now) {
		t.Fatal("first check not due")
	}
	if f.due("sms-a-000000000000", now.Add(30*time.Second)) {
		t.Fatal("re-fetched on the next catalog reload")
	}
	if !f.due("sms-a-000000000000", now.Add(smsHashRefreshEvery)) {
		t.Fatal("not refreshed after the interval")
	}
}

func TestSMSHashFillCancellationLeavesUnattemptedRowsEligible(t *testing.T) {
	store := mustOpenStore(t)
	rows := []hostclient.Game{}
	for _, id := range []string{"sms-a-000000000000", "sms-b-000000000000", "sms-c-000000000000"} {
		row := smsRow()
		row.ID = id
		rows = append(rows, row)
	}
	var fill smsHashFill
	started := make(chan struct{})
	firstDone := make(chan struct{})
	var mu sync.Mutex
	var calls []string
	firstFetch := func(ctx context.Context, id string) (string, error) {
		mu.Lock()
		calls = append(calls, id)
		mu.Unlock()
		if id == rows[0].ID {
			close(started)
			<-ctx.Done()
			close(firstDone)
			return "", ctx.Err()
		}
		return strings.Repeat("ab", 32), nil
	}
	fill.start(context.Background(), firstFetch, store, rows)
	<-started
	secondDone := make(chan struct{})
	fill.start(context.Background(), func(_ context.Context, id string) (string, error) {
		mu.Lock()
		calls = append(calls, id)
		mu.Unlock()
		if id == rows[2].ID {
			close(secondDone)
		}
		return strings.Repeat("cd", 32), nil
	}, store, rows)
	<-firstDone
	<-secondDone
	// Wait for the last digest write: RememberROMHash holds the store lock
	// through the atomic file write, so the value is visible only after it.
	deadline := time.Now().Add(5 * time.Second)
	for store.ROMHash(rows[2].ID) != strings.Repeat("cd", 32) {
		if time.Now().After(deadline) {
			t.Fatal("second fill did not store the last digest")
		}
		time.Sleep(time.Millisecond)
	}
	fill.stop()
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 3 || calls[0] != rows[0].ID || calls[1] != rows[1].ID || calls[2] != rows[2].ID {
		t.Fatalf("fetch sequence %v", calls)
	}
}
