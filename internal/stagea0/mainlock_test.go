package stagea0

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func assertMainLockCode(t *testing.T, raw string, want Code) {
	t.Helper()
	_, err := ParseMainLock([]byte(raw))
	if !hasCode(err, want) {
		t.Fatalf("code = %v, want %v; err=%v", codeOf(err), want, err)
	}
}

const validMainLock = `format = 1
schema = "fogcast.stage-a0-main-lock"
fogcast_base_revision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
source_date_epoch = 1

[environment]
locale = "C"
timezone = "UTC"
umask = "022"
job_count = 1
network = "disabled-during-build"
path_policy = ["/stage-a0/build-utils/bin", "/stage-a0/toolchain/bin"]
container_material_id = "oci"

[main]
upstream_material_id = "upstream"
fork_material_id = "fork"
upstream_commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
upstream_tree = "cccccccccccccccccccccccccccccccccccccccc"
fork_commit = "dddddddddddddddddddddddddddddddddddddddd"
fork_tree = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
fork_parent_commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
patch_commits = ["dddddddddddddddddddddddddddddddddddddddd"]
publication_status = "local-only"

[build]
entrypoint = ["/stage-a0/build-utils/bin/bash", "build.sh"]
working_directory = "/stage-a0/src"
vdate_format = "YYMMDD"
vdate_expression = "%y%m%d"
allowed_final_artifacts = ["bin/MiSTer", "bin/MiSTer.elf"]

[[materials]]
id = "archive"
role = "consumed-build-input"
kind = "archive-https"
license_ids = ["archive-license"]
[materials.archive_https]
url = "https://example.com/archive.tar"
size = 1
sha256 = "1111111111111111111111111111111111111111111111111111111111111111"

[[materials]]
id = "file"
role = "consumed-build-input"
kind = "material-file"
license_ids = ["file-license"]
[materials.material_file]
parent_id = "archive"
path = "policy/source-set.toml"
size = 1
sha256 = "2222222222222222222222222222222222222222222222222222222222222222"

[[materials]]
id = "fork"
role = "consumed-build-input"
kind = "git-local"
license_ids = ["fork-license"]
[materials.git_local]
repository_id = "main"
commit = "dddddddddddddddddddddddddddddddddddddddd"
tree = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

[[materials]]
id = "oci"
role = "consumed-build-input"
kind = "oci"
license_ids = ["oci-license"]
[materials.oci]
reference = "registry.example.com/build/image@sha256:3333333333333333333333333333333333333333333333333333333333333333"
manifest_digest = "sha256:3333333333333333333333333333333333333333333333333333333333333333"
config_digest = "sha256:4444444444444444444444444444444444444444444444444444444444444444"
os = "linux"
architecture = "amd64"

[[materials]]
id = "subtree"
role = "consumed-build-input"
kind = "git-subtree"
license_ids = ["subtree-license"]
[materials.git_subtree]
parent_id = "fork"
path = "lib/miniz"
tree = "5555555555555555555555555555555555555555"

[[materials]]
id = "upstream"
role = "consumed-build-input"
kind = "git-https"
license_ids = ["upstream-license"]
[materials.git_https]
repository_id = "main"
url = "https://example.com/main"
commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
tree = "cccccccccccccccccccccccccccccccccccccccc"

[[toolchains]]
id = "native"
target_triple = "x86_64-linux-gnu"
`

