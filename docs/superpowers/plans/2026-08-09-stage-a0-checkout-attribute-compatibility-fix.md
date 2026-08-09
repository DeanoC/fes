# Stage A0 Checkout-Attribute Compatibility Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Admit the selected official Main tree's exact byte-preserving
`Makefile` checkout attributes without weakening Stage A0's fail-closed source
materialization boundary.

**Architecture:** Isolate attribute admission in one pure validator called
immediately after `git check-attr --cached --all` and before
`checkout-index`. It accepts an empty result or exactly `text=set` followed by
`eol=lf` for the locked source path, and admits the pair only for valid CR-free
LF source bytes ending in LF. The existing post-checkout SHA-256 equality gate
remains the final byte-identity proof.

**Tech Stack:** Go 1.26.5, Git plumbing, synthetic local bare Git fixtures,
GNU Make, POSIX shell.

## Global Constraints

- Work only in the Stage A0 authoring worktree until the replacement C0 is
  independently approved.
- The attribute policy is not extensible configuration: no caller-provided
  attribute names or values and no wildcard admission.
- Reject reversed, duplicate, additional, malformed, differently valued, or
  differently pathed records before `checkout-index`.
- Preserve the exact VDATE-only source transformation, closed Git environment,
  single immutable HTTPS fetch, no-push policy, and post-checkout SHA-256 gate.
- The sibling destination must remain absent until a replacement C0 tool and
  bootstrap pass independent review.
- User standing authorization covers required documentation, replacement C0,
  bootstrap regeneration, sibling creation, intrinsic first Main commit, and
  review operations; it does not authorize a Main push or later Main commit.

---

### Task 1: Exact checkout-attribute admission

**Files:**

- Modify: `internal/stagea0/git.go`
- Modify: `internal/stagea0/git_test.go`

**Interfaces:**

- Consumes: `Bootstrap.VDate.SourcePath`, transformed raw source bytes, and
  the exact stdout from `git check-attr --cached --all -- <source_path>`.
- Produces: `verifyCheckoutAttributes(sourcePath string, source, output []byte) error`,
  returning `CodeRepositoryPolicyMismatch` for every non-admitted result.

- [ ] **Step 1: Write the failing pure-policy table test**

Add `TestVerifyCheckoutAttributesUsesClosedOfficialPolicy`. Its valid source is
`[]byte("SHELL = /bin/bash\nDFLAGS = x\n")`. Require success for empty output
and exactly:

```text
Makefile: text: set
Makefile: eol: lf
```

Require `CodeRepositoryPolicyMismatch` for reversed records, a duplicate,
an extra attribute, `text=auto`, `eol=crlf`, a different path, malformed/no
terminal LF output, the exact pair with CRLF source, the exact pair with a CR
inside a line, and the exact pair with source missing terminal LF.

- [ ] **Step 2: Run the focused test and record RED**

Run:

```sh
mise exec go@1.26.5 -- go test ./internal/stagea0 -run TestVerifyCheckoutAttributesUsesClosedOfficialPolicy -count=1 -v
```

Expected: FAIL because `verifyCheckoutAttributes` does not exist.

- [ ] **Step 3: Write the failing real-Git fixture test**

Add `TestExecRunnerAdmitsOfficialMakefileAttributes`. Extend a local upstream
fixture with root `.gitattributes` containing exactly:

```gitattributes
* text=auto eol=lf
Makefile text eol=lf
```

Keep the locked `Makefile` raw bytes CR-free LF with terminal LF. Run
`InitializeFork`, require success, require the observed cached attributes to be
the exact two admitted records, and compare the materialized `Makefile`
SHA-256 to `HEAD:Makefile`. Add a sibling fixture with `Makefile text eol=crlf`
and require rejection before `checkout-index` and no published destination.

- [ ] **Step 4: Run the integration tests and record RED**

Run:

```sh
mise exec go@1.26.5 -- go test ./internal/stagea0 -run 'Test(VerifyCheckoutAttributes|ExecRunnerAdmitsOfficialMakefileAttributes)' -count=1 -v
```

Expected: FAIL at the existing blanket non-empty attribute rejection.

- [ ] **Step 5: Implement the minimal pure validator and wire it before checkout**

