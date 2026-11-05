package corepackage

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nanoManifest(manifest []byte) []byte {
	m := strings.ReplaceAll(string(manifest), "de10_nano", "de25_nano")
	return []byte(strings.ReplaceAll(m, "5CSEBA6U23I7", "A5EB013BB23BE4SCS"))
}

func TestNanoRetainedFormat2Recognition(t *testing.T) {
	manifest, payload := fixtureBytes(t, loadCases(t)[0])
	old, err := RetainArchive(context.Background(), int64(len(canonicalArchive(manifest, payload))), bytes.NewReader(canonicalArchive(manifest, payload)))
	if err != nil {
		t.Fatal(err)
	}
	manifest = nanoManifest(manifest)
	archive := canonicalArchive(manifest, payload)
	retained, err := RetainArchive(context.Background(), int64(len(archive)), bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	i := retained.Inspection()
	if i.PackageID == old.Inspection().PackageID || i.Descriptor.Target.Platform != "de25_nano" || i.Descriptor.Target.Device != "A5EB013BB23BE4SCS" {
		t.Fatalf("target/identity %+v", i)
	}
	clear(archive)
	got, err := io.ReadAll(retained.Payload())
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatal("payload ownership", err)
	}
	for _, bad := range []string{
		strings.Replace(string(manifest), "de25_nano", "de10_nano", 1),
		strings.Replace(string(manifest), "A5EB013BB23BE4SCS", "5CSEBA6U23I7", 1),
		strings.Replace(string(manifest), "A5EB013BB23BE4SCS", "A5EB013BB23BE4SC", 1),
	} {
		a := canonicalArchive([]byte(bad), payload)
		if _, err := RetainArchive(context.Background(), int64(len(a)), bytes.NewReader(a)); err == nil {
			t.Fatal("mismatched target accepted")
		}
	}
}

func TestNanoCycloneVROMAndTwoSourceFormatsRemainUnsupported(t *testing.T) {
	for _, version := range []string{"3", "4"} {
		root := filepath.Join("testdata", "core-bundle-v"+version)
		manifest, err := os.ReadFile(filepath.Join(root, "manifests/valid-basic.toml"))
		if err != nil {
			t.Fatal(err)
		}
		descriptor, err := decodeManifest(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateDescriptor(descriptor); err != nil {
			t.Fatalf("v%s baseline: %v", version, err)
		}
		descriptor, err = decodeManifest(nanoManifest(manifest))
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateDescriptor(descriptor); err == nil {
			t.Fatalf("Nano v%s linker enabled: %v", version, err)
		}
	}
}
