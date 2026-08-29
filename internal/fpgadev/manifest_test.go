package fpgadev

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testRunID        = "0123456789abcdef0123456789abcdef"
	testArtifactHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSourceCommit = "0123456789abcdef0123456789abcdef01234567"
)

func validManifest() Manifest {
	return Manifest{
		Schema:           1,
		RunID:            testRunID,
		Experiment:       "020_linux_mailbox",
		Board:            "misterpi",
		BuildLane:        "oss",
		ArtifactFilename: "top.rbf",
		ArtifactSize:     12,
		ArtifactSHA256:   testArtifactHash,
		SourceCommit:     testSourceCommit,
	}
}

func canonicalManifestBytes(t *testing.T, manifest Manifest) []byte {
	t.Helper()
	raw, err := manifest.MarshalCanonical()
	if err != nil {
		t.Fatalf("marshal canonical manifest: %v", err)
	}
	return raw
}

func TestManifestFixtureUsesExactSchemaV1(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "manifest-oss.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ParseManifest(raw)
	if err != nil {
		t.Fatalf("ParseManifest(fixture): %v", err)
	}
	if manifest != (Manifest{
		Schema:           1,
		RunID:            testRunID,
		Experiment:       "020_linux_mailbox",
		Board:            "misterpi",
		BuildLane:        "oss",
		ArtifactFilename: "top.rbf",
		ArtifactSize:     7007204,
		ArtifactSHA256:   testArtifactHash,
		SourceCommit:     testSourceCommit,
	}) {
		t.Fatalf("fixture decoded as %#v", manifest)
	}
	if !bytes.Equal(raw, canonicalManifestBytes(t, manifest)) {
		t.Fatal("fixture is not the canonical manifest encoding")
	}
}

func TestManifestNamesTheSeparateUnsignedArtifactBundleMembers(t *testing.T) {
	if ManifestArtifact != "top.rbf" || ManifestEvidence != "resource_evidence.json" || ManifestChecksums != "bundle.sha256" {
		t.Fatalf("artifact bundle names = %q, %q, %q", ManifestArtifact, ManifestEvidence, ManifestChecksums)
	}
	if got := validManifest().Schema; got != ManifestSchemaVersion {
		t.Fatalf("artifact manifest schema = %d, want schema-v1", got)
	}
	if ManifestSchemaVersion != 1 {
		t.Fatalf("ManifestSchemaVersion = %d, want 1", ManifestSchemaVersion)
	}
}

func TestManifestValidateAcceptsOSSAndOracle(t *testing.T) {
	for _, lane := range []string{"oss", "oracle"} {
		manifest := validManifest()
		manifest.BuildLane = lane
		if err := manifest.Validate(); err != nil {
			t.Fatalf("lane %q rejected: %v", lane, err)
		}
	}
}

func TestManifestRejectsHostileMatrix(t *testing.T) {
	base := string(canonicalManifestBytes(t, validManifest()))
	mutations := map[string]string{
		"missing schema":           strings.Replace(base, `{"schema":1,`, `{`, 1),
		"missing run id":           strings.Replace(base, `"run_id":"`+testRunID+`",`, "", 1),
		"missing source commit":    strings.Replace(base, `,"source_commit":"`+testSourceCommit+`"`, "", 1),
		"unknown field":            strings.Replace(base, `{"schema":1,`, `{"unexpected":true,"schema":1,`, 1),
		"duplicate key":            strings.Replace(base, `"run_id":"`+testRunID+`",`, `"run_id":"`+testRunID+`","run_id":"`+testRunID+`",`, 1),
		"trailing value":           base + `{"schema":1}`,
		"uppercase run id":         strings.Replace(base, testRunID, strings.ToUpper(testRunID), 1),
		"uppercase artifact hash":  strings.Replace(base, testArtifactHash, strings.ToUpper(testArtifactHash), 1),
		"short artifact hash":      strings.Replace(base, testArtifactHash, "aaaa", 1),
		"uppercase source commit":  strings.Replace(base, testSourceCommit, strings.ToUpper(testSourceCommit), 1),
		"unsafe artifact filename": strings.Replace(base, `"top.rbf"`, `"../top.rbf"`, 1),
		"wrong experiment":         strings.Replace(base, `"020_linux_mailbox"`, `"other"`, 1),
		"wrong board":              strings.Replace(base, `"misterpi"`, `"de10nano"`, 1),
		"wrong lane":               strings.Replace(base, `"build_lane":"oss"`, `"build_lane":"quartus"`, 1),
		"zero size":                strings.Replace(base, `"artifact_size":12`, `"artifact_size":0`, 1),
		"oversize":                 strings.Replace(base, `"artifact_size":12`, `"artifact_size":16777217`, 1),
		"fractional size":          strings.Replace(base, `"artifact_size":12`, `"artifact_size":12.0`, 1),
		"wrong field order":        strings.Replace(base, `{"schema":1,"run_id":"`+testRunID+`"`, `{"run_id":"`+testRunID+`","schema":1`, 1),
	}
	for name, raw := range mutations {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(raw)); err == nil {
				t.Fatal("hostile manifest was accepted")
			}
		})
	}
}

func TestManifestParserRejectsNonObjectAndTrailingWhitespaceData(t *testing.T) {
	valid := canonicalManifestBytes(t, validManifest())
	for name, raw := range map[string][]byte{
		"array":    []byte(`[]`),
		"null":     []byte(`null`),
		"trailing": append(append([]byte(nil), valid...), []byte("\nnull")...),
	} {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest(raw); err == nil {
				t.Fatal("invalid top-level JSON was accepted")
			}
		})
	}
}

func TestManifestCanonicalMarshalRoundTrip(t *testing.T) {
	manifest := validManifest()
	manifest.ArtifactSize = 7007204
	raw := canonicalManifestBytes(t, manifest)
	if string(raw) != `{"schema":1,"run_id":"0123456789abcdef0123456789abcdef","experiment":"020_linux_mailbox","board":"misterpi","build_lane":"oss","artifact_filename":"top.rbf","artifact_size":7007204,"artifact_sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_commit":"0123456789abcdef0123456789abcdef01234567"}`+"\n" {
		t.Fatalf("canonical manifest = %q", raw)
	}
	got, err := ParseManifest(raw)
	if err != nil || got != manifest {
		t.Fatalf("round trip = %#v, %v", got, err)
	}
}

func TestManifestParserDoesNotAcceptJSONNumberCoercion(t *testing.T) {
	manifest := validManifest()
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"schema":1`), []byte(`"schema":"1"`), 1)
	if _, err := ParseManifest(raw); err == nil {
		t.Fatal("string schema was accepted as a number")
	}
}