Implement the closed comparison:

```go
func verifyCheckoutAttributes(sourcePath string, source, output []byte) error {
	if len(output) == 0 {
		return nil
	}
	want := []byte(sourcePath + ": text: set\n" + sourcePath + ": eol: lf\n")
	if !bytes.Equal(output, want) || !utf8.Valid(source) || bytes.IndexByte(source, '\r') >= 0 || !bytes.HasSuffix(source, []byte("\n")) {
		return repositoryMismatch("source has checkout-altering attributes")
	}
	return nil
}
```

Import `unicode/utf8`. Replace the blanket `strings.TrimSpace(attrs) != ""`
condition with a call that passes the raw command stdout and transformed source
bytes. Do not trim, split, sort, or normalize attribute output.

- [ ] **Step 6: Run focused and full verification**

Run:

```sh
mise exec go@1.26.5 -- go test ./internal/stagea0 -run 'Test(VerifyCheckoutAttributes|ExecRunnerAdmitsOfficialMakefileAttributes)' -count=1 -v
make stage-a0-check
make test
make vet
git diff --check
```

Expected: PASS.

- [ ] **Step 7: Independent Sol/Vega review**

Sol checks the implementation against immutable official `.gitattributes`
blob `0bd9e7376ace40b343b10e175b6ec5afe3cf72ea` and raw SHA-256
`414d7713e26b7e7f8ae586e0a0c205a28e7b7ae7851828408de016096b4252da`.
Vega checks the exact diff, RED/GREEN evidence, hostile table, integration
fixture, and preservation of the post-checkout SHA-256 equality gate. Resolve
all Critical and Important findings.

### Task 2: Replacement C0, bootstrap rebind, and authorized fork retry

**Files:**

- Modify: `build/stage-a0-bootstrap.toml` only to replace
  `fogcast_base_revision` after the replacement C0 exists.
- Create after successful fork: one hashed handoff under `docs/stage-a0/` as
  specified by the parent Stage A0 plan.

**Interfaces:**

- Consumes: independently approved Task 1 diff, original immutable Main
  commit/tree/source authority, original approved deterministic Main commit
  identity, and the exact replacement C0 commit/tool hashes.
- Produces: clean detached replacement-C0 tool, re-reviewed bootstrap,
  `/Users/clawzai/Developer/Main_MiSTer`, intrinsic VDATE patch commit/tree,
  and the Stage A0 local-fork handoff.

- [ ] **Step 1: Commit the independently approved implementation as replacement C0**

Stage only `internal/stagea0/git.go` and `internal/stagea0/git_test.go`. Commit
with message `fix: admit locked LF checkout attributes`. Record the full
commit/tree IDs and exact two-file diff. Do not amend or delete the historical
failed-attempt C0; replacement C0 is a new descendant and the audit trail stays
intact.

- [ ] **Step 2: Build and fingerprint from a clean detached replacement C0**

Create a new detached worktree, build `cmd/stage-a0` with Go 1.26.5 using
`-buildvcs=false -trimpath`, and record C0/tree, Git/Go versions, and tool
SHA-256. Require the detached worktree clean.

- [ ] **Step 3: Rebind and re-review the bootstrap**

Change only `fogcast_base_revision` in the existing bootstrap. Require its raw
SHA-256 to change, validate it from the replacement C0 tool, and independently
review that every Main/source/recipe/commit-identity field stayed byte-for-byte
equivalent apart from the C0 value.

- [ ] **Step 4: Retry the already-authorized sibling initialization**

Require `/Users/clawzai/Developer/Main_MiSTer` absent, run
`bootstrap-verify`, then the exact wrapper from the replacement detached C0.
The initializer performs the immutable official HTTPS fetch and creates only
the intrinsic VDATE commit. Do not push or create another Main commit.

- [ ] **Step 5: Review, rerun idempotently, and write the handoff**

Run the parent plan's closed-environment review commands. Require the exact
upstream parent/tree, one-path VDATE diff, exact raw patch commit, clean worktree,
sole disabled-push `upstream` remote, materialized-versus-committed SHA-256
equality, and byte-identical snapshot on a second initializer invocation.
Write the hashed handoff and obtain independent PASS/APPROVED review.
