package fogcast

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast/catalog"
	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/romsource"
)

func TestNativeSMSROMHashRawZIPAndBounds(t *testing.T) {
	dir := t.TempDir()
	root := catalog.Root{ID: "sms-root", System: protocol.SystemSMS, Path: dir}
	s := newService(Config{Libraries: []catalog.Root{root}}, Paths{Staging: filepath.Join(dir, "staging")}, &fakeServiceCatalog{}, &fakeServiceScanner{}, &romsource.Preparer{StagingRoot: filepath.Join(dir, "staging")}, nil)
	body := []byte("SMS cartridge")
	want := fmt.Sprintf("%x", sha256.Sum256(body))
	rawPath := filepath.Join(dir, "game.sms")
	if err := os.WriteFile(rawPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	rawInfo, _ := os.Stat(rawPath)
	game := catalog.Game{ID: "sms-game", LibraryID: root.ID, RelativePath: "game.sms", System: protocol.SystemSMS, Kind: catalog.SourceKindRaw, State: catalog.SourceStateAvailable, RootOnline: true, Fingerprint: catalog.Fingerprint{SourceSize: rawInfo.Size(), ModifiedNS: rawInfo.ModTime().UnixNano()}}
	got, err := s.NativeSMSROMHash(context.Background(), game)
	if err != nil || got != want {
		t.Fatalf("raw = %q, %v", got, err)
	}
	game.Fingerprint.SourceSize = maxSMSMatchBytes + 1
	got, err = s.NativeSMSROMHash(context.Background(), game)
	if err != nil || got != "" {
		t.Fatalf("oversize raw = %q, %v", got, err)
	}
	zipPath := filepath.Join(dir, "game.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("folder/game.sms")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	zipInfo, _ := os.Stat(zipPath)
	game.Kind, game.RelativePath = catalog.SourceKindZIP, "game.zip"
	game.Fingerprint = catalog.Fingerprint{SourceSize: zipInfo.Size(), ModifiedNS: zipInfo.ModTime().UnixNano(), ZIPMember: "folder/game.sms", ZIPSize: int64(len(body)), ZIPCRC32: crc32.ChecksumIEEE(body), ZIPEntryCount: 1}
	got, err = s.NativeSMSROMHash(context.Background(), game)
	if err != nil || got != want {
		t.Fatalf("zip = %q, %v", got, err)
	}
	game.Fingerprint.ZIPSize = maxSMSMatchBytes + 1
	got, err = s.NativeSMSROMHash(context.Background(), game)
	if err != nil || got != "" {
		t.Fatalf("oversize = %q, %v", got, err)
	}
	game.System = protocol.SystemSNES
	got, err = s.NativeSMSROMHash(context.Background(), game)
	if err != nil || got != "" {
		t.Fatalf("non-SMS = %q, %v", got, err)
	}
}
