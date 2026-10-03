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
	"testing"

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
	learned, err := launchMatchedLocalGame(context.Background(), core, fetch, resolve, []hostclient.Game{local}, matcher, host)
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
	if _, err := launchMatchedLocalGame(context.Background(), core, func(context.Context, string) (string, error) { return strings.Repeat("b", 64), nil }, resolve, []hostclient.Game{local}, matcher, host); !errors.Is(err, errNotOnKit) || localCoreMessage(err) != "Not on this kit" {
		t.Fatalf("miss: %v", err)
	}
	if _, err := launchMatchedLocalGame(context.Background(), core, func(context.Context, string) (string, error) { return "", errors.New("offline") }, resolve, []hostclient.Game{local}, matcher, host); !errors.Is(err, errNeedsHost) || localCoreMessage(err) != "Needs the host" {
		t.Fatalf("fetch: %v", err)
	}
	if core.launchPath != "" {
		t.Fatal("unexpected launch on miss or fetch failure")
	}
	if _, err := launchMatchedLocalGame(context.Background(), core, nil, resolve, []hostclient.Game{local}, matcher, hostclient.Game{ID: local.ID}); err != nil || core.launchPath != path {
		t.Fatalf("direct local = %q, %v", core.launchPath, err)
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
	learned, err := launchMatchedLocalGame(context.Background(), core, fetch, resolve, []hostclient.Game{local}, &localROMMatcher{}, host)
	if err != nil || learned != "" {
		t.Fatalf("launch err=%v learned=%q", err, learned)
	}
	if core.launchPath != path || core.launchID != strings.Repeat("a", 64) {
		t.Fatalf("launch %q %q", core.launchID, core.launchPath)
	}

	core.launchPath = ""
	host.ROMSHA256 = strings.Repeat("ab", 32)
	if _, err := launchMatchedLocalGame(context.Background(), core, fetch, resolve, []hostclient.Game{local}, &localROMMatcher{}, host); !errors.Is(err, errNotOnKit) || localCoreMessage(err) != "Not on this kit" {
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
	if _, err := launchMatchedLocalGame(context.Background(), core, offline, resolve, []hostclient.Game{local}, &localROMMatcher{}, host); err != nil || core.launchPath != path {
		t.Fatalf("title match %q %v", core.launchPath, err)
	}

	core.launchPath = ""
	other := local
	other.ID = "sms-other-aaaaaaaaaaaa"
	other.Title = "Other"
	host.Title = "Data Storm"
	host.ID = "sms-datastorm-5f961211d191"
	if _, err := launchMatchedLocalGame(context.Background(), core, offline, resolve, []hostclient.Game{other}, &localROMMatcher{}, host); !errors.Is(err, errNotOnKit) || localCoreMessage(err) != "Not on this kit" {
		t.Fatalf("no match: %v", err)
	}
	if core.launchPath != "" {
		t.Fatal("missing cartridge launched")
	}

	twin := local
	twin.ID = "sms-datastorm-aaaaaaaaaaaa"
	if _, err := launchMatchedLocalGame(context.Background(), core, offline, resolve, []hostclient.Game{local, twin}, &localROMMatcher{}, host); !errors.Is(err, errNeedsHost) || localCoreMessage(err) != "Needs the host" {
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
	if again != 0 || store.ROMHash(id) != hash {
		t.Fatalf("refill calls=%d hash=%q", again, store.ROMHash(id))
	}
}
