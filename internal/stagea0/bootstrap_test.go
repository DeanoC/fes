package stagea0

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func validBootstrapTOML() []byte {
	return []byte(`format = 1
schema = "fogcast.stage-a0-bootstrap"
fogcast_base_revision = "0123456789abcdef0123456789abcdef01234567"

[main_upstream]
repository_id = "mister-devel-main-mister"
fetch_url = "https://github.com/MiSTer-devel/Main_MiSTer.git"
commit = "89abcdef0123456789abcdef0123456789abcdef"
tree = "fedcba9876543210fedcba9876543210fedcba98"

[branch]
name = "fogcast/stage-a-baseline"
parent_commit = "89abcdef0123456789abcdef0123456789abcdef"

[vdate]
source_path = "Makefile"
source_evidence_sha256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
official_expression = "%y%m%d"
format = "YYMMDD"
timezone = "UTC"
ascii_digits = 6

[patch]
recipe_version = "vdate-recipe-v1"

[initial_commit]
author_name = "FogCast"
author_email = "fogcast@example.invalid"
committer_name = "FogCast"
committer_email = "fogcast@example.invalid"
author_timestamp = 0
committer_timestamp = 1722470400
commit_message = "stage-a0: make VDATE reproducible\n"
signing = false
`)
}

func hasCode(err error, want Code) bool {
	var got *Failure
	return errors.As(err, &got) && got.Code == want
}

func TestParseBootstrapAcceptsClosedSchema(t *testing.T) {
	got, err := ParseBootstrap(validBootstrapTOML())
	if err != nil {
		t.Fatal(err)
	}
	if got.MainUpstream.Commit != "89abcdef0123456789abcdef0123456789abcdef" || got.Branch.ParentCommit != got.MainUpstream.Commit {
		t.Fatalf("parsed bootstrap = %#v", got)
	}
}

func TestParseBootstrapRejectsSchemaAndEvidenceViolations(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{"unknown-field", func(in []byte) []byte { return append(in, []byte("unknown = true\n")...) }},
		{"crlf", func(in []byte) []byte { return bytes.ReplaceAll(in, []byte("\n"), []byte("\r\n")) }},
		{"invalid-utf8", func(in []byte) []byte { return append(in, 0xff) }},
		{"abbreviated-fogcast-commit", replaceBootstrap("0123456789abcdef0123456789abcdef01234567", "0123456")},
		{"uppercase-upstream-commit", replaceBootstrap("89abcdef0123456789abcdef0123456789abcdef", "89ABCDEF0123456789abcdef0123456789abcdef")},
		{"abbreviated-tree", replaceBootstrap("fedcba9876543210fedcba9876543210fedcba98", "fedcba9")},
		{"uppercase-sha256", replaceBootstrap(strings.Repeat("a", 64), strings.Repeat("A", 64))},
		{"non-https-url", replaceBootstrap("https://github.com/MiSTer-devel/Main_MiSTer.git", "http://github.com/MiSTer-devel/Main_MiSTer.git")},
		{"url-userinfo", replaceBootstrap("https://github.com/MiSTer-devel/Main_MiSTer.git", "https://user@github.com/MiSTer-devel/Main_MiSTer.git")},
		{"url-query", replaceBootstrap("https://github.com/MiSTer-devel/Main_MiSTer.git", "https://github.com/MiSTer-devel/Main_MiSTer.git?x=1")},
		{"url-fragment", replaceBootstrap("https://github.com/MiSTer-devel/Main_MiSTer.git", "https://github.com/MiSTer-devel/Main_MiSTer.git#main")},
		{"url-default-port", replaceBootstrap("https://github.com/MiSTer-devel/Main_MiSTer.git", "https://github.com:443/MiSTer-devel/Main_MiSTer.git")},
		{"url-host-case", replaceBootstrap("https://github.com/MiSTer-devel/Main_MiSTer.git", "https://GitHub.com/MiSTer-devel/Main_MiSTer.git")},
		{"url-trailing-slash", replaceBootstrap("https://github.com/MiSTer-devel/Main_MiSTer.git", "https://github.com/MiSTer-devel/Main_MiSTer.git/")},
		{"control-byte", func(in []byte) []byte { return bytes.Replace(in, []byte("FogCast"), []byte("Fog\x01Cast"), 1) }},
		{"parent-mismatch", replaceBootstrap("parent_commit = \"89abcdef0123456789abcdef0123456789abcdef\"", "parent_commit = \"0123456789abcdef0123456789abcdef01234567\"")},
		{"branch", replaceBootstrap("fogcast/stage-a-baseline", "fogcast/other")},
		{"absolute-source-path", replaceBootstrap("source_path = \"Makefile\"", "source_path = \"/x\"")},
		{"parent-source-path", replaceBootstrap("source_path = \"Makefile\"", "source_path = \"a/../b\"")},
		{"dot-source-path", replaceBootstrap("source_path = \"Makefile\"", "source_path = \"./a\"")},
		{"double-slash-source-path", replaceBootstrap("source_path = \"Makefile\"", "source_path = \"a//b\"")},
		{"nul-source-path", replaceBootstrap("source_path = \"Makefile\"", "source_path = \"a\\u0000b\"")},
		{"recipe", replaceBootstrap("vdate-recipe-v1", "other")},
		{"expression", replaceBootstrap("%y%m%d", "%Y%m%d")},
		{"format", replaceBootstrap("YYMMDD", "YYYYMMDD")},
		{"timezone", replaceBootstrap("UTC", "GMT")},
		{"digit-count", replaceBootstrap("ascii_digits = 6", "ascii_digits = 8")},
		{"negative-timestamp", replaceBootstrap("author_timestamp = 0", "author_timestamp = -1")},
		{"signing", replaceBootstrap("signing = false", "signing = true")},
		{"message-without-lf", replaceBootstrap("stage-a0: make VDATE reproducible\\n", "stage-a0: make VDATE reproducible")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBootstrap(tc.edit(validBootstrapTOML()))
			if !hasCode(err, CodeBootstrapSchemaInvalid) {
				t.Fatalf("error code = %v", err)
			}
		})
	}
}

func TestParseBootstrapRejectsCRLF(t *testing.T) {
	_, err := ParseBootstrap(bytes.ReplaceAll(validBootstrapTOML(), []byte("\n"), []byte("\r\n")))
	if !hasCode(err, CodeBootstrapSchemaInvalid) {
		t.Fatalf("error code = %v", err)
	}
}

func TestRenderVDateUsesUTC(t *testing.T) {
	got, err := RenderVDate(1722470400)
	if err != nil || got != "240801" {
		t.Fatalf("RenderVDate() = %q, %v", got, err)
	}
}

func TestRenderVDateEpoch(t *testing.T) {
	got, err := RenderVDate(0)
	if err != nil || got != "700101" {
		t.Fatalf("RenderVDate() = %q, %v", got, err)
	}
}

func replaceBootstrap(old, new string) func([]byte) []byte {
	return func(in []byte) []byte { return bytes.Replace(in, []byte(old), []byte(new), 1) }
}