const validMainLockTail = `
[[toolchains.components]]
role = "archiver"
material_id = "fork"
logical_path = "/stage-a0/toolchain/bin/ar"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[toolchains.components]]
role = "assembler"
material_id = "fork"
logical_path = "/stage-a0/toolchain/bin/as"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[toolchains.components]]
role = "binutils"
material_id = "fork"
logical_path = "/stage-a0/toolchain"
[[toolchains.components]]
role = "compiler"
material_id = "fork"
logical_path = "/stage-a0/toolchain/bin/cc"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[toolchains.components]]
role = "libc"
material_id = "fork"
logical_path = "/stage-a0/sysroot"
[[toolchains.components]]
role = "linker"
material_id = "fork"
logical_path = "/stage-a0/toolchain/bin/ld"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[toolchains.components]]
role = "objcopy"
material_id = "fork"
logical_path = "/stage-a0/toolchain/bin/objcopy"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[toolchains.components]]
role = "objdump"
material_id = "fork"
logical_path = "/stage-a0/toolchain/bin/objdump"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[toolchains.components]]
role = "readelf"
material_id = "fork"
logical_path = "/stage-a0/toolchain/bin/readelf"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[toolchains.components]]
role = "strip"
material_id = "fork"
logical_path = "/stage-a0/toolchain/bin/strip"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[toolchains.components]]
role = "sysroot"
material_id = "fork"
logical_path = "/stage-a0/sysroot"

[[build_utilities]]
role = "bash"
material_id = "fork"
logical_path = "/stage-a0/build-utils/bin/bash"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[build_utilities]]
role = "cp"
material_id = "fork"
logical_path = "/stage-a0/build-utils/bin/cp"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[build_utilities]]
role = "git"
material_id = "fork"
logical_path = "/stage-a0/build-utils/bin/git"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[build_utilities]]
role = "make"
material_id = "fork"
logical_path = "/stage-a0/build-utils/bin/make"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[build_utilities]]
role = "mkdir"
material_id = "fork"
logical_path = "/stage-a0/build-utils/bin/mkdir"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[build_utilities]]
role = "nproc-shim"
material_id = "fork"
logical_path = "/stage-a0/build-utils/bin/nproc"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[build_utilities]]
role = "rm"
material_id = "fork"
logical_path = "/stage-a0/build-utils/bin/rm"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
[[build_utilities]]
role = "sed"
material_id = "fork"
logical_path = "/stage-a0/build-utils/bin/sed"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"

[[configs]]
id = "config"
material_id = "fork"
path = "config/main.cfg"
sha256 = "7777777777777777777777777777777777777777777777777777777777777777"
purpose = "build"

[[policies]]
id = "compile"
material_id = "file"
path = "policy/source-set.toml"
sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
kind = "compile-link"
[[policies]]
id = "delta"
material_id = "file"
path = "policy/source-set.toml"
sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
kind = "upstream-fork-delta"
[[policies]]
id = "elf"
material_id = "file"
path = "policy/source-set.toml"
sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
kind = "elf-dependency"
[[policies]]
id = "generated"
material_id = "file"
path = "policy/source-set.toml"
sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
kind = "generated-input"
[[policies]]
id = "intermediate"
material_id = "file"
path = "policy/source-set.toml"
sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
kind = "intermediate-path"
[[policies]]
id = "source"
material_id = "file"
path = "policy/source-set.toml"
sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
kind = "source-set"
`

func fullMainLock() string {
	var b strings.Builder
	b.WriteString(validMainLock)
	b.WriteString(validMainLockTail)
	for _, id := range []string{"archive", "file", "fork", "oci", "subtree", "upstream"} {
		b.WriteString("[[licenses]]\n")
		b.WriteString("id = \"")
		b.WriteString(id)
		b.WriteString("-license\"\nmaterial_id = \"")
		b.WriteString(id)
		b.WriteString("\"\nspdx_expression = \"MIT OR Apache-2.0 WITH LLVM-exception\"\nnotice_locator = \"LICENSE\"\ncorresponding_source_locator = \"SOURCE\"\nredistribution_status = \"redistributable\"\n")
	}
	return b.String()
}

func TestParseMainLockCanonicalWire(t *testing.T) {
	lock, err := ParseMainLock([]byte(fullMainLock()))
	if err != nil {
		t.Fatalf("ParseMainLock() error = %v", err)
	}
	if len(lock.Materials) != 6 || len(lock.Policies) != 6 || len(lock.Toolchains) != 1 {
		t.Fatalf("parsed closure is incomplete: %+v", lock)
	}
}

func TestParseMainLockRejectsClosedSchema(t *testing.T) {
	for _, hostile := range []string{"", "\ufeff" + fullMainLock(), strings.Replace(fullMainLock(), "\n", "\r\n", 1), strings.Replace(fullMainLock(), "format = 1", "format = 1\nunknown = 1", 1), strings.TrimSuffix(fullMainLock(), "\n")} {
		_, err := ParseMainLock([]byte(hostile))
		if !hasCode(err, CodeLockSchemaInvalid) {
			t.Fatalf("code = %v, want %v", codeOf(err), CodeLockSchemaInvalid)
		}
	}
}

