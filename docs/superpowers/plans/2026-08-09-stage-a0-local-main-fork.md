# Stage A0 Local Main Fork Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and host-test the strict bootstrap and deterministic VDATE fork
initializer required to create the authorized local `Main_MiSTer` fork.

**Architecture:** `internal/stagea0` parses a closed, byte-validated bootstrap
and applies Recipe V1 to a synthetic build source without consulting ambient
time, locale, Git configuration, or a target.  A runner-injected initializer
uses a private Git configuration and `commit-tree` to create exactly one VDATE
commit.  The real bootstrap and fork are operator-gated outputs after the
generic tooling has passed review; Tasks 1–2 make no network request, while
Task 3 performs one official immutable HTTPS object fetch. This plan does not
build Main, deploy, or touch hardware.

**Tech Stack:** Go 1.26.5, `github.com/pelletier/go-toml/v2` v2.4.3, Go standard
library, system Git in host-only synthetic fixtures, POSIX `sh`, GNU Make, and
ShellCheck.

## Global Constraints

- Governing design: [Stage A0 reproducible Main baseline design](../specs/2026-08-08-stage-a0-reproducible-main-baseline-design.md); the active roadmap labels this work **Designed** until evidence is recorded.
- This plan changes no target image, hardware, deployment, SSH configuration, public protocol, cache, final lock, build sandbox, artifact report, or promotion record.
- Do not add dependencies. Use the existing TOML decoder with `DisallowUnknownFields` and the Go standard library.
- `build/stage-a0-bootstrap.toml` is UTF-8, LF-only TOML. It contains no credential, target identity, local machine path, local fork commit, or self-hash.
- The bootstrap locks a full 40-character lowercase FogCast C0 revision, upstream commit, and tree; a normalized official HTTPS URL; branch parent equal to the locked commit; a safe relative regular-blob source path; raw source SHA-256; `%y%m%d`; `YYMMDD`; `UTC`; six ASCII digits; Recipe V1; and exact unsigned Git identities/dates/message.
- Official HTTPS URLs use lowercase `https` and lowercase host, no userinfo, query, fragment, explicit default port, host case variant, or trailing slash. `source_path` is nonempty relative UTF-8 with no control byte, absolute prefix, `.` or `..` component; every component resolves in the locked tree without a symlink and the leaf is a regular blob mode `100644` or `100755`.
- Recipe V1 verifies the bootstrap-locked source bytes before it finds a unique expression token. It does not assume or record any actual upstream line bytes until Sol completes source-authority resolution.
- The only intended Main source change is the deterministic VDATE build-metadata transformation. It preserves compiler flags, libraries, toolchain, network behavior, and FogCast documentation; this source review does not claim runtime equivalence or any observed runtime behavior.
- The initializer invokes no push and accepts no credential argument. Its only destination mutation is the user-authorized sibling fork creation, including its intrinsic first deterministic VDATE commit. Later Main commits and every push require separate explicit user authorization.
- An existing destination is verification-only: mismatch, dirty state, extra remote, or changed ancestry fails closed. The initializer never reset, rebase, overwrite, delete, or repairs it.
- C0 is a FogCast commit containing only reviewed generic tooling and tests. It requires explicit user authorization after Sol and Vega gates. The real bootstrap is excluded from C0.
- C0's exact allowlist is `internal/stagea0/types.go`, `internal/stagea0/{bootstrap,recipe,git}.go`, `internal/stagea0/{bootstrap,recipe,git}_test.go`, `cmd/stage-a0/{main,main_test}.go`, `scripts/stage-a0-init-main.sh`, `scripts/tests/stage-a0-init_test.sh`, and `Makefile`. This plan document is dispositioned separately: if a user authorizes a documentation commit, it precedes C0; otherwise it remains uncommitted and is excluded from C0.
- Source authority creates the real bootstrap only after C0. The already-authorized sibling clone and first VDATE commit run only after that review, the real bootstrap is validated, the tool verifies its C0 binding, and the user selects implementation execution. No target authorization is relevant.
- One Luna owner writes the FogCast files in this worktree. A separately assigned Main owner writes only the sibling `Main_MiSTer` worktree. Sol and Vega are read-only reviewers; Vega does not review its own changes.
- Before handoff run `git diff --check`, `git status --short`, no-index whitespace checks for untracked deliverables, and SHA-256 over every untracked deliverable. Do not stage, commit, push, clone, use the network, or touch hardware while authoring this plan.

---

## Exact file map

| File | Responsibility |
| --- | --- |
| `internal/stagea0/types.go` | Closed bootstrap, recipe, Git request, outcome, and error-code types. |
| `internal/stagea0/bootstrap.go` | Raw-byte validation, TOML decode, semantic bootstrap validation, and UTC VDATE rendering. |
| `internal/stagea0/recipe.go` | Exact Recipe V1 source-evidence verification and one-line transformation. |
| `internal/stagea0/git.go` | Runner-injected isolated Git commands, repository-policy verification, and deterministic `commit-tree` creation. |
| `internal/stagea0/bootstrap_test.go` | Closed-schema, encoding, semantic, and UTC-render tests. |
| `internal/stagea0/recipe_test.go` | Byte-exact Recipe V1 positive and hostile-source tests. |
| `internal/stagea0/git_test.go` | Hostile synthetic Git-runner command/configuration/idempotence tests. |
| `cmd/stage-a0/main.go` | `bootstrap-verify` and `init-main` argument parsing and stable exit mapping. |
| `cmd/stage-a0/main_test.go` | CLI argument, error-code, and stdout/stderr tests. |
| `scripts/stage-a0-init-main.sh` | Thin POSIX `exec` wrapper for `init-main`. |
| `scripts/tests/stage-a0-init_test.sh` | Wrapper argument forwarding and prohibited-operation source scan. |
| `Makefile` | `build-stage-a0`, `stage-a0-test`, and `stage-a0-check` targets. |
| `build/stage-a0-bootstrap.toml` | Post-C0, Sol-reviewed real bootstrap; not created or committed by Tasks 1–2. |
| `docs/stage-a0/local-main-fork-handoff-<bootstrap-sha256>.md` | Post-execution C0/tool/source/fork review and evidence handoff. |

`internal/stagea0` is a new package. No `cache.go`, lock type, container,
build adapter, report writer, or promotion API belongs to this plan.

## Complete contracts

All errors returned from the public functions below are `*Failure` with one of
the listed `Code` values. `Detail` contains no physical path, credentials, or
target identity.

