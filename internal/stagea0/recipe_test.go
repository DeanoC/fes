package stagea0

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func validRecipeSource() []byte {
	return []byte("SHELL := /bin/bash\nDFLAGS = -O2 -DVDATE=\\\"`date +\"%y%m%d\"`\\\" -Wall\nprint:\n\t@printf '%s\\n' \"$(DFLAGS)\"\n")
}

func validVDateSpecFor(source []byte) VDateSpec {
	digest := sha256.Sum256(source)
	return VDateSpec{
		SourcePath:           "Makefile",
		SourceEvidenceSHA256: fmtHex(digest[:]),
		OfficialExpression:   "%y%m%d",
		Format:               "YYMMDD",
		Timezone:             "UTC",
		ASCIIDigits:          6,
	}
}

func TestApplyVDateRecipeV1PreservesDFLAGSBytes(t *testing.T) {
	source := validRecipeSource()
	spec := validVDateSpecFor(source)
	got, err := ApplyVDateRecipeV1(source, spec)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("SHELL := /bin/bash\n" + RecipeV1ValidationBlock + "DFLAGS = -O2 -DVDATE=\\\"$(VDATE)\\\" -Wall\nprint:\n\t@printf '%s\\n' \"$(DFLAGS)\"\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("recipe bytes = %q", got)
	}
	if !bytes.Contains(got, []byte("DFLAGS = -O2 -DVDATE=\\\"$(VDATE)\\\" -Wall\n")) {
		t.Fatalf("DFLAGS prefix or suffix changed: %q", got)
	}
}

func TestApplyVDateRecipeV1RejectsMismatchedOrUnsafeSource(t *testing.T) {
	valid := validRecipeSource()
	for _, tc := range []struct {
		name   string
		source []byte
		spec   VDateSpec
	}{
		{"sha-mismatch", valid, VDateSpec{}},
		{"two-full-tokens", bytes.Replace(valid, []byte(" -Wall"), []byte(" -DVDATE=\\\"`date +\"%y%m%d\"`\\\" -Wall"), 1), validVDateSpecFor(bytes.Replace(valid, []byte(" -Wall"), []byte(" -DVDATE=\\\"`date +\"%y%m%d\"`\\\" -Wall"), 1))},
		{"no-full-token", bytes.Replace(valid, []byte("`date +\"%y%m%d\"`"), []byte("`date +\"%Y%m%d\"`"), 1), validVDateSpecFor(bytes.Replace(valid, []byte("`date +\"%y%m%d\"`"), []byte("`date +\"%Y%m%d\"`"), 1))},
		{"crlf", bytes.ReplaceAll(valid, []byte("\n"), []byte("\r\n")), validVDateSpecFor(bytes.ReplaceAll(valid, []byte("\n"), []byte("\r\n")))},
		{"malformed-utf8", append(append([]byte(nil), valid...), 0xff), validVDateSpecFor(append(append([]byte(nil), valid...), 0xff))},
		{"no-terminal-lf", valid[:len(valid)-1], validVDateSpecFor(valid[:len(valid)-1])},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "sha-mismatch" {
				tc.spec = validVDateSpecFor(valid)
				tc.spec.SourceEvidenceSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			}
			_, err := ApplyVDateRecipeV1(tc.source, tc.spec)
			if !hasCode(err, CodeVDateRecipeMismatch) {
				t.Fatalf("error code = %v", err)
			}
		})
	}
}

func TestRecipeV1MakeInputValidation(t *testing.T) {
	makefile := filepath.Join(t.TempDir(), "Makefile")
	source := validRecipeSource()
	patched, err := ApplyVDateRecipeV1(source, validVDateSpecFor(source))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(makefile, patched, 0o600); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "must-not-exist")
	for _, tc := range []struct {
		name      string
		args, env []string
		want      string
		ok        bool
	}{
		{"missing", nil, nil, "VDATE must be supplied as six ASCII YYMMDD digits", false},
		{"environment-only", nil, []string{"VDATE=240801"}, "VDATE must be supplied as six ASCII YYMMDD digits", false},
		{"invalid-command-line", []string{"VDATE=2408AA"}, nil, "VDATE must be supplied as six ASCII YYMMDD digits", false},
		{"hostile-command-line", []string{"VDATE=240801'; touch " + sentinel + "; echo '"}, nil, "VDATE must be supplied as six ASCII YYMMDD digits", false},
		{"combined-command-line-overrides", []string{"VDATE=240801", "VDATE_VALID=valid", "STAGE_A0_VDATE=2408AA", "stage_a0_shell_quote=bad"}, nil, `-DVDATE="240801"`, true},
		{"hostile-helper-override", []string{"VDATE=240801", "stage_a0_shell_quote=$(shell touch " + sentinel + ")"}, nil, `-DVDATE="240801"`, true},
		{"harmless-makeflags", []string{"VDATE=240801"}, []string{"MAKEFLAGS=--no-builtin-rules"}, `-DVDATE="240801"`, true},
		{"valid-command-line", []string{"VDATE=240801"}, nil, `-DVDATE="240801"`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("make", append([]string{"-f", makefile, "print"}, tc.args...)...)
			cmd.Env = append([]string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "LC_ALL=C", "LANG=C", "TZ=UTC"}, tc.env...)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok || !bytes.Contains(out, []byte(tc.want)) || (tc.ok && bytes.Contains(out, []byte(`\"`))) {
				t.Fatalf("out=%q err=%v", out, err)
			}
			if _, err := os.Stat(sentinel); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("hostile VDATE executed: %v", err)
			}
		})
	}
}

func fmtHex(in []byte) string {
	const alphabet = "0123456789abcdef"
	out := make([]byte, len(in)*2)
	for i, b := range in {
		out[i*2] = alphabet[b>>4]
		out[i*2+1] = alphabet[b&0x0f]
	}
	return string(out)
}
