package fpgadev

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testResourceEvidenceSource = "0123456789abcdef0123456789abcdef01234567"
const testResourceEvidenceArtifact = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const testResourceEvidenceReport = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func validResourceEvidenceV2() ResourceEvidenceV2 {
	return ResourceEvidenceV2{
		Schema:                      2,
		Experiment:                  ManifestExperiment,
		Board:                       ManifestBoard,
		BuildLane:                   BuildLaneOSS,
		SourceCommit:                testResourceEvidenceSource,
		ArtifactSHA256:              testResourceEvidenceArtifact,
		SynthesisReportSHA256:       testResourceEvidenceReport,
		ClockInputs:                 1,
		ExternalInputPorts:          0,
		ExternalOutputPorts:         0,
		BidirectionalPorts:          0,
		HPSGeneralPurposeInterfaces: 1,
		PLLBlocks:                   0,
		DSPBlocks:                   0,
		BlockMemoryBits:             0,
		LUTRAMBits:                  0,
		SDRAMInterfaces:             0,
	}
}

func validResourceEvidenceBytes(t *testing.T) []byte {
	t.Helper()
	raw, err := validResourceEvidenceV2().MarshalCanonical()
	if err != nil {
		t.Fatalf("MarshalCanonical: %v", err)
	}
	return raw
}

func TestResourceEvidenceV2ParsesCanonicalUnsignedSchema(t *testing.T) {
	raw := validResourceEvidenceBytes(t)
	if len(raw) > 2048 || !bytes.HasSuffix(raw, []byte("\n")) || bytes.ContainsAny(raw[:len(raw)-1], " \t\r") {
		t.Fatalf("evidence bytes are not compact and newline terminated: %q", raw)
	}
	got, err := ParseResourceEvidenceV2(raw)
	if err != nil {
		t.Fatalf("ParseResourceEvidenceV2: %v", err)
	}
	if got != validResourceEvidenceV2() {
		t.Fatalf("evidence = %#v, want %#v", got, validResourceEvidenceV2())
	}
	canonical, err := got.MarshalCanonical()
	if err != nil || !bytes.Equal(canonical, raw) {
		t.Fatalf("canonical round trip = %q, %v", canonical, err)
	}
	if strings.Contains(strings.ToLower(string(raw)), "sign") || strings.Contains(string(raw), "signature") {
		t.Fatal("unsigned evidence contains signing material")
	}
}

func TestResourceEvidenceV2RejectsMalformedCanonicalMatrix(t *testing.T) {
	base := string(validResourceEvidenceBytes(t))
	mutations := map[string]string{
		"schema one":              strings.Replace(base, `"schema":2`, `"schema":1`, 1),
		"schema string":           strings.Replace(base, `"schema":2`, `"schema":"2"`, 1),
		"missing field":           strings.Replace(base, `,"synthesis_report_sha256":"`+testResourceEvidenceReport+`"`, "", 1),
		"unknown field":           strings.Replace(base, `{"schema":2,`, `{"extra":true,"schema":2,`, 1),
		"duplicate field":         strings.Replace(base, `"board":"misterpi",`, `"board":"misterpi","board":"misterpi",`, 1),
		"wrong order":             strings.Replace(base, `{"schema":2,"experiment":"020_linux_mailbox"`, `{"experiment":"020_linux_mailbox","schema":2`, 1),
		"trailing data":           base + `null`,
		"trailing whitespace":     strings.TrimSuffix(base, "\n") + " \n",
		"no newline":              strings.TrimSuffix(base, "\n"),
		"wrong experiment":        strings.Replace(base, `"experiment":"020_linux_mailbox"`, `"experiment":"other"`, 1),
		"wrong board":             strings.Replace(base, `"board":"misterpi"`, `"board":"de10nano"`, 1),
		"wrong lane":              strings.Replace(base, `"build_lane":"oss"`, `"build_lane":"oraclex"`, 1),
		"uppercase commit":        strings.Replace(base, testResourceEvidenceSource, strings.ToUpper(testResourceEvidenceSource), 1),
		"short artifact hash":     strings.Replace(base, testResourceEvidenceArtifact, "aaaa", 1),
		"bad report hash":         strings.Replace(base, testResourceEvidenceReport, "not-a-hash", 1),
		"count float":             strings.Replace(base, `"clock_inputs":1`, `"clock_inputs":1.0`, 1),
		"count exponent":          strings.Replace(base, `"clock_inputs":1`, `"clock_inputs":1e0`, 1),
		"count string":            strings.Replace(base, `"clock_inputs":1`, `"clock_inputs":"1"`, 1),
		"count bool":              strings.Replace(base, `"clock_inputs":1`, `"clock_inputs":true`, 1),
		"count negative":          strings.Replace(base, `"clock_inputs":1`, `"clock_inputs":-1`, 1),
		"count overflow":          strings.Replace(base, `"clock_inputs":1`, `"clock_inputs":4294967296`, 1),
		"clock count":             strings.Replace(base, `"clock_inputs":1`, `"clock_inputs":2`, 1),
		"hps count":               strings.Replace(base, `"hps_general_purpose_interfaces":1`, `"hps_general_purpose_interfaces":2`, 1),
		"forbidden count":         strings.Replace(base, `"external_output_ports":0`, `"external_output_ports":1`, 1),
		"signed schema one field": strings.TrimSuffix(base, "\n") + `,"signature":"` + strings.Repeat("a", 128) + `"}` + "\n",
	}
	for name, raw := range mutations {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			if _, err := ParseResourceEvidenceV2([]byte(raw)); err == nil {
				t.Fatal("malformed resource evidence was accepted")
			}
		})
	}
}