func TestValidateMainLockFormats(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		code      Code
	}{
		{"mutable URL", strings.Replace(fullMainLock(), "https://example.com/main", "http://example.com/main", 1), CodeLockMutableIdentity},
		{"bad digest", strings.Replace(fullMainLock(), "sha256 = \"111", "sha256 = \"ABC", 1), CodeLockSchemaInvalid},
		{"unsafe path", strings.Replace(fullMainLock(), "policy/source-set.toml", "policy/../source-set.toml", 1), CodeLockSchemaInvalid},
		{"extra union", fullMainLock() + "[materials.git_local]\nrepository_id = \"x\"\ncommit = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\ntree = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n", CodeLockSchemaInvalid},
		{"missing scalar", strings.Replace(fullMainLock(), "notice_locator = \"LICENSE\"\n", "", 1), CodeLockSchemaInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseMainLock([]byte(tc.raw))
			if !hasCode(err, tc.code) {
				t.Fatalf("code = %v, want %v; err=%v", codeOf(err), tc.code, err)
			}
		})
	}
}

func TestValidateMainLockOrderAndReferences(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"unsorted material", strings.Replace(fullMainLock(), "id = \"archive\"", "id = \"zarchive\"", 1)},
		{"forward parent", strings.Replace(fullMainLock(), "parent_id = \"archive\"", "parent_id = \"upstream\"", 1)},
		{"wrong fork parent", strings.Replace(fullMainLock(), "fork_parent_commit = \"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\"", "fork_parent_commit = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"", 1)},
		{"policy material mismatch", strings.Replace(fullMainLock(), "material_id = \"file\"\npath = \"policy/source-set.toml\"", "material_id = \"fork\"\npath = \"policy/source-set.toml\"", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseMainLock([]byte(tc.raw))
			if !hasCode(err, CodeLockSchemaInvalid) {
				t.Fatalf("code = %v, want %v", codeOf(err), CodeLockSchemaInvalid)
			}
		})
	}
}

func TestValidateMainLockMaterialUnion(t *testing.T) {
	_, err := ParseMainLock([]byte(fullMainLock() + "[materials.git_local]\nrepository_id = \"x\"\ncommit = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\ntree = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n"))
	if !hasCode(err, CodeLockSchemaInvalid) {
		t.Fatalf("code = %v", codeOf(err))
	}
}

func TestValidateMainLockEnvironmentAndBuild(t *testing.T) {
	for _, raw := range []string{
		strings.Replace(fullMainLock(), "locale = \"C\"", "locale = \"en_US.UTF-8\"", 1),
		strings.Replace(fullMainLock(), "entrypoint = [\"/stage-a0/build-utils/bin/bash\", \"build.sh\"]", "entrypoint = [\"/bin/bash\"]", 1),
	} {
		_, err := ParseMainLock([]byte(raw))
		if !hasCode(err, CodeLockSchemaInvalid) {
			t.Fatalf("code = %v", codeOf(err))
		}
	}
}

func TestValidateMainLockToolRoles(t *testing.T) {
	badRoot := strings.Replace(fullMainLock(), "role = \"binutils\"\nmaterial_id = \"fork\"\nlogical_path = \"/stage-a0/toolchain\"", "role = \"binutils\"\nmaterial_id = \"fork\"\nlogical_path = \"/stage-a0/toolchain\"\nexecutable_sha256 = \"6666666666666666666666666666666666666666666666666666666666666666\"", 1)
	_, err := ParseMainLock([]byte(badRoot))
	if !hasCode(err, CodeLockSchemaInvalid) {
		t.Fatalf("code = %v", codeOf(err))
	}
}

func TestValidateMainLockPolicies(t *testing.T) {
	_, err := ParseMainLock([]byte(strings.Replace(fullMainLock(), "kind = \"source-set\"", "kind = \"compile-link\"", 1)))
	if !hasCode(err, CodeLockSchemaInvalid) {
		t.Fatalf("code = %v", codeOf(err))
	}
}

func TestValidateMainLockPublication(t *testing.T) {
	_, err := ParseMainLock([]byte(strings.Replace(fullMainLock(), "publication_status = \"local-only\"", "publication_status = \"durably-retrievable\"", 1)))
	if !hasCode(err, CodeLockSchemaInvalid) {
		t.Fatalf("code = %v", codeOf(err))
	}
}

