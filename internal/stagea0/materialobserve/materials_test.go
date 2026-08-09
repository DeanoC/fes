package materialobserve

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DeanoC/FogCast-POC/internal/stagea0/policy"
)

func TestManifestCanonicalRoundTrip(t *testing.T) {
	manifest := validManifest()
	raw, err := Encode(manifest)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() = %v", err)
	}
	reencoded, err := Encode(decoded)
	if err != nil || !bytes.Equal(raw, reencoded) {
		t.Fatalf("canonical bytes changed: err=%v", err)
	}
	for _, hostile := range [][]byte{
		append([]byte(" "), raw...),
		append(append([]byte(nil), raw...), []byte("{}")...),
		[]byte(`{"format":1,"schema":"fogcast.stage-a0.materials.v1","unknown":true}` + "\n"),
	} {
		if _, err := Decode(hostile); !hasCode(err, CodeSchemaInvalid) {
			t.Fatalf("Decode(%q) = %v, want %s", hostile, err, CodeSchemaInvalid)
		}
	}
}

func TestValidateManifestRejectsOrderingAndLicenseClaims(t *testing.T) {
	manifest := validManifest()
	manifest.Records[0], manifest.Records[1] = manifest.Records[1], manifest.Records[0]
	if err := Validate(manifest); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("record order error = %v, want %s", err, CodeSchemaInvalid)
	}
	manifest = validManifest()
	manifest.Records[0].LicenseState = "reviewed"
	if err := Validate(manifest); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("license claim = %v, want %s", err, CodeSchemaInvalid)
	}
	manifest = validManifest()
	manifest.Unresolved[0] = manifest.Unresolved[1]
	if err := Validate(manifest); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("unresolved order = %v, want %s", err, CodeSchemaInvalid)
	}
}

func TestReviewedManifestHasAnExplicitLegalBoundary(t *testing.T) {
	candidate := validManifest()
	candidate.Schema = SchemaReviewedV1
	candidate.Status = StatusReviewed
	candidate.SourceAvailability = SourceAvailabilityDurable
	if err := ValidateReviewed(candidate); !hasCode(err, CodeSchemaInvalid) {
		t.Fatalf("candidate relabel was accepted: %v", err)
	}

	reviewed := reviewedManifest()
	raw, err := EncodeReviewed(reviewed)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeReviewed(raw)
	if err != nil {
		t.Fatalf("DecodeReviewed() = %v", err)
	}
	if decoded.Status != StatusReviewed || decoded.SourceAvailability != SourceAvailabilityDurable || len(decoded.Unresolved) != 1 || decoded.Unresolved[0] != "material-license-review" {
		t.Fatalf("reviewed boundary was not retained: %#v", decoded)
	}
}

func TestTreeDigestIsContentAndSymlinkBound(t *testing.T) {
	left := t.TempDir()
	right := t.TempDir()
	for _, root := range []string{left, right} {
		if err := os.Mkdir(filepath.Join(root, "lib"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "lib", "libc.so"), []byte("same"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("lib/libc.so", filepath.Join(left, "libc.so")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("lib/libc.so", filepath.Join(right, "libc.so")); err != nil {
		t.Fatal(err)
	}
	leftHash, err := treeDigest(left)
	if err != nil {
		t.Fatal(err)
	}
	rightHash, err := treeDigest(right)
	if err != nil || leftHash != rightHash {
		t.Fatalf("equivalent trees differ: left=%s right=%s err=%v", leftHash, rightHash, err)
	}
	if err := os.WriteFile(filepath.Join(right, "lib", "libc.so"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	changedHash, err := treeDigest(right)
	if err != nil || changedHash == leftHash {
		t.Fatalf("content change did not change tree digest: %s %v", changedHash, err)
	}
	escape := t.TempDir()
	if err := os.Symlink(escape, filepath.Join(left, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := treeDigest(left); err == nil {
		t.Fatal("escaping symlink was accepted")
	}
}

func TestValidateBuildLogRequiresLockedAdapterObservation(t *testing.T) {
	authority := validManifest().Authority
	valid := []byte("STAGE_A0_JOB_COUNT=1\nSOURCE_DATE_EPOCH=1786215171 make clean VDATE=260808 make V=1 VDATE=260808\n")
	if err := validateBuildLog(valid, authority); err != nil {
		t.Fatalf("valid build log rejected: %v", err)
	}
	for _, raw := range [][]byte{
		[]byte("SOURCE_DATE_EPOCH=1786215171 make clean VDATE=260808 make V=1 VDATE=260808\n"),
		[]byte("STAGE_A0_JOB_COUNT=2\nSOURCE_DATE_EPOCH=1786215171 make clean VDATE=260808 make V=1 VDATE=260808\n"),
	} {
		if err := validateBuildLog(raw, authority); !hasCode(err, CodeInputInvalid) {
			t.Fatalf("invalid build log %q = %v, want %s", raw, err, CodeInputInvalid)
		}
	}
}

func validManifest() Manifest {
	authority := policy.Authority{
		UpstreamCommit:   "1111111111111111111111111111111111111111",
		UpstreamTree:     "2222222222222222222222222222222222222222",
		ForkCommit:       "3333333333333333333333333333333333333333",
		ForkTree:         "4444444444444444444444444444444444444444",
		ForkParentCommit: "1111111111111111111111111111111111111111",
		PatchCommits:     []string{"3333333333333333333333333333333333333333"},
		SourceDateEpoch:  1786215171,
		VDate:            "260808",
	}
	return Manifest{
		Format: FormatV1, Schema: SchemaV1, Status: StatusCandidateObserved, SourceAvailability: SourceAvailabilityLocal,
		Authority:      authority,
		ReceiptSHA256:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		BuildLogSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Records: []Record{
			{ID: "main-fork", Role: "consumed-build-input", Kind: "git-local", Commit: authority.ForkCommit, Tree: authority.ForkTree, LicenseState: licenseUnreviewed},
			{ID: "main-upstream", Role: "consumed-build-input", Kind: "git-https", Commit: authority.UpstreamCommit, Tree: authority.UpstreamTree, LicenseState: licenseUnreviewed},
			{ID: "policy-source-set", Role: "consumed-build-input", Kind: "material-file", ParentID: "main-fork", Path: "source-set.json", Size: 1, SHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", LicenseState: licenseUnreviewed},
			{ID: "toolchain", Role: "consumed-build-input", Kind: "archive-https", Size: 1, SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", LicenseState: licenseUnreviewed},
		},
		Unresolved: []string{"build-log-review-identity", "container-durable-provenance"},
	}
}

func reviewedManifest() Manifest {
	m := validManifest()
	m.Schema = SchemaReviewedV1
	m.Status = StatusReviewed
	m.SourceAvailability = SourceAvailabilityDurable
	m.Unresolved = []string{"material-license-review"}
	for i := range m.Records {
		m.Records[i].LicenseState = licenseReviewRequired
		m.Records[i].LicenseIDs = []string{"review-" + m.Records[i].ID}
		switch m.Records[i].Kind {
		case "git-local":
			m.Records[i].Kind = "git-https"
			m.Records[i].URL = "https://github.com/DeanoC/Main_MiSTer.git"
		case "git-https":
			m.Records[i].URL = "https://github.com/MiSTer-devel/Main_MiSTer.git"
		case "archive-https":
			m.Records[i].URL = "https://example.com/toolchain.tar.xz"
		}
	}
	return m
}

func hasCode(err error, want Code) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.Code == want
}