```go
package stagea0

type Code string

const (
	CodeBootstrapSchemaInvalid    Code = "BOOTSTRAP_SCHEMA_INVALID"
	CodeVDateRecipeMismatch       Code = "VDATE_RECIPE_MISMATCH"
	CodeVDateInputInvalid         Code = "VDATE_INPUT_INVALID"
	CodeRepositoryPolicyMismatch  Code = "REPOSITORY_POLICY_MISMATCH"
	CodeCommandFailed             Code = "COMMAND_FAILED"
)

const (
	ExitOK        = 0
	ExitUsage     = 2
	ExitBootstrap = 3
	ExitRecipe    = 4
	ExitRepository = 5
	ExitCommand   = 6
)

const (
	BootstrapFormatV1 = 1
	BootstrapSchemaV1 = "fogcast.stage-a0-bootstrap"
	VDateRecipeV1     = "vdate-recipe-v1"
	VDateExpressionV1 = "%y%m%d"
	VDateFormatV1     = "YYMMDD"
	VDateTimezoneV1   = "UTC"
	VDateDigitsV1     = 6
	DisabledPushURL    = "disabled://stage-a0/upstream"
	RecipeV1ValidationBlock = "ifneq ($(origin VDATE),command line)\n$(error VDATE must be supplied as six ASCII YYMMDD digits)\nendif\noverride stage_a0_shell_quote = '$(subst ','\"'\"',$(1))'\noverride STAGE_A0_VDATE := $(value VDATE)\noverride VDATE_VALID := $(shell LC_ALL=C; export LC_ALL; VDATE=$(call stage_a0_shell_quote,$(STAGE_A0_VDATE)); export VDATE; [[ \"$$VDATE\" =~ ^[0-9]{6}$$ ]] && printf valid)\nifneq ($(VDATE_VALID),valid)\n$(error VDATE must be supplied as six ASCII YYMMDD digits)\nendif\noverride VDATE := $(STAGE_A0_VDATE)\n"
	RecipeV1DateValue   = "$(VDATE)"
)

type Failure struct {
	Code   Code
	Op     string
	Detail string
	Err    error
}

func (f *Failure) Error() string
func (f *Failure) Unwrap() error

type Bootstrap struct {
	Format              int           `toml:"format"`
	Schema              string        `toml:"schema"`
	FogCastBaseRevision string        `toml:"fogcast_base_revision"`
	MainUpstream        MainUpstream  `toml:"main_upstream"`
	Branch              Branch        `toml:"branch"`
	VDate               VDateSpec     `toml:"vdate"`
	Patch               PatchSpec     `toml:"patch"`
	InitialCommit       CommitSpec    `toml:"initial_commit"`
}

type MainUpstream struct {
	RepositoryID string `toml:"repository_id"`
	FetchURL     string `toml:"fetch_url"`
	Commit       string `toml:"commit"`
	Tree         string `toml:"tree"`
}

type Branch struct {
	Name         string `toml:"name"`
	ParentCommit string `toml:"parent_commit"`
}

type VDateSpec struct {
	SourcePath           string `toml:"source_path"`
	SourceEvidenceSHA256 string `toml:"source_evidence_sha256"`
	OfficialExpression   string `toml:"official_expression"`
	Format               string `toml:"format"`
	Timezone             string `toml:"timezone"`
	ASCIIDigits          int    `toml:"ascii_digits"`
}

type PatchSpec struct { RecipeVersion string `toml:"recipe_version"` }

type CommitSpec struct {
	AuthorName         string `toml:"author_name"`
	AuthorEmail        string `toml:"author_email"`
	CommitterName      string `toml:"committer_name"`
	CommitterEmail     string `toml:"committer_email"`
	AuthorTimestamp    int64  `toml:"author_timestamp"`
	CommitterTimestamp int64  `toml:"committer_timestamp"`
	CommitMessage      string `toml:"commit_message"`
	Signing            bool   `toml:"signing"`
}

type Command struct {
	Path  string
	Args  []string
	Env   []string
	Dir   string
	Stdin []byte
}

type CommandResult struct { Stdout, Stderr []byte; ExitCode int }

type Runner interface {
	Run(ctx context.Context, command Command) (CommandResult, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command Command) (CommandResult, error)

type InitRequest struct {
	Bootstrap   Bootstrap
	Destination string
	FogCastRoot string
}

type ForkIdentity struct {
	UpstreamCommit string
	UpstreamTree   string
	Branch          string
	PatchCommit     string
	PatchTree       string
}

func ParseBootstrap(raw []byte) (Bootstrap, error)
func ValidateBootstrap(value Bootstrap) error
func RenderVDate(timestamp int64) (string, error)
func ApplyVDateRecipeV1(source []byte, spec VDateSpec) ([]byte, error)
func VerifyFogCastC0(ctx context.Context, runner Runner, bootstrap Bootstrap, fogcastRoot string) error
func InitializeFork(ctx context.Context, runner Runner, request InitRequest) (ForkIdentity, error)
func run(args []string, stdout, stderr io.Writer, runner Runner) int
func exitCode(err error) int

func validateRecipeV1(spec VDateSpec) error
func verifySourceBytes(source []byte, expectedSHA256 string) error
func recipeV1Token(spec VDateSpec) []byte
func recipeV1Offsets(source, token []byte) (lineStart, tokenStart, tokenEnd, lineEnd, count int)
func failure(code Code, op, detail string) error
```

`run` writes a single safe line `CODE: detail` to stderr for a failure and never
formats a wrapped filesystem error directly. Its stable process-status mapping
is fixed here:

| Condition | Exit status |
| --- | --- |
| Successful command | `ExitOK` (`0`) |
| Missing/unknown CLI syntax | `ExitUsage` (`2`) |
| `CodeBootstrapSchemaInvalid` | `ExitBootstrap` (`3`) |
| `CodeVDateRecipeMismatch` or `CodeVDateInputInvalid` | `ExitRecipe` (`4`) |
| `CodeRepositoryPolicyMismatch` | `ExitRepository` (`5`) |
| `CodeCommandFailed` | `ExitCommand` (`6`) |

`VerifyFogCastC0` uses the isolated read-only Git environment and requires all
three facts at execution time: `HEAD^{commit}` equals the bootstrap's full
`FogCastBaseRevision`; `git symbolic-ref -q HEAD` reports detached HEAD; and
`git status --porcelain=v1 --untracked-files=all` is empty. Any other state is
`CodeRepositoryPolicyMismatch`.

`recipeV1Token` has one closed construction; it does not accept caller text
other than the already-validated bootstrap expression:

```go
func recipeV1Token(spec VDateSpec) []byte {
	return []byte("`date +\"" + spec.OfficialExpression + "\"`")
}
```

## Recipe V1 byte contract

Recipe V1 is intentionally independent of unresolved upstream source text.
The real bootstrap's `vdate.source_path` and `vdate.source_evidence_sha256` are
filled only by Sol's source-authority review of the selected immutable commit
and tree. The following algorithm is the entire transformation contract:

1. `ValidateBootstrap` requires `Patch.RecipeVersion == "vdate-recipe-v1"`, `OfficialExpression == "%y%m%d"`, `Format == "YYMMDD"`, `Timezone == "UTC"`, and `ASCIIDigits == 6`. `ApplyVDateRecipeV1` accepts only `VDateSpec`; it is intrinsically Recipe V1 and does not inspect `PatchSpec`.
2. Require the raw source be valid UTF-8, LF-only, and end with LF. Compute SHA-256 over the raw bytes; it must equal `SourceEvidenceSHA256` in lowercase hexadecimal.
3. Construct the exact byte token from the closed Recipe V1 form and the
   bootstrap expression: `` `date +"%y%m%d"` ``. Across the raw source there
   must be exactly one occurrence of that full token, and it must lie in one
   complete LF-terminated line. A second occurrence, no occurrence, a CR byte,
   malformed UTF-8, or a missing final LF returns `CodeVDateRecipeMismatch`.
4. Insert the exact UTF-8 bytes in `RecipeV1ValidationBlock` immediately before the
   containing line. Its first operation is the conditional requiring
   `$(origin VDATE)` to be exactly `command line`; it therefore rejects a missing
   or environment-only value before every private override. `override STAGE_A0_VDATE := $(value VDATE)` captures the original command-line
   bytes without self-assignment and cannot be replaced by a direct command-line
   assignment. `override stage_a0_shell_quote` single-quotes that
   captured value for the shell; its only quote escape is `'"'"'`. Its `LC_ALL=C`
   Bash test is also `override`-assigned, accepts only the already-rendered six ASCII digit value, and only
   after that success does `override VDATE := $(STAGE_A0_VDATE)` set the public
   variable. In that original containing line,
   replace only the matched full token with `RecipeV1DateValue`. Preserve the
   line's preceding bytes, following bytes, and terminating LF without
   normalization.
5. Recompute the output as: original prefix before the containing line;
   `RecipeV1ValidationBlock`; original containing-line prefix before the date command;
   `RecipeV1DateValue`; original containing-line suffix after the date command;
   and the untouched original suffix. No wall-clock, locale, timezone,
   environment variable, shell, regex substitution, or Git operation participates.

The exact inserted GNU Make/Bash validation block is:

```make
ifneq ($(origin VDATE),command line)
$(error VDATE must be supplied as six ASCII YYMMDD digits)
endif
override stage_a0_shell_quote = '$(subst ','"'"',$(1))'
override STAGE_A0_VDATE := $(value VDATE)
override VDATE_VALID := $(shell LC_ALL=C; export LC_ALL; VDATE=$(call stage_a0_shell_quote,$(STAGE_A0_VDATE)); export VDATE; [[ "$$VDATE" =~ ^[0-9]{6}$$ ]] && printf valid)
ifneq ($(VDATE_VALID),valid)
$(error VDATE must be supplied as six ASCII YYMMDD digits)
endif
override VDATE := $(STAGE_A0_VDATE)
```

Recipe V1 validates a canonical command-line six-ASCII-digit metadata value;
`RenderVDate` plus the later locked adapter establish its equality to the
locked epoch. This plan creates neither that adapter nor a lock; it only
guarantees that the first fork patch removes the ambient date command and
requires an explicit metadata input. Recipe V1 preserves all other
DFLAGS/build-line bytes: the resulting existing line contains
`-DVDATE=\"$(VDATE)\"` at the original date-command position. The source-authority
review must verify that the selected source has a Bash-compatible `SHELL` in
effect at this insertion position. Recipe V1 makes no intended non-metadata
source change; it makes no runtime-equivalence claim.

Arbitrary or expanding `MAKEFLAGS` is outside Recipe V1's boundary. The later
locked build adapter must clear and replace `MAKEFLAGS` before starting Make;
Recipe V1 neither validates nor prevents arbitrary `MAKEFLAGS` expansion.

## Task 1: Closed bootstrap parser and Recipe V1

**Files:**

- Create: `internal/stagea0/types.go`
- Create: `internal/stagea0/bootstrap.go`
- Create: `internal/stagea0/recipe.go`
- Create: `internal/stagea0/bootstrap_test.go`
- Create: `internal/stagea0/recipe_test.go`

**Interfaces:** Produces all parser, VDATE renderer, and recipe signatures in
the complete contracts section. Task 2 consumes `Bootstrap`, `Runner`,
`InitRequest`, and `ApplyVDateRecipeV1` unchanged.

- [ ] **Step 1: Write the failing closed-bootstrap tests**

Add a valid synthetic TOML byte slice plus table cases for an unknown field,
CRLF, invalid UTF-8, abbreviated/uppercase FogCast C0/upstream commit/tree or
SHA-256, non-HTTPS URL, URL userinfo/query/fragment/default port/host case/
trailing slash, a control byte, parent mismatch, branch other than exact
`fogcast/stage-a-baseline`, unsafe `source_path` (`/x`, `a/../b`, `./a`,
`a//b`, or NUL), incorrect recipe/version/token/format/timezone/digit count,
negative timestamp, `signing = true`, and a commit message without trailing LF.
Add UTC rendering tests for epoch `0` (`700101`) and `1722470400` (`240801`).

```go
func TestRenderVDateUsesUTC(t *testing.T) {
	got, err := RenderVDate(1722470400)
	if err != nil || got != "240801" {
		t.Fatalf("RenderVDate() = %q, %v", got, err)
	}
}

func TestParseBootstrapRejectsCRLF(t *testing.T) {
	_, err := ParseBootstrap(bytes.ReplaceAll(validBootstrapTOML(), []byte("\n"), []byte("\r\n")))
	if !hasCode(err, CodeBootstrapSchemaInvalid) {
		t.Fatalf("error code = %v", err)
	}
}
```

- [ ] **Step 2: Run the parser test to confirm failure**

Run: `mise exec go@1.26.5 -- go test ./internal/stagea0 -run 'Test(ParseBootstrap|RenderVDate)' -count=1 -v`

Expected: FAIL because the package and parser do not exist.

- [ ] **Step 3: Write the failing byte-exact Recipe V1 tests**

Use a synthetic LF-only source whose prior line sets a Bash-compatible `SHELL`
and whose one DFLAGS line contains the exact full token
`` `date +"%y%m%d"` ``; its SHA-256 belongs in the synthetic
`VDateSpec`. Assert the complete original DFLAGS prefix and suffix survive
unchanged, the input line is inserted directly before that DFLAGS line, and
only the matched date-command bytes become `$(VDATE)`. Add cases for SHA
mismatch, two full tokens, no full token, CRLF, malformed UTF-8, and no terminal
LF. Execute the transformed synthetic Makefile with missing `VDATE`,
environment-only `VDATE=240801`, command-line `VDATE=2408AA`, and command-line
`VDATE=240801`; the first three must stop with the exact error text and the
last must emit the preserved DFLAGS line containing `-DVDATE="240801"` with no
backslash byte in stdout. Add a command-line single-quote/semicolon payload;
it must reject and prove its sentinel command never executes. Add command-line
attempts to override `VDATE_VALID`, `STAGE_A0_VDATE`, and
`stage_a0_shell_quote`; each either rejects or retains the canonical valid
`VDATE=240801`, and none executes a sentinel command. Include only a harmless,
non-expanding `MAKEFLAGS=--no-builtin-rules` case; arbitrary or expanding
`MAKEFLAGS` is intentionally not exercised by Recipe V1 tests.