func TestParseMainLockPresenceBoundaries(t *testing.T) {
	assertMainLockCode(t, strings.Replace(fullMainLock(), "publication_status = \"local-only\"", "publication_status = \"local-only\"\ndurable_retrieval_material_id = \"\"", 1), CodeLockSchemaInvalid)
	assertMainLockCode(t, strings.Replace(fullMainLock(), "license_ids = [\"archive-license\"]", "license_ids = [\"archive-license\"]\npurpose = \"\"", 1), CodeLockSchemaInvalid)
	assertMainLockCode(t, strings.Replace(fullMainLock(), "job_count = 1", "job_count = 0", 1), CodeLockSchemaInvalid)
	assertMainLockCode(t, strings.Replace(fullMainLock(), "size = 1", "size = -1", 1), CodeLockSchemaInvalid)
}

func TestValidateMainLockSPDXGrammarAndDepth(t *testing.T) {
	for _, expression := range []string{
		"(MIT OR Apache-2.0) WITH LLVM-exception",
		"MIT AND (Apache-2.0 OR)",
		"DocumentRef-x:LicenseRef-",
		"MIT  OR Apache-2.0",
		strings.Repeat("(", 65) + "MIT" + strings.Repeat(")", 65),
	} {
		raw := strings.Replace(fullMainLock(), "MIT OR Apache-2.0 WITH LLVM-exception", expression, 1)
		assertMainLockCode(t, raw, CodeLicenseRecordIncomplete)
	}
	atLimit := strings.Repeat("(", 64) + "MIT" + strings.Repeat(")", 64)
	if _, err := ParseMainLock([]byte(strings.Replace(fullMainLock(), "MIT OR Apache-2.0 WITH LLVM-exception", atLimit, 1))); err != nil {
		t.Fatalf("SPDX depth limit rejected: %v", err)
	}
}

func TestValidateMainLockSPDXByteBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
		code Code
	}{
		{"exact limit", 4096, ""},
		{"one over", 4097, CodeLicenseRecordIncomplete},
	} {
		raw := strings.Replace(fullMainLock(), "MIT OR Apache-2.0 WITH LLVM-exception", strings.Repeat("A", tc.size), 1)
		if tc.code == "" {
			if _, err := ParseMainLock([]byte(raw)); err != nil {
				t.Fatalf("%s rejected: %v", tc.name, err)
			}
		} else {
			assertMainLockCode(t, raw, tc.code)
		}
	}
}

func TestValidateMainLockOCIFailurePrecedence(t *testing.T) {
	base := fullMainLock()
	for _, tc := range []struct {
		from, to string
		code     Code
	}{
		{"build/image@sha256:", "build/image:latest@sha256:", CodeLockMutableIdentity},
		{"build/image@sha256:333", "build/image:latest", CodeLockMutableIdentity},
		{"registry.example.com/build", "registry.example.com:5000/build", CodeLockSchemaInvalid},
		{"registry.example.com/build", "Registry.example.com/build", CodeLockSchemaInvalid},
		{"build/image@", "build/Bad@", CodeLockSchemaInvalid},
		{"config_digest = \"sha256:444", "config_digest = \"sha512:444", CodeLockSchemaInvalid},
		{"os = \"linux\"", "os = \"darwin\"", CodeLockSchemaInvalid},
		{"architecture = \"amd64\"", "architecture = \"arm64\"", CodeLockSchemaInvalid},
	} {
		assertMainLockCode(t, strings.Replace(base, tc.from, tc.to, 1), tc.code)
	}
}

func TestParseMainLockRawAndStringBoundaries(t *testing.T) {
	base := fullMainLock()
	padding := (8 << 20) - len(base)
	atLimit := "#" + strings.Repeat("x", padding-2) + "\n" + base
	if len(atLimit) != 8<<20 {
		t.Fatalf("fixture size = %d", len(atLimit))
	}
	if _, err := ParseMainLock([]byte(atLimit)); err != nil {
		t.Fatalf("raw size limit rejected: %v", err)
	}
	for _, raw := range [][]byte{
		append([]byte{0xff}, []byte(base)...),
		[]byte(strings.Replace(base, "schema =", "# \ufeff\nschema =", 1)),
		[]byte(strings.Replace(base, "\n", "\r\n", 1)),
		append([]byte(base), 0),
		append([]byte(base), 0x7f),
		append([]byte(base), 1),
		[]byte(strings.TrimSuffix(base, "\n")),
		[]byte("#x\n" + atLimit),
	} {
		_, err := ParseMainLock(raw)
		if !hasCode(err, CodeLockSchemaInvalid) {
			t.Fatalf("code = %v, want schema", codeOf(err))
		}
	}
	for _, tc := range []struct{ from, to string }{
		{"purpose = \"build\"", "purpose = \"bad\\u0000value\""},
		{"purpose = \"build\"", "purpose = \"bad\\nvalue\""},
		{"target_triple = \"x86_64-linux-gnu\"", "target_triple = \"bad\\ntriple\""},
		{"repository_id = \"main\"", "repository_id = \"bad\\u0000repo\""},
		{"version = \"1\"", "version = \"bad\\nversion\""},
		{"license_ids = [\"archive-license\"]", "license_ids = [\"bad\\u0000license\"]"},
		{"license_ids = [\"archive-license\"]", "license_ids = [\"archive-license\"]\npurpose = \"bad\\nmaterial\""},
	} {
		raw := strings.Replace(base, tc.from, tc.to, 1)
		assertMainLockCode(t, raw, CodeLockSchemaInvalid)
	}
}

