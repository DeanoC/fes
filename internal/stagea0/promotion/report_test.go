package promotion

import (
	"bytes"
	"strings"
	"testing"
)

func TestEvaluateBlockedCandidateLockProducesStableBlockers(t *testing.T) {
	report := Evaluate(Inputs{Lock: []byte("format = 1\n")})
	if report.Decision != DecisionBlocked {
		t.Fatalf("decision = %q, want %q", report.Decision, DecisionBlocked)
	}
	if report.EvidenceStatus != "Software-tested" || report.SourceAvailability != "local-only" {
		t.Fatalf("evidence = %q/%q, want Software-tested/local-only", report.EvidenceStatus, report.SourceAvailability)
	}
	if len(report.Blockers) == 0 || report.Blockers[0] != "LOCK_SCHEMA_INVALID" {
		t.Fatalf("blockers = %#v, want LOCK_SCHEMA_INVALID first", report.Blockers)
	}
	lockCheck, ok := findCheck(report.Checks, "lock-schema")
	if !ok || lockCheck.Status != CheckBlocked {
		t.Fatalf("checks = %#v, want blocked lock-schema", report.Checks)
	}
	raw, err := EncodeReport(report)
	if err != nil {
		t.Fatalf("EncodeReport() error = %v", err)
	}
	decoded, err := DecodeReport(raw)
	if err != nil {
		t.Fatalf("DecodeReport() error = %v", err)
	}
	if !bytes.Equal(raw, mustEncodeReport(t, decoded)) {
		t.Fatal("report encoding is not stable after decode")
	}
}

func TestEvaluateSortsChecksAndBlockers(t *testing.T) {
	report := Evaluate(Inputs{
		Lock:       []byte("not-a-lock"),
		Materials:  []byte("not-materials"),
		Comparison: []byte("not-comparison"),
		Policies:   map[string][]byte{"compile-link.json": []byte("not-policy")},
	})
	if !sortChecks(report.Checks) {
		t.Fatalf("checks are not sorted: %#v", report.Checks)
	}
	for i := 1; i < len(report.Blockers); i++ {
		if report.Blockers[i-1] >= report.Blockers[i] {
			t.Fatalf("blockers are not strictly sorted: %#v", report.Blockers)
		}
	}
}

func TestDecodeReportRejectsNonCanonicalJSON(t *testing.T) {
	if _, err := DecodeReport([]byte("{}\n{}\n")); err == nil || !strings.Contains(err.Error(), "trailing data") {
		t.Fatalf("DecodeReport() error = %v, want trailing-data error", err)
	}
}

func TestValidateReportRejectsForgedEvidenceAndUnknownChecks(t *testing.T) {
	report := Evaluate(Inputs{Lock: []byte("not-a-lock")})
	report.EvidenceStatus = "Reproducible"
	if err := ValidateReport(report); err == nil {
		t.Fatal("ValidateReport accepted blocked reproducible evidence")
	}
	report = Evaluate(Inputs{Lock: []byte("not-a-lock")})
	report.Checks[0].Status = CheckPass
	for i := range report.Checks {
		report.Checks[i].Status = CheckPass
	}
	if err := ValidateReport(report); err == nil {
		t.Fatal("ValidateReport accepted blocked report without a failed check")
	}
	report = Evaluate(Inputs{Lock: []byte("not-a-lock")})
	report.Checks[0].ID = "forged-check"
	if err := ValidateReport(report); err == nil {
		t.Fatal("ValidateReport accepted unknown check ID")
	}
}

func sortChecks(checks []Check) bool {
	for i := 1; i < len(checks); i++ {
		if checks[i-1].ID >= checks[i].ID {
			return false
		}
	}
	return true
}

func findCheck(checks []Check, id string) (Check, bool) {
	for _, check := range checks {
		if check.ID == id {
			return check, true
		}
	}
	return Check{}, false
}

func mustEncodeReport(t *testing.T, report Report) []byte {
	t.Helper()
	raw, err := EncodeReport(report)
	if err != nil {
		t.Fatalf("EncodeReport() error = %v", err)
	}
	return raw
}
