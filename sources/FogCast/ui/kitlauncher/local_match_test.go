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
	if err := launchMatchedLocalGame(context.Background(), core, fetch, resolve, []hostclient.Game{local}, matcher, "host-id"); err != nil {
		t.Fatal(err)
	}
	if core.launchPath != path || core.launchID != strings.Repeat("a", 64) {
		t.Fatalf("launch %q %q", core.launchID, core.launchPath)
	}
	core.launchPath = ""
	if err := launchMatchedLocalGame(context.Background(), core, func(context.Context, string) (string, error) { return strings.Repeat("b", 64), nil }, resolve, []hostclient.Game{local}, matcher, "host-id"); !errors.Is(err, errNotOnKit) || localCoreMessage(err) != "Not on this kit" {
		t.Fatalf("miss: %v", err)
	}
	if err := launchMatchedLocalGame(context.Background(), core, func(context.Context, string) (string, error) { return "", errors.New("offline") }, resolve, []hostclient.Game{local}, matcher, "host-id"); !errors.Is(err, errCartridgeCheck) || localCoreMessage(err) != "Could not check this kit's cartridges" {
		t.Fatalf("fetch: %v", err)
	}
	if core.launchPath != "" {
		t.Fatal("unexpected launch on miss or fetch failure")
	}
	if err := launchMatchedLocalGame(context.Background(), core, nil, resolve, []hostclient.Game{local}, matcher, local.ID); err != nil || core.launchPath != path {
		t.Fatalf("direct local = %q, %v", core.launchPath, err)
	}
}