func TestParseMainLockDecodedUnicodeControls(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
	}{
		{"BOM purpose", "purpose = \"build\"", "purpose = \"\\uFEFF\""},
		{"C1 purpose", "purpose = \"build\"", "purpose = \"\\u0085\""},
		{"BOM repository", "repository_id = \"main\"", "repository_id = \"\\uFEFF\""},
		{"C1 target", "target_triple = \"x86_64-linux-gnu\"", "target_triple = \"x86_64\\u0085linux-gnu\""},
		{"BOM version", "version = \"1\"", "version = \"1\\uFEFF\""},
		{"C1 entry argument", "\"build.sh\"", "\"build\\u0085.sh\""},
	} {
		assertMainLockCode(t, strings.Replace(fullMainLock(), tc.from, tc.to, 1), CodeLockSchemaInvalid)
	}
}

func TestValidateMainLockLicenseBacklinkPrecedence(t *testing.T) {
	base := fullMainLock()
	assertMainLockCode(t, strings.Replace(base, "id = \"archive-license\"\nmaterial_id = \"archive\"", "id = \"other-license\"\nmaterial_id = \"archive\"", 1), CodeLicenseRecordIncomplete)
	assertMainLockCode(t, strings.Replace(base, "id = \"archive-license\"\nmaterial_id = \"archive\"", "id = \"archive-license\"\nmaterial_id = \"file\"", 1), CodeLicenseRecordIncomplete)
	start := strings.Index(base, "[[licenses]]\nid = \"archive-license\"")
	end := strings.Index(base[start+1:], "[[licenses]]") + start + 1
	assertMainLockCode(t, base[:start]+base[end:], CodeLicenseRecordIncomplete)
}

