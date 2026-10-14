package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DeanoC/FogCast/appliance"
)

func TestChooseDeadline(t *testing.T) {
	cases := []struct {
		action         string
		explicit       bool
		supplied, want time.Duration
		size           int64
	}{
		{action: "update", size: 128 << 20, want: 21 * time.Minute},
		{action: "update", explicit: true, supplied: 2 * time.Minute, size: 128 << 20, want: 2 * time.Minute},
		{action: "rollback", want: 10 * time.Minute},
		{action: "status", want: time.Minute},
		{action: "confirm", want: 15 * time.Minute},
	}
	for _, tc := range cases {
		got, _ := chooseDeadline(tc.action, tc.explicit, tc.supplied, tc.size)
		if got != tc.want {
			t.Errorf("%s explicit=%t: got %s, want %s", tc.action, tc.explicit, got, tc.want)
		}
	}
}

func TestValidateConfirmFlags(t *testing.T) {
	hash := strings.Repeat("a", 64)
	cases := []struct {
		release, hash string
		ok            bool
	}{
		{ok: false},
		{release: "dir", ok: true},
		{hash: hash, ok: true},
		{release: "dir", hash: hash, ok: false},
		{hash: strings.Repeat("A", 64), ok: false},
		{hash: strings.Repeat("a", 63), ok: false},
	}
	for _, tc := range cases {
		err := validateActionFlags("confirm", tc.release, tc.hash)
		if (err == nil) != tc.ok {
			t.Errorf("release=%q hash=%q: err=%v", tc.release, tc.hash, err)
		}
	}
}

func releaseJSON(formatField, extra string) string {
	body := `{"format":1,"board":"de10-nano","boot_abi":"fes-bootstrap-v1","version":"v1","kernel_sha256":"` + strings.Repeat("a", 64) + `","image_sha256":"` + strings.Repeat("b", 64) + `","image_size":4096,"fes_revision":"` + strings.Repeat("c", 40) + `","fogcast_revision":"` + strings.Repeat("d", 40) + `","runtime_revision":"` + strings.Repeat("e", 40) + `"}`
	if formatField == "" && extra == "" {
		return body
	}
	insert := ""
	if formatField != "" {
		insert = `"manifest_format":"` + formatField + `"`
	}
	if extra != "" {
		if insert != "" {
			insert += ","
		}
		insert += extra
	}
	return strings.Replace(body, `"format":1`, `"format":1,`+insert, 1)
}

func writeRelease(t *testing.T, raw string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "release.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadReleaseManifestFormat(t *testing.T) {
	var log bytes.Buffer
	dir := writeRelease(t, releaseJSON("1.1", `"future_field":{"keep":true}`))
	manifest, err := readReleaseManifest(dir, &log)
	if err != nil || manifest.ManifestFormat != "1.1" || manifest.Version != "v1" {
		t.Fatalf("higher minor: %+v %v", manifest, err)
	}
	if !bytes.Contains(log.Bytes(), []byte("future_field")) {
		t.Fatalf("ignored field was not logged: %s", log.String())
	}
	log.Reset()
	legacy, err := readReleaseManifest(writeRelease(t, releaseJSON("", "")), &log)
	if err != nil || legacy.ManifestFormat != "" || log.Len() != 0 {
		t.Fatalf("legacy manifest: %+v log %q err %v", legacy, log.String(), err)
	}
	if _, err = readReleaseManifest(writeRelease(t, releaseJSON("2.0", `"future_field":1`)), &log); err == nil {
		t.Fatal("accepted unknown major")
	}
	missing := strings.Replace(releaseJSON("1.1", `"future_field":1`), `"version":"v1",`, "", 1)
	if _, err = readReleaseManifest(writeRelease(t, missing), &log); err == nil {
		t.Fatal("accepted missing version")
	}
	if legacy.Board != appliance.Board {
		t.Fatalf("board %s", legacy.Board)
	}
}