func TestResourceEvidenceV2RejectsOversizedRecord(t *testing.T) {
	raw := validResourceEvidenceBytes(t)
	// Keep the object syntactically valid while exceeding the bounded wire size.
	padding := strings.Repeat("x", 2048)
	raw = bytes.Replace(raw, []byte(`"experiment":"020_linux_mailbox"`), []byte(`"experiment":"`+padding+`"`), 1)
	if len(raw) <= 2048 {
		t.Fatal("oversized fixture did not exceed the bound")
	}
	if _, err := ParseResourceEvidenceV2(raw); err == nil {
		t.Fatal("oversized resource evidence was accepted")
	}
}

func makeVerifiedBundleFixture(t *testing.T) (string, Manifest, ResourceEvidenceV2) {
	t.Helper()
	staging := t.TempDir()
	if err := os.Chmod(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	rbf := []byte("verified-rbf")
	rbfHash := sha256.Sum256(rbf)
	manifest := validManifest()
	manifest.ArtifactSize = uint64(len(rbf))
	manifest.ArtifactSHA256 = hex.EncodeToString(rbfHash[:])
	evidence := validResourceEvidenceV2()
	evidence.ArtifactSHA256 = manifest.ArtifactSHA256
	evidence.SourceCommit = manifest.SourceCommit
	manifestRaw, err := manifest.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	evidenceRaw, err := evidence.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"manifest.json":          manifestRaw,
		"resource_evidence.json": evidenceRaw,
		"top.rbf":                rbf,
	}
	var checksums strings.Builder
	for _, name := range requiredBundleMembers {
		hash := sha256.Sum256(files[name])
		checksums.WriteString(hex.EncodeToString(hash[:]))
		checksums.WriteString("  ")
		checksums.WriteString(name)
		checksums.WriteByte('\n')
	}
	files["bundle.sha256"] = []byte(checksums.String())
	for name, payload := range files {
		if err := os.WriteFile(filepath.Join(staging, name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return staging, manifest, evidence
}

func TestVerifyArtifactBundleChecksumsBeforeEvidenceParse(t *testing.T) {
	staging, _, _ := makeVerifiedBundleFixture(t)
	if _, err := VerifyArtifactBundle(staging, uint32(os.Getuid())); err != nil {
		t.Fatalf("VerifyArtifactBundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "resource_evidence.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyArtifactBundle(staging, uint32(os.Getuid())); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("tampered evidence error = %v, want checksum mismatch", err)
	}
}

func TestVerifyArtifactBundleBindsEvidenceToManifestAndRBF(t *testing.T) {
	staging, manifest, _ := makeVerifiedBundleFixture(t)
	bundle, err := VerifyArtifactBundle(staging, uint32(os.Getuid()))
	if err != nil {
		t.Fatalf("VerifyArtifactBundle: %v", err)
	}
	if bundle.Manifest != manifest || bundle.Evidence.ArtifactSHA256 != bundle.Artifact.SHA256 || bundle.Artifact.Size != int64(manifest.ArtifactSize) {
		t.Fatalf("bundle = %#v, want manifest/artifact tuple", bundle)
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), bytes.Replace(bundleManifestBytes(t, manifest), []byte(`"build_lane":"oss"`), []byte(`"build_lane":"oracle"`), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyArtifactBundle(staging, uint32(os.Getuid())); err == nil {
		t.Fatal("manifest mutation with stale checksum was accepted")
	}
}

func TestVerifyArtifactBundleHonorsDescriptorIdentityChecks(t *testing.T) {
	tests := map[string]func(*testing.T, string){
		"wrong owner": func(t *testing.T, staging string) {
			wrongUID := uint32(os.Getuid()) + 1
			if _, err := VerifyArtifactBundle(staging, wrongUID); err == nil {
				t.Fatal("bundle owned by another UID was accepted")
			}
		},
		"wrong directory mode": func(t *testing.T, staging string) {
			if err := os.Chmod(staging, 0o750); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyArtifactBundle(staging, uint32(os.Getuid())); err == nil {
				t.Fatal("bundle directory with non-private mode was accepted")
			}
		},
		"wrong mode": func(t *testing.T, staging string) {
			path := filepath.Join(staging, ManifestEvidence)
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyArtifactBundle(staging, uint32(os.Getuid())); err == nil {
				t.Fatal("bundle member with non-private mode was accepted")
			}
		},
		"multiple links": func(t *testing.T, staging string) {
			path := filepath.Join(staging, ManifestEvidence)
			link := filepath.Join(staging, "resource-evidence-link")
			if err := os.Link(path, link); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(link)
			if _, err := VerifyArtifactBundle(staging, uint32(os.Getuid())); err == nil {
				t.Fatal("bundle member with multiple links was accepted")
			}
		},
		"symlink member": func(t *testing.T, staging string) {
			path := filepath.Join(staging, ManifestEvidence)
			backup := path + ".original"
			if err := os.Rename(path, backup); err != nil {
				t.Fatal(err)
			}
			defer os.Rename(backup, path)
			if err := os.Symlink(filepath.Base(backup), path); err != nil {
				t.Fatal(err)
			}
			if _, err := VerifyArtifactBundle(staging, uint32(os.Getuid())); err == nil {
				t.Fatal("symlink bundle member was accepted")
			}
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			staging, _, _ := makeVerifiedBundleFixture(t)
			mutate(t, staging)
		})
	}
}

func TestVerifyArtifactBundleRejectsUnlistedDirectoryMembers(t *testing.T) {
	staging, _, _ := makeVerifiedBundleFixture(t)
	if err := os.WriteFile(filepath.Join(staging, "unexpected"), []byte("not in bundle.sha256"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyArtifactBundle(staging, uint32(os.Getuid())); err == nil {
		t.Fatal("bundle with an unlisted directory member was accepted")
	}
}

func TestArtifactBindingResourceEvidenceUsesRetainedDirectoryAfterWholeDirectorySwap(t *testing.T) {
	staging, manifest, originalEvidence := makeVerifiedBundleFixture(t)
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer binding.Close()

	replacement := filepath.Join(filepath.Dir(staging), "replacement-bundle")
	if err := os.Mkdir(replacement, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", ManifestArtifact, ManifestChecksums} {
		raw, err := os.ReadFile(filepath.Join(staging, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(replacement, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	replacementEvidence := originalEvidence
	replacementEvidence.SynthesisReportSHA256 = strings.Repeat("d", 64)
	replacementEvidenceRaw, err := replacementEvidence.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, ManifestEvidence), replacementEvidenceRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rewriteBundleChecksums(t, replacement); err != nil {
		t.Fatal(err)
	}

	moved := staging + ".original"
	if err := os.Rename(staging, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, staging); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(moved, staging)

	evidence, err := binding.ResourceEvidence()
	if err != nil {
		t.Fatalf("ResourceEvidence after whole-directory swap: %v", err)
	}
	if evidence.SynthesisReportSHA256 != originalEvidence.SynthesisReportSHA256 {
		t.Fatalf("evidence came from the replacement directory: %#v", evidence)
	}
}

func TestArtifactBindingResourceEvidenceRejectsRBFMemberReplacement(t *testing.T) {
	staging, manifest, _ := makeVerifiedBundleFixture(t)
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer binding.Close()

	original, err := os.ReadFile(filepath.Join(staging, ManifestArtifact))
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(staging, "top.rbf.replacement")
	if err := os.WriteFile(replacement, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, filepath.Join(staging, ManifestArtifact)); err != nil {
		t.Fatal(err)
	}
	if _, err := binding.ResourceEvidence(); err == nil {
		t.Fatal("RBF member replacement was accepted despite the retained descriptor")
	}
}

func rewriteBundleChecksums(t *testing.T, staging string) error {
	t.Helper()
	var checksums strings.Builder
	for _, name := range requiredBundleMembers {
		raw, err := os.ReadFile(filepath.Join(staging, name))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(raw)
		checksums.WriteString(hex.EncodeToString(sum[:]))
		checksums.WriteString("  ")
		checksums.WriteString(name)
		checksums.WriteByte('\n')
	}
	return os.WriteFile(filepath.Join(staging, ManifestChecksums), []byte(checksums.String()), 0o600)
}

func TestStaticPolicyConsumesOnlyVerifiedBundleEvidence(t *testing.T) {
	staging, manifest, _ := makeVerifiedBundleFixture(t)
	binding, err := testArtifactAccess().Bind(manifest, staging)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	defer binding.Close()
	proof, err := (staticPolicyVerifier{}).Verify(context.Background(), binding)
	if err != nil {
		t.Fatalf("static policy Verify: %v", err)
	}
	if proof.Evidence.ArtifactSHA256 != manifest.ArtifactSHA256 || !proof.ExactlyOneHPSGeneralPurpose || !proof.NoForbiddenResources {
		t.Fatalf("policy proof = %#v, want evidence-derived fixed policy", proof)
	}
}

func TestParseBundleSHA256RequiresExactSortedMembers(t *testing.T) {
	base := "" + strings.Repeat("a", 64) + "  manifest.json\n" +
		strings.Repeat("b", 64) + "  resource_evidence.json\n" +
		strings.Repeat("c", 64) + "  top.rbf\n"
	if got, err := ParseBundleSHA256([]byte(base)); err != nil || len(got) != 3 {
		t.Fatalf("ParseBundleSHA256 valid = %#v, %v", got, err)
	}
	mutations := map[string]string{
		"missing":    strings.Replace(base, strings.Repeat("b", 64)+"  resource_evidence.json\n", "", 1),
		"extra":      base + strings.Repeat("d", 64) + "  extra\n",
		"reorder":    strings.Replace(base, ""+strings.Repeat("a", 64)+"  manifest.json\n"+strings.Repeat("b", 64)+"  resource_evidence.json\n", ""+strings.Repeat("b", 64)+"  resource_evidence.json\n"+strings.Repeat("a", 64)+"  manifest.json\n", 1),
		"wrong name": strings.Replace(base, "  top.rbf\n", "  resource_evidence.json\n", 1),
		"bad hash":   strings.Replace(base, strings.Repeat("c", 64), "bad", 1),
		"no newline": strings.TrimSuffix(base, "\n"),
		"oversized":  base + strings.Repeat("x", maxBundleChecksumBytes),
	}
	for name, raw := range mutations {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			if _, err := ParseBundleSHA256([]byte(raw)); err == nil {
				t.Fatal("malformed bundle checksum was accepted")
			}
		})
	}
}

func bundleManifestBytes(t *testing.T, manifest Manifest) []byte {
	t.Helper()
	raw, err := manifest.MarshalCanonical()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