func TestParseMainLockClosedSchemaMatrix(t *testing.T) {
	base := fullMainLock()
	for _, tc := range []struct {
		name, raw string
	}{
		{"duplicate scalar", strings.Replace(base, "format = 1", "format = 1\nformat = 1", 1)},
		{"wrong scalar type", strings.Replace(base, "format = 1", "format = \"1\"", 1)},
		{"missing top scalar", strings.Replace(base, "format = 1\n", "", 1)},
		{"missing nested scalar", strings.Replace(base, "locale = \"C\"\n", "", 1)},
		{"missing array", strings.Replace(base, "patch_commits = [\"dddddddddddddddddddddddddddddddddddddddd\"]\n", "", 1)},
		{"wrong array type", strings.Replace(base, "patch_commits = [\"dddddddddddddddddddddddddddddddddddddddd\"]", "patch_commits = \"dddddddddddddddddddddddddddddddddddddddd\"", 1)},
		{"unknown nested scalar", strings.Replace(base, "locale = \"C\"", "locale = \"C\"\nunknown = true", 1)},
		{"empty revision", strings.Replace(base, "fogcast_base_revision = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"", "fogcast_base_revision = \"\"", 1)},
		{"invalid material role", strings.Replace(base, "role = \"consumed-build-input\"", "role = \"input\"", 1)},
		{"invalid material kind", strings.Replace(base, "kind = \"archive-https\"", "kind = \"git-local\"", 1)},
		{"duplicate license id", strings.Replace(base, "license_ids = [\"archive-license\"]", "license_ids = [\"archive-license\", \"archive-license\"]", 1)},
		{"bad SHA case", strings.Replace(base, "sha256 = \"111", "sha256 = \"A11", 1)},
		{"URL trailing slash", strings.Replace(base, "https://example.com/main", "https://example.com/main/", 1)},
		{"unsafe absolute material path", strings.Replace(base, "path = \"lib/miniz\"", "path = \"/lib/miniz\"", 1)},
		{"missing component role", strings.Replace(base, "role = \"archiver\"\n", "", 1)},
		{"missing executable hash", strings.Replace(base, "executable_sha256 = \"6666666666666666666666666666666666666666666666666666666666666666\"\n", "", 1)},
		{"duplicate tool role", strings.Replace(base, "role = \"assembler\"", "role = \"archiver\"", 1)},
		{"missing baseline utility", strings.Replace(base, "role = \"nproc-shim\"", "role = \"nproc-extra\"", 1)},
		{"empty config id", strings.Replace(base, "id = \"config\"", "id = \"\"", 1)},
		{"policy hash mismatch", strings.Replace(base, "sha256 = \"2222222222222222222222222222222222222222222222222222222222222222\"\nkind = \"compile-link\"", "sha256 = \"9999999999999999999999999999999999999999999999999999999999999999\"\nkind = \"compile-link\"", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) { assertMainLockCode(t, tc.raw, CodeLockSchemaInvalid) })
	}
	for _, raw := range []string{
		strings.Replace(base, "source_date_epoch = 1", "source_date_epoch = 0", 1),
		strings.Replace(base, "size = 1", "size = 0", 1),
	} {
		if _, err := ParseMainLock([]byte(raw)); err != nil {
			t.Fatalf("valid zero rejected: %v", err)
		}
	}
	assertMainLockCode(t, strings.Replace(base, "https://example.com/main", "https://example.com/main?q=x", 1), CodeLockMutableIdentity)
	assertMainLockCode(t, strings.Replace(base, "https://example.com/main", "https://example.com/main@part", 1), CodeLockSchemaInvalid)
	assertMainLockCode(t, strings.Replace(base, "https://example.com/main", "https://example.com/main?", 1), CodeLockMutableIdentity)
	extraComponent := `[[toolchains.components]]
role = "custom-tool"
material_id = "fork"
logical_path = "/stage-a0/toolchain/bin/custom-tool"
executable_sha256 = "6666666666666666666666666666666666666666666666666666666666666666"
version = "1"
`
	withExtra := strings.Replace(base, "[[toolchains.components]]\nrole = \"libc\"", extraComponent+"[[toolchains.components]]\nrole = \"libc\"", 1)
	if _, err := ParseMainLock([]byte(withExtra)); err != nil {
		t.Fatalf("traced extra tool rejected: %v", err)
	}
}

func TestParseMainLockRequiredTablesAndTypes(t *testing.T) {
	base := fullMainLock()
	for _, field := range []string{
		"format = 1\n", "schema = \"fogcast.stage-a0-main-lock\"\n", "fogcast_base_revision = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"\n", "source_date_epoch = 1\n",
		"[environment]\n", "[main]\n", "[build]\n", "[[materials]]\n", "[materials.archive_https]\n", "[[toolchains]]\n", "[[toolchains.components]]\n", "[[build_utilities]]\n", "[[configs]]\n", "[[policies]]\n", "[[licenses]]\n",
	} {
		assertMainLockCode(t, strings.Replace(base, field, "", 1), CodeLockSchemaInvalid)
	}
	for _, raw := range []string{
		strings.Replace(base, "job_count = 1", "job_count = \"1\"", 1),
		strings.Replace(base, "path_policy = [\"/stage-a0/build-utils/bin\", \"/stage-a0/toolchain/bin\"]", "path_policy = \"/stage-a0/build-utils/bin\"", 1),
		strings.Replace(base, "entrypoint = [\"/stage-a0/build-utils/bin/bash\", \"build.sh\"]", "entrypoint = []", 1),
		strings.Replace(base, "material_id = \"fork\"\npath = \"config/main.cfg\"", "material_id = \"missing\"\npath = \"config/main.cfg\"", 1),
		strings.Replace(base, "container_material_id = \"oci\"", "container_material_id = \"missing\"", 1),
		strings.Replace(base, "patch_commits = [\"dddddddddddddddddddddddddddddddddddddddd\"]", "patch_commits = [\"dddddddddddddddddddddddddddddddddddddddd\", \"dddddddddddddddddddddddddddddddddddddddd\"]", 1),
		strings.Replace(base, "patch_commits = [\"dddddddddddddddddddddddddddddddddddddddd\"]", "patch_commits = [\"dddddddddddddddddddddddddddddddddddddddd\", \"cccccccccccccccccccccccccccccccccccccccc\"]", 1),
	} {
		assertMainLockCode(t, raw, CodeLockSchemaInvalid)
	}
	assertMainLockCode(t, base+"[environment]\nlocale = \"C\"\n", CodeLockSchemaInvalid)
	assertMainLockCode(t, base+"[[licenses]]\n", CodeLockSchemaInvalid)
}