```go
func TestApplyVDateRecipeV1PreservesDFLAGSBytes(t *testing.T) {
	source := []byte("SHELL := /bin/bash\nDFLAGS = -O2 -DVDATE=\\\"`date +\"%y%m%d\"`\\\" -Wall\nprint:\n\t@printf '%s\\n' \"$(DFLAGS)\"\n")
	spec := validVDateSpecFor(source)
	got, err := ApplyVDateRecipeV1(source, spec)
	if err != nil { t.Fatal(err) }
	want := []byte("SHELL := /bin/bash\n" + RecipeV1ValidationBlock + "DFLAGS = -O2 -DVDATE=\\\"$(VDATE)\\\" -Wall\nprint:\n\t@printf '%s\\n' \"$(DFLAGS)\"\n")
	if !bytes.Equal(got, want) { t.Fatalf("recipe bytes = %q", got) }
	if !bytes.Contains(got, []byte("DFLAGS = -O2 -DVDATE=\\\"$(VDATE)\\\" -Wall\n")) {
		t.Fatalf("DFLAGS prefix or suffix changed: %q", got)
	}
}

func TestRecipeV1MakeInputValidation(t *testing.T) {
	makefile := filepath.Join(t.TempDir(), "Makefile")
	patched, err := ApplyVDateRecipeV1(validRecipeSource(), validVDateSpecFor(validRecipeSource()))
	if err != nil { t.Fatal(err) }
	if err := os.WriteFile(makefile, patched, 0o600); err != nil { t.Fatal(err) }
	sentinel := filepath.Join(t.TempDir(), "must-not-exist")
	for _, tc := range []struct { name string; args, env []string; want string; ok bool }{
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
			cmd.Env = append([]string{
				"PATH=/usr/bin:/bin", "HOME=" + t.TempDir(), "LC_ALL=C", "LANG=C", "TZ=UTC",
			}, tc.env...)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok || !bytes.Contains(out, []byte(tc.want)) || (tc.ok && bytes.Contains(out, []byte(`\"`))) { t.Fatalf("out=%q err=%v", out, err) }
			if _, err := os.Stat(sentinel); !errors.Is(err, fs.ErrNotExist) { t.Fatalf("hostile VDATE executed: %v", err) }
		})
	}
}
```

The `combined-command-line-overrides` case is the direct probe
`VDATE=240801 VDATE_VALID=valid STAGE_A0_VDATE=2408AA stage_a0_shell_quote=bad`;
it must print `-DVDATE="240801"`. Both sentinel cases assert the sentinel path
does not exist after Make exits.

- [ ] **Step 4: Run the recipe test to confirm failure**

Run:

```sh
bash -c 'LC_ALL=C; export LC_ALL; VDATE=240801; [[ "$VDATE" =~ ^[0-9]{6}$ ]] && printf valid'
mise exec go@1.26.5 -- go test ./internal/stagea0 -run 'Test(ApplyVDateRecipeV1|RecipeV1MakeInputValidation)' -count=1 -v
```

Expected: the Bash probe prints `valid`; the Go test fails because Recipe V1
does not exist.

- [ ] **Step 5: Implement parser, semantic validation, UTC rendering, and Recipe V1**

Use `toml.NewDecoder(bytes.NewReader(raw))`, call
`decoder.DisallowUnknownFields()`, decode once, and reject trailing data. Wrap
each invalidity in `Failure{Code: CodeBootstrapSchemaInvalid, ...}`. Validate
each fixed bootstrap literal and regex-free fixed-width lowercase hexadecimal
identifier with explicit byte predicates. Render only with
`time.Unix(timestamp, 0).UTC().Format("060102")` and verify every byte is
`'0'` through `'9'`.

```go
func ApplyVDateRecipeV1(source []byte, spec VDateSpec) ([]byte, error) {
	if err := validateRecipeV1(spec); err != nil { return nil, err }
	if err := verifySourceBytes(source, spec.SourceEvidenceSHA256); err != nil { return nil, err }
	lineStart, tokenStart, tokenEnd, lineEnd, count := recipeV1Offsets(source, recipeV1Token(spec))
	if count != 1 { return nil, failure(CodeVDateRecipeMismatch, "recipe", "date command token is not unique") }
	out := make([]byte, 0, len(source)+len(RecipeV1ValidationBlock)-tokenEnd+tokenStart+len(RecipeV1DateValue))
	out = append(out, source[:lineStart]...)
	out = append(out, RecipeV1ValidationBlock...)
	out = append(out, source[lineStart:tokenStart]...)
	out = append(out, RecipeV1DateValue...)
	out = append(out, source[tokenEnd:lineEnd]...)
	return append(out, source[lineEnd:]...), nil
}
```

- [ ] **Step 6: Run focused tests**

Run: `mise exec go@1.26.5 -- go test ./internal/stagea0 -run 'Test(ParseBootstrap|RenderVDate|ApplyVDateRecipeV1|RecipeV1MakeInputValidation)' -count=1 -v`

Expected: PASS.

- [ ] **Step 7: Sol and Vega gate**

Sol verifies that the schema, raw-byte evidence, UTC conversion, and recipe do
not turn unresolved upstream text into an assumed fact. Vega independently
inspects the exact synthetic-byte diff and every hostile parser/recipe case.
Resolve Critical and Important findings before Task 2.

## Task 2: Isolated Git initializer, CLI, and wrapper

**Files:**

- Create: `internal/stagea0/git.go`
- Create: `internal/stagea0/git_test.go`
- Create: `cmd/stage-a0/main.go`
- Create: `cmd/stage-a0/main_test.go`
- Create: `scripts/stage-a0-init-main.sh`
- Create: `scripts/tests/stage-a0-init_test.sh`
- Modify: `Makefile`

**Interfaces:** Consumes the Task 1 contracts without alteration. Produces
`bin/stage-a0`, `bootstrap-verify`, `init-main`, `build-stage-a0`,
`stage-a0-test`, and `stage-a0-check`.

- [ ] **Step 1: Write failing hostile Git-runner tests**

Implement a recording fake `Runner` that returns scripted command results and
captures the exact `Command`. Test absent-destination command sequence; reject
an extra remote, a changed effective push URL, a branch with wrong parent/tree,
a dirty worktree, a patch with wrong direct parent, and any attempted `push`,
`reset`, `rebase`, `clean`, `checkout -f`, or `config --global`. Assert each
Git command has an absolute executable. The initialization command environment
replaces, rather than extends, the ambient environment and contains exactly:

```text
GIT_CONFIG_NOSYSTEM=1
GIT_CONFIG_GLOBAL=/dev/null
GIT_TERMINAL_PROMPT=0
GIT_ATTR_NOSYSTEM=1
GIT_NO_REPLACE_OBJECTS=1
LC_ALL=C
LANG=C
TZ=UTC
GIT_INDEX_FILE=<absolute temporary-repository/.git/index>
GIT_AUTHOR_NAME=<locked author name>
GIT_AUTHOR_EMAIL=<locked author email>
GIT_AUTHOR_DATE=<locked author epoch> +0000
GIT_COMMITTER_NAME=<locked committer name>
GIT_COMMITTER_EMAIL=<locked committer email>
GIT_COMMITTER_DATE=<locked committer epoch> +0000
```

Existing-destination verification uses its own replacement environment with
the first eight non-index lines above plus `GIT_OPTIONAL_LOCKS=0`; it does not
set author/committer fields or a writable index. Test the exact fetch command
is `fetch --no-tags --no-write-fetch-head --no-recurse-submodules <temporary-remote> <full-40-character-commit>`, with no branch/tag refspec, persisted
temporary ref, or `FETCH_HEAD` file.

Use synthetic trees where `source_path` is a symlink, has a symlink parent, is a
submodule/tree, or has a mode other than `100644`/`100755`; each must fail before
the temporary worktree writes the source. Include a before/after filesystem,
refs, index, config, and timestamp snapshot in every existing-destination test.

```go
func TestInitializeForkUsesCommitTreeAndNoPush(t *testing.T) {
	runner := newScriptedRunner(successfulInitTranscript())
	_, err := InitializeFork(context.Background(), runner, validInitRequest(t))
	if err != nil { t.Fatal(err) }
	runner.requireNoArg("push")
	runner.requireCommand("commit-tree", "-p", runner.upstreamCommit)
	runner.requireEnv("GIT_CONFIG_NOSYSTEM=1")
	runner.requireEnv("GIT_CONFIG_GLOBAL=/dev/null")
	runner.requireEnvPrefix("GIT_INDEX_FILE=")
}
```

Add `TestExecRunnerDeterministicFork` using real local bare synthetic upstream
repositories and two absent destinations with hostile inherited Git config,
timezone, locale, and environment. It asserts identical patch commit/tree,
parent, commit message bytes, author bytes, and committer bytes. Snapshot the
first destination's complete content, refs, index, config, and file timestamps;
rerun the initializer and assert the snapshot is byte-identical. Create a
mismatched existing destination, snapshot it before invocation, and assert the
same snapshot after `CodeRepositoryPolicyMismatch`. Hold the adjacent publish
lock from a competing initializer and assert a second initializer stops before
creating a destination. Inject a destination appearance after the lock is
acquired and assert publication stops, the destination remains untouched, and
cleanup removes only the initializer's validated temporary sibling root.

Use `HybridFixtureRunner` for this test. It delegates every command to
`ExecRunner` except one command whose path and arguments exactly equal the
production official-HTTPS full-object fetch command. For that one command only,
it imports the locked commit object from the local bare fixture into the
temporary repository, without changing the production initializer or accepting
a local URL. Any other fetch, branch/tag refspec, or argument drift is a test
failure. This fixture is test-only; production has no transport bypass.

```go
type HybridFixtureRunner struct {
	Exec             ExecRunner
	GitPath          string
	OfficialHTTPSURL string
	LockedCommit     string
	LocalBareFixture string
	TempRoot         string
	CanonicalIndex   string
	Intercepted      bool
}