func TestValidateMainLockOrderingAndCardinalityMatrix(t *testing.T) {
	base := fullMainLock()
	rows := []struct {
		name, raw string
	}{
		{"duplicate toolchain", base + "[[toolchains]]\nid = \"native\"\ntarget_triple = \"x86_64-linux-gnu\"\n"},
		{"duplicate utility", strings.Replace(base, "role = \"make\"", "role = \"bash\"", 1)},
		{"duplicate config", base + "[[configs]]\nid = \"config\"\nmaterial_id = \"fork\"\npath = \"config/main.cfg\"\nsha256 = \"7777777777777777777777777777777777777777777777777777777777777777\"\npurpose = \"build\"\n"},
		{"duplicate policy kind", strings.Replace(base, "kind = \"source-set\"", "kind = \"compile-link\"", 1)},
		{"duplicate license", base + "[[licenses]]\nid = \"archive-license\"\nmaterial_id = \"archive\"\nspdx_expression = \"MIT\"\nnotice_locator = \"LICENSE\"\ncorresponding_source_locator = \"SOURCE\"\nredistribution_status = \"redistributable\"\n"},
		{"missing policy kind", strings.Replace(base, "kind = \"source-set\"", "kind = \"missing\"", 1)},
		{"unresolved utility material", strings.Replace(base, "role = \"bash\"\nmaterial_id = \"fork\"", "role = \"bash\"\nmaterial_id = \"missing\"", 1)},
		{"unresolved license material", strings.Replace(base, "id = \"archive-license\"\nmaterial_id = \"archive\"", "id = \"archive-license\"\nmaterial_id = \"missing\"", 1)},
		{"fork parent mismatch", strings.Replace(base, "fork_parent_commit = \"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\"", "fork_parent_commit = \"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"", 1)},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) { assertMainLockCode(t, row.raw, CodeLockSchemaInvalid) })
	}
}

func TestValidateMainLockPublicationMatrix(t *testing.T) {
	local := fullMainLock()
	if _, err := ParseMainLock([]byte(local)); err != nil {
		t.Fatalf("local-only rejected: %v", err)
	}
	durable := strings.Replace(local, "publication_status = \"local-only\"", "publication_status = \"durably-retrievable\"\ndurable_retrieval_material_id = \"fork\"", 1)
	durable = strings.Replace(durable, "kind = \"git-local\"", "kind = \"git-https\"", 1)
	durable = strings.Replace(durable, "[materials.git_local]\nrepository_id = \"main\"\ncommit = \"dddddddddddddddddddddddddddddddddddddddd\"\ntree = \"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee\"", "[materials.git_https]\nrepository_id = \"main\"\nurl = \"https://example.com/fork\"\ncommit = \"dddddddddddddddddddddddddddddddddddddddd\"\ntree = \"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee\"", 1)
	if _, err := ParseMainLock([]byte(durable)); err != nil {
		t.Fatalf("durable lock rejected: %v", err)
	}
	assertMainLockCode(t, strings.Replace(durable, "durable_retrieval_material_id = \"fork\"", "durable_retrieval_material_id = \"upstream\"", 1), CodeLockSchemaInvalid)
}

func TestValidateMainLockNestedPointersDoNotMutate(t *testing.T) {
	lock, err := ParseMainLock([]byte(fullMainLock()))
	if err != nil {
		t.Fatal(err)
	}
	broken := cloneMainLock(lock)
	broken.Materials[0].ArchiveHTTPS.URL = "http://example.com/archive.tar"
	broken.Toolchains[0].Components[0].Version = stringPointer("bad\nversion")
	before := cloneMainLock(broken)
	if err := ValidateMainLock(broken); err == nil {
		t.Fatal("invalid nested values accepted")
	}
	if !reflect.DeepEqual(broken, before) {
		t.Fatal("failure validation mutated nested pointers")
	}
}

func stringPointer(value string) *string { return &value }

func TestValidateMainLockLicenses(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"invalid SPDX", strings.Replace(fullMainLock(), "MIT OR Apache-2.0 WITH LLVM-exception", "MIT OR", 1)},
		{"mutable locator", strings.Replace(fullMainLock(), "notice_locator = \"LICENSE\"", "notice_locator = \"https://example.com/license?tag=x\"", 1)},
		{"bad redistribution", strings.Replace(fullMainLock(), "redistribution_status = \"redistributable\"", "redistribution_status = \"yes\"", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseMainLock([]byte(tc.raw))
			if !hasCode(err, CodeLicenseRecordIncomplete) {
				t.Fatalf("code = %v, want %v", codeOf(err), CodeLicenseRecordIncomplete)
			}
		})
	}
}

func TestValidateMainLockDoesNotMutate(t *testing.T) {
	lock, err := ParseMainLock([]byte(fullMainLock()))
	if err != nil {
		t.Fatal(err)
	}
	before := cloneMainLock(lock)
	if err := ValidateMainLock(lock); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(lock, before) {
		t.Fatal("success validation mutated lock")
	}
	broken := cloneMainLock(lock)
	broken.Materials[0].Role = "invalid"
	brokenBefore := cloneMainLock(broken)
	if err := ValidateMainLock(broken); !hasCode(err, CodeLockSchemaInvalid) {
		t.Fatalf("code = %v", codeOf(err))
	}
	if !reflect.DeepEqual(broken, brokenBefore) {
		t.Fatal("failure validation mutated lock")
	}
}

func cloneMainLock(in MainLock) MainLock {
	out := in
	out.Environment.PathPolicy = append([]string(nil), in.Environment.PathPolicy...)
	out.Main.PatchCommits = append([]string(nil), in.Main.PatchCommits...)
	out.Build.Entrypoint = append([]string(nil), in.Build.Entrypoint...)
	out.Build.AllowedFinalArtifacts = append([]string(nil), in.Build.AllowedFinalArtifacts...)
	out.Materials = append([]Material(nil), in.Materials...)
	for i := range out.Materials {
		out.Materials[i].LicenseIDs = append([]string(nil), in.Materials[i].LicenseIDs...)
		if in.Materials[i].GitLocal != nil {
			x := *in.Materials[i].GitLocal
			out.Materials[i].GitLocal = &x
		}
		if in.Materials[i].GitHTTPS != nil {
			x := *in.Materials[i].GitHTTPS
			out.Materials[i].GitHTTPS = &x
		}
		if in.Materials[i].ArchiveHTTPS != nil {
			x := *in.Materials[i].ArchiveHTTPS
			out.Materials[i].ArchiveHTTPS = &x
		}
		if in.Materials[i].OCI != nil {
			x := *in.Materials[i].OCI
			out.Materials[i].OCI = &x
		}
		if in.Materials[i].GitSubtree != nil {
			x := *in.Materials[i].GitSubtree
			out.Materials[i].GitSubtree = &x
		}
		if in.Materials[i].MaterialFile != nil {
			x := *in.Materials[i].MaterialFile
			out.Materials[i].MaterialFile = &x
		}
	}
	out.Toolchains = append([]Toolchain(nil), in.Toolchains...)
	for i := range out.Toolchains {
		out.Toolchains[i].Components = append([]ToolchainComponent(nil), in.Toolchains[i].Components...)
		for j := range out.Toolchains[i].Components {
			if in.Toolchains[i].Components[j].ExecutableSHA256 != nil {
				x := *in.Toolchains[i].Components[j].ExecutableSHA256
				out.Toolchains[i].Components[j].ExecutableSHA256 = &x
			}
			if in.Toolchains[i].Components[j].Version != nil {
				x := *in.Toolchains[i].Components[j].Version
				out.Toolchains[i].Components[j].Version = &x
			}
		}
	}
	out.BuildUtilities = append([]BuildUtility(nil), in.BuildUtilities...)
	out.Configs = append([]BuildConfig(nil), in.Configs...)
	out.Policies = append([]Policy(nil), in.Policies...)
	out.Licenses = append([]License(nil), in.Licenses...)
	return out
}

func codeOf(err error) Code {
	var f *Failure
	if errors.As(err, &f) {
		return f.Code
	}
	return ""
}