func importLockedObject(ctx context.Context, execRunner ExecRunner, observedFetch Command, localBareFixture string) (CommandResult, error)

func (r *HybridFixtureRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	if isOfficialObjectFetch(command, r.GitPath, r.OfficialHTTPSURL, r.LockedCommit) {
		root, index, ok := learnContainedIndexAndRoot(command.Env, command.Dir)
		if r.Intercepted || !ok {
			return CommandResult{}, failure(CodeCommandFailed, "fixture", "invalid official fetch")
		}
		r.TempRoot, r.CanonicalIndex = root, index
		r.Intercepted = true
		return importLockedObject(ctx, r.Exec, command, r.LocalBareFixture)
	}
	if slices.Contains(command.Args, "fetch") {
		return CommandResult{}, failure(CodeCommandFailed, "fixture", "unexpected fetch command")
	}
	return r.Exec.Run(ctx, command)
}

func isOfficialObjectFetch(command Command, gitPath, officialHTTPSURL, lockedCommit string) bool
func learnContainedIndexAndRoot(env []string, root string) (temporaryRoot, canonicalIndex string, ok bool)
```

- [ ] **Step 2: Run the Git test to confirm failure**

Run: `mise exec go@1.26.5 -- go test ./internal/stagea0 -run 'Test(InitializeFork|ExecRunnerDeterministicFork)' -count=1 -v`

Expected: FAIL because the initializer does not exist.

- [ ] **Step 3: Implement the explicit Git command policy**

`InitializeFork` first validates the bootstrap. It resolves `git` once with
`exec.LookPath`, requires the resolved path be absolute, and passes that path
to every `Runner.Run` call. It first validates `source_path` against the locked
path syntax without touching a worktree. For an absent requested destination,
it acquires an adjacent single-writer publication lock before `os.MkdirTemp` or
any worktree write. The lock is
at `<destination>.stage-a0.publish.lock` using `os.OpenFile` with
`O_WRONLY|O_CREATE|O_EXCL`, mode `0600`, and a randomly generated initializer
token written and `fsync`ed by the creating descriptor. Re-read via that open
descriptor and `lstat` the pathname to require one regular file with the same
device/inode; otherwise fail. Hold this verified lock through the final
destination-absence check, rename, and final verification; unlink only that
lock in the creator's deferred cleanup. The protocol excludes non-cooperating
writers in the parent directory. After the lock is acquired, require the
destination absent; immediately before rename require it absent again. This
prevents cooperating initializers from relying on Go `os.Rename` replacement
semantics. Only after the lock and first absence check does it create a
validated empty temporary sibling with `os.MkdirTemp(parent,
".Main_MiSTer.stage-a0-")`. Failure cleanup may remove only that exact
validated temporary root; the requested destination is never created early.

Inside that temporary root it initializes an empty repository, adds the
official HTTPS URL as a temporary fetch transport, executes exactly
`git fetch --no-tags --no-write-fetch-head --no-recurse-submodules <temporary-remote> <full-commit>`, verifies `<commit>^{tree}` equals the locked tree, and
does not materialize any parent or source worktree. It removes the temporary
transport and verifies no temporary ref or `FETCH_HEAD` remains, then validates
the locked source tree component/mode/blob and adds the sole `upstream` remote
with the locked URL and `DisabledPushURL` and verifies
the effective fetch/push URLs. It sets only `core.autocrlf=false`, `core.eol=lf`,
`core.attributesfile=/dev/null`,
`attr.tree=4b825dc642cb6eb9a060e54bf8d69288fbee4904`,
`remote.upstream.url=<locked-url>`,
`remote.upstream.fetch=+refs/heads/*:refs/remotes/upstream/*`, and
`remote.upstream.pushurl=disabled://stage-a0/upstream` in local configuration.
It parses `git config --local --null --list` and rejects every key/value outside
the Git init repository-format keys plus those seven exact key/value entries.
It also requires SHA-1 object format, writes an empty tree with deterministic
empty stdin, requires the resulting OID to equal the configured canonical
value, verifies that object exists as a zero-entry tree, and rejects any
`.git/info/attributes` entry.

Set `GIT_INDEX_FILE` from the start to the exact canonical path
`<temporary-repository>/.git/index`, so it is the repository index that survives
the atomic rename. After fetch/object availability, validate the safe source
path and mode/blob, then seed that index with `git read-tree <parent>`, read the safe
source blob, transform its raw bytes with `ApplyVDateRecipeV1`, hash those bytes
with `git hash-object -w --stdin`, and run exactly
`git update-index --add --cacheinfo <mode>,<blob>,<path>`. Write the tree; use
`git diff-tree --no-commit-id --name-status -r <parent> <tree>` to require one
changed path equal to `source_path`; then run
`git -c commit.gpgSign=false commit-tree <tree> -p <parent> -F -` with the exact
message in stdin. Publish the ref only through
`git update-ref refs/heads/<branch> <patch-commit> <zero-oid>`, attach `HEAD`
with `git symbolic-ref HEAD refs/heads/<branch>`, read the patch tree into the
canonical index, and materialize it with valid `git checkout-index --all` after
the closed attributes policy above. Persistent attribute resolution must be
empty under the configured canonical empty tree. Before
checkout/materialization, run a command-scoped locked-tree audit equivalent to
`GIT_ATTR_SOURCE=<full-patch-tree> git check-attr --all -- <source_path>`;
accept an empty result or exactly the two LF-terminated records
`<source_path>: text: set` and `<source_path>: eol: lf`, in that order, only
when the locked raw source is CR-free LF text with a terminal LF. Any other
attribute name, value, order, duplicate, malformed output, or incompatible
source bytes fails closed. This exact pair is the selected official tree's
reviewed VDATE-source policy; it is not a general attribute allowlist.
`GIT_ATTR_SOURCE` is present only on that audit command and is absent from
checkout, status, and every other persistent-worktree operation. Verify the
materialized source raw SHA-256 equals the committed patched blob SHA-256, and
after checkout require ordinary `check-attr --all -- <source_path>` to remain
empty; this post-materialization gate proves `attr.tree` is effective even
though the tree now contains `.gitattributes`. Then
verify a clean worktree plus final index, tree, config, remotes, effective URLs,
and attached HEAD before atomically renaming the complete temporary root to the
still-absent requested destination. If the destination appears before
publication, stop and remove only the temporary root. The committed blob is
protected by the raw `hash-object` input and canonical index before materialization.
Tests assert no worktree write occurs before the source-tree/mode/blob gate,
locked-tree attribute audit, patch-tree diff gate, and commit/ref construction all
pass; `checkout-index --all` is the sole worktree materialization operation.
Add regression cases for a CRLF tracked file covered by `text eol=lf`, raw
blob/worktree equality, clean status after stat invalidation and repeated
checkout, exact command-scoped attribute recovery, and rejection of a wrong or
nonempty attribute tree and any `.git/info/attributes` entry. Also retain cases
for the exact admitted pair, reversed or duplicate
records, additional attributes, different `text`/`eol` values, malformed
records, and CRLF VDATE source bytes. The admitted-pair fixture must reach checkout
and prove the post-checkout raw SHA-256 equality gate; every rejected case must
stop before checkout.

It never uses checkout/reset/rebase/clean force options. For an existing
destination it creates a before/after snapshot of complete filesystem content,
refs, index, config, and timestamps; runs only read-only commands with
`GIT_OPTIONAL_LOCKS=0`; and returns `CodeRepositoryPolicyMismatch` unchanged on
any mismatch. Under that snapshot it reruns the SHA-1 object-format gate, exact
`attr.tree` config gate, empty-tree object existence/type/content checks,
`.git/info/attributes` absence check, post-materialization persistent-empty
attribute check, and command-scoped locked-tree audit. It never creates a
missing empty-tree object or repairs configuration in an existing destination.

```go
func gitEnv(spec CommitSpec, canonicalIndexPath string) []string {
	return []string{
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
		"GIT_ATTR_NOSYSTEM=1", "GIT_NO_REPLACE_OBJECTS=1", "LC_ALL=C", "LANG=C", "TZ=UTC",
		"GIT_INDEX_FILE=" + canonicalIndexPath,
		"GIT_AUTHOR_NAME=" + spec.AuthorName,
		"GIT_AUTHOR_EMAIL=" + spec.AuthorEmail,
		fmt.Sprintf("GIT_AUTHOR_DATE=%d +0000", spec.AuthorTimestamp),
		"GIT_COMMITTER_NAME=" + spec.CommitterName,
		"GIT_COMMITTER_EMAIL=" + spec.CommitterEmail,
		fmt.Sprintf("GIT_COMMITTER_DATE=%d +0000", spec.CommitterTimestamp),
	}
}
```

For an existing destination, run only read-only policy checks. An exact match
returns the existing `ForkIdentity`; every mismatch returns
`CodeRepositoryPolicyMismatch` without a mutation.

- [ ] **Step 4: Write failing CLI and wrapper tests**

Test `bootstrap-verify --bootstrap <fixture>` prints only `valid\n` after
`VerifyFogCastC0` observes a full current `HEAD` equal to
`fogcast_base_revision`; test `init-main` requires exactly `--bootstrap` and
`--destination`; unknown flags return `ExitUsage`, bootstrap failures return
`ExitBootstrap`, recipe failures return `ExitRecipe`, repository failures return
`ExitRepository`, and command failures return `ExitCommand`. Assert wrapped
filesystem errors render only the stable code and safe detail, never a physical
path. The wrapper test installs a fake
`STAGE_A0_TOOL`, checks forwarded bytes, and source-scans its script for
`push`, `ssh`, `curl`, `wget`, target paths, and credential flag names.
The fake tool writes each received argument followed by NUL to an artifact
file. Invoke the wrapper with a destination containing an embedded newline and
compare that NUL-delimited artifact with expected records; this proves `"$@"`
preserves boundaries rather than flattening arguments. POSIX argv cannot contain
a NUL byte, and the wrapper uses neither `eval` nor string reconstruction.

```sh
STAGE_A0_TOOL="$fixture_tool" scripts/stage-a0-init-main.sh \
  --bootstrap fixtures/bootstrap.toml --destination "$tmp/destination"
printf 'init-main\0--bootstrap\0fixtures/bootstrap.toml\0--destination\0%s\0' "$tmp/destination" > "$expected_args"
cmp "$expected_args" "$fixture_args"
```

- [ ] **Step 5: Run CLI and wrapper tests to confirm failure**

Run: `mise exec go@1.26.5 -- go test ./cmd/stage-a0 -count=1 -v && sh scripts/tests/stage-a0-init_test.sh`

Expected: FAIL because the CLI and wrapper do not exist.

- [ ] **Step 6: Implement CLI, wrapper, and Make targets**

`main` calls `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, ExecRunner{}))`.
`ExecRunner.Run` uses `exec.CommandContext`, assigns `command.Env` directly
(never appending `os.Environ()`), assigns `command.Dir`, copies `Stdin`, and
captures separate stdout/stderr. `bootstrap-verify` reads the supplied raw file,
calls `ParseBootstrap`, then `ValidateBootstrap` and `VerifyFogCastC0`;
`init-main` performs those calls before `InitializeFork`. Tests inject the
recording fake into `run` and assert only the listed argv/environment reaches
it. The wrapper is exactly an argument-preserving `exec` boundary:

```sh
#!/bin/sh
set -eu
tool=${STAGE_A0_TOOL:-"$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)/bin/stage-a0"}
exec "$tool" init-main "$@"
```

Add these Make targets without changing existing targets:

```make
.PHONY: build-stage-a0 stage-a0-test stage-a0-check

build-stage-a0:
	mkdir -p bin
	CGO_ENABLED=0 mise exec go@1.26.5 -- go build -buildvcs=false -trimpath -o bin/stage-a0 ./cmd/stage-a0

stage-a0-test:
	mise exec go@1.26.5 -- go test ./internal/stagea0 ./cmd/stage-a0
	sh scripts/tests/stage-a0-init_test.sh

stage-a0-check: build-stage-a0 stage-a0-test
	mise exec go@1.26.5 -- go test -race ./internal/stagea0 ./cmd/stage-a0
	mise exec go@1.26.5 -- go vet ./internal/stagea0 ./cmd/stage-a0
	test -z "$$(gofmt -l internal/stagea0 cmd/stage-a0)"
	sh -n scripts/stage-a0-init-main.sh scripts/tests/stage-a0-init_test.sh
	shellcheck -x scripts/stage-a0-init-main.sh scripts/tests/stage-a0-init_test.sh
```

- [ ] **Step 7: Run focused and full regression verification**

Run:

```sh
make stage-a0-check
make test
make vet
git diff --check
git status --short
```

Expected: all Stage A0 focused checks and the existing repository regression
suite PASS. If an unrelated existing regression fails, preserve its output in
the handoff and do not change unrelated code.

- [ ] **Step 8: Sol and Vega gate, then C0 authorization boundary**

Sol reviews Git isolation, remote policy, deterministic commit metadata, and
the repository-mutation boundary. Vega independently reviews the complete diff,
hostile command transcript, CLI/wrapper tests, and full regression output.
After both approve, request explicit user authorization to create C0 containing
only the reviewed generic FogCast source, tests, wrapper, and Make targets.
Do not include a real bootstrap, any Main content, source evidence, secrets, or
generated source data. C0 does not authorize a push.

## Task 3: Operator-gated source authority, authorized fork, and handoff

**Files:**

- Create after C0 and Sol review: `build/stage-a0-bootstrap.toml`
- Create in the separately owned sibling worktree: the one VDATE-only source-file change and intrinsic initial Git commit
- Create in FogCast handoff record after execution: `docs/stage-a0/local-main-fork-handoff-<bootstrap-sha256>.md`

**Interfaces:** Consumes only `bin/stage-a0`, the Task 1 bootstrap schema, and
the Task 2 initializer. Produces the reviewed local fork identity and a
handoff for the next scoped plan. It produces no cache, final lock, build,
artifact, report, promotion, HIL, or Accepted output.

- [ ] **Step 1: Record C0 only after explicit user authorization**

After Task 2's Sol/Vega approval and an explicit user instruction to commit,
record C0's full 40-character lowercase commit ID, tree ID, and the exact
allowlist from Global Constraints. Confirm C0 excludes
`build/stage-a0-bootstrap.toml`, the sibling repository, and this plan unless
the separately authorized documentation disposition occurred first. Do not
push. Build and use the tool only from a clean detached C0 worktree:

```sh
git -C "$FOGCAST_ROOT" worktree add --detach "$STAGE_A0_C0_WORKTREE" "$C0"
test -z "$(git -C "$STAGE_A0_C0_WORKTREE" status --porcelain=v1 --untracked-files=all)"
(cd "$STAGE_A0_C0_WORKTREE" && mise exec go@1.26.5 -- go build -buildvcs=false -trimpath -o "$STAGE_A0_TOOL" ./cmd/stage-a0)
git -C "$STAGE_A0_C0_WORKTREE" rev-parse --verify HEAD^{commit}
git --version
shasum -a 256 "$STAGE_A0_TOOL"
```

Record the C0 tree ID, detached-worktree `HEAD`, `git --version`, Go toolchain
version, and tool SHA-256. The bootstrap must set `fogcast_base_revision`
exactly to C0; `bootstrap-verify` and `init-main` run from that detached C0
worktree and reject a different current `HEAD`.

- [ ] **Step 2: Sol resolves source authority; root/operator approves commit identity; Luna creates the real bootstrap**

Sol resolves the official immutable HTTPS repository, full commit, tree, and
the source bytes at the locked `vdate.source_path`; records the raw source
SHA-256; verifies that the exact full fragment `` `date +"%y%m%d"` `` occurs
exactly once in that file; verifies the source line is complete LF UTF-8 text
and has a Bash-compatible `SHELL` in effect before the insertion position; and
records only the source-authority evidence. The root coordinator/operator then
approves the exact author/committer names, emails, timestamps, unsigned state,
and UTF-8 LF commit-message bytes for the first patch. The named Luna FogCast
owner creates the candidate bootstrap from those two signed-off inputs with
`format = 1`, `schema = "fogcast.stage-a0-bootstrap"`, Recipe V1, and no
machine/target/credential/self-hash field, and with
`fogcast_base_revision = <C0>`. Vega checks the raw bootstrap bytes,
source-evidence calculation, C0 binding, and exact tool hash. This step does
not commit the bootstrap or clone Main. Set `STAGE_A0_BOOTSTRAP` to this
reviewed absolute candidate path in the FogCast authoring worktree; it is a
private operator path, never copied into the detached C0 worktree, never placed
in shared identity, and never logged in the handoff.

- [ ] **Step 3: Validate bootstrap, then create the already-authorized fork**

When the user selects implementation execution, the separately assigned Main
owner validates the reviewed bootstrap from the clean detached C0 worktree and
invokes exactly:

```sh
: "${STAGE_A0_BOOTSTRAP:?set the reviewed absolute bootstrap path}"
case "$STAGE_A0_BOOTSTRAP" in /*) ;; *) exit 2 ;; esac
cd "$STAGE_A0_C0_WORKTREE"
"$STAGE_A0_TOOL" bootstrap-verify --bootstrap "$STAGE_A0_BOOTSTRAP"
STAGE_A0_TOOL="$STAGE_A0_TOOL" scripts/stage-a0-init-main.sh \
  --bootstrap "$STAGE_A0_BOOTSTRAP" \
  --destination /Users/clawzai/Developer/Main_MiSTer
```

The local clone and its first deterministic VDATE commit are both covered by
the user's existing fork authorization. No later Main commit and no push is
authorized. If the destination already exists, the command only verifies it;
a mismatch stops unchanged. This invocation performs the plan's sole network
operation: the initializer's one official immutable HTTPS full-object fetch.

- [ ] **Step 4: Perform exact Main review and write the handoff**

Sol and Vega inspect the exact Main diff and record: C0 ID; raw bootstrap
SHA-256; upstream URL/commit/tree; source path and source-evidence SHA-256;
Recipe V1; first patch commit/tree/direct parent; author/committer metadata;
remote fetch/push effective URLs; clean worktree result; commands and exit
results; machine-observed Git IDs/hashes; exact C0 tree, Git/Go/tool versions
and tool SHA-256; reviewer role/model/fallback; and remaining risks. Run and
record these handoff checks:

```sh
git -C "$FOGCAST_AUTHORING_ROOT" diff --check
fogcast_status=$(git -C "$FOGCAST_AUTHORING_ROOT" status --short)
handoff="$FOGCAST_AUTHORING_ROOT/docs/stage-a0/local-main-fork-handoff-$BOOTSTRAP_SHA256.md"
plan="$FOGCAST_AUTHORING_ROOT/docs/superpowers/plans/2026-08-09-stage-a0-local-main-fork.md"
set -- "$STAGE_A0_BOOTSTRAP" "$handoff"
case "$fogcast_status" in *"?? docs/superpowers/plans/2026-08-09-stage-a0-local-main-fork.md"*) set -- "$@" "$plan";; esac
for file in "$@"; do
  code=0
  diagnostics=$(git diff --no-index --check /dev/null "$file" 2>&1) || code=$?
  test "$code" -eq 1 && test -z "$diagnostics"
  deliverable_sha=$(shasum -a 256 "$file" | cut -d ' ' -f1)
  printf 'deliverable_sha256=%s\n' "$deliverable_sha"
done
bootstrap_sha=$(shasum -a 256 "$STAGE_A0_BOOTSTRAP" | cut -d ' ' -f1)
handoff_sha=$(shasum -a 256 "$handoff" | cut -d ' ' -f1)
tool_sha=$(shasum -a 256 "$STAGE_A0_TOOL" | cut -d ' ' -f1)
printf 'bootstrap_sha256=%s\nhandoff_sha256=%s\ntool_sha256=%s\n' "$bootstrap_sha" "$handoff_sha" "$tool_sha"
gitleaks detect --no-git --source "$FOGCAST_AUTHORING_ROOT/internal/stagea0" --redact
gitleaks detect --no-git --source "$FOGCAST_AUTHORING_ROOT/cmd/stage-a0" --redact
gitleaks detect --no-git --source "$FOGCAST_AUTHORING_ROOT/scripts/stage-a0-init-main.sh" --redact
gitleaks detect --no-git --source "$FOGCAST_AUTHORING_ROOT/scripts/tests/stage-a0-init_test.sh" --redact
gitleaks detect --no-git --source "$STAGE_A0_BOOTSTRAP" --redact
gitleaks detect --no-git --source "$handoff" --redact
case "$fogcast_status" in *"?? docs/superpowers/plans/2026-08-09-stage-a0-local-main-fork.md"*) gitleaks detect --no-git --source "$plan" --redact;; esac
main_git() { env -i HOME="$STAGE_A0_MAIN_REVIEW_HOME" PATH="$(dirname "$GIT_PATH"):/usr/bin:/bin" GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null GIT_NO_REPLACE_OBJECTS=1 GIT_OPTIONAL_LOCKS=0 LC_ALL=C LANG=C TZ=UTC "$GIT_PATH" -C /Users/clawzai/Developer/Main_MiSTer "$@"; }
main_git fsck --no-dangling
main_git status --porcelain=v1
main_git cat-file -p "$PATCH_COMMIT"
main_git rev-parse "$PATCH_COMMIT^{tree}" "$PATCH_COMMIT^"
main_git diff --no-ext-diff --binary "$PATCH_COMMIT^" "$PATCH_COMMIT" > "$STAGE_A0_PRIVATE_DIFF"
private_diff_sha=$(shasum -a 256 "$STAGE_A0_PRIVATE_DIFF" | cut -d ' ' -f1)
printf 'private_diff_sha256=%s\n' "$private_diff_sha"
main_git remote get-url --all upstream
main_git remote get-url --push --all upstream
```

Record `fogcast_status` exactly. Its only permitted lines are
`?? build/stage-a0-bootstrap.toml`,
`?? docs/stage-a0/local-main-fork-handoff-<bootstrap-sha256>.md`, and, only if
the separate plan-document disposition remains uncommitted,
`?? docs/superpowers/plans/2026-08-09-stage-a0-local-main-fork.md`; every other
FogCast or sibling-Main status line fails the handoff. The absolute candidate
path stays out of the handoff; its raw SHA-256 is recorded instead. Record that
`GIT_NO_REPLACE_OBJECTS=1` was present, the
canonical `.git/index` path was used, and the `update-ref` old-value guard and
attached-HEAD verification succeeded.

Classify only host-side verification evidence; do not claim
build reproducibility, hardware behavior, HIL-observed, or Accepted status.
The maximum canonical status for this handoff is **Software-tested**.
The next safe action is the separately scoped locked-materials/final-lock plan.

## Follow-on scoped plans (non-executable here)

1. **Stage A0 locked materials and final lock** — closed material union,
source/cache verification, policies, and immutable lock inputs.
2. **Stage A0 isolated Linux/amd64 Main build** — locked toolchain/container,
detached source materialization, compiler trace, ELF/dependency inventory.
3. **Stage A0 immutable reports and two-build comparison** — payload copies,
canonical manifests, source-availability evidence, and the reproducibility
gate.

Those plans may consume the handoff identities above only after their own Sol,
Vega, authorization, and evidence gates. They are not tasks in this plan.

## Plan self-review

- Scope coverage: Tasks 1–2 deliver a host-testable parser, exact Recipe V1,
isolated Git initializer, CLI, wrapper, and full regression gate. Task 3 places
C0, source authority, the existing fork authorization, first commit, and review
in their required order without turning any operation into a current claim.
- Type consistency: all later tasks use the exact `Bootstrap`, `VDateSpec`,
`CommitSpec`, `Runner`, `InitRequest`, `ForkIdentity`, and function signatures
defined above. No cache, final-lock, build, report, or promotion API appears.
- Placeholder check: this document contains no deferred implementation wording,
ambiguous error-handling instruction, or cross-task shorthand.
- Rollback: FogCast remains unchanged until separately authorized C0; an
existing sibling destination is never altered on mismatch, and the disposable
target is out of scope.

## Execution handoff

Plan complete and saved to
`docs/superpowers/plans/2026-08-09-stage-a0-local-main-fork.md`. Two execution
options:

1. **Subagent-Driven (recommended)** — dispatch a fresh Luna task owner per
implementation task, obtain Sol review, then an independent Vega review.
2. **Inline Execution** — use `superpowers:executing-plans` with the same
review and authorization checkpoints.

The plan itself grants no new authority. It records that the user's existing
authorization covers the sibling-fork creation and its intrinsic first
deterministic VDATE commit; C0, later Main commits, every push, and any other
FogCast commit still require their stated explicit authorization gates.
