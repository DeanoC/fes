# Stage A0 Raw Checkout Isolation Fix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:subagent-driven-development and superpowers:test-driven-development
> task by task.

**Goal:** Materialize the locked Main tree byte-for-byte while preserving a
clean ordinary worktree and a separately audited locked-tree attribute policy.

**Architecture:** Configure the local fork with Git's canonical empty SHA-1
tree as `attr.tree` for every persistent worktree operation. Inspect the locked
patch tree's effective VDATE-source attributes only through one command whose
complete environment adds `GIT_ATTR_SOURCE=<patch-tree>`. No source file other
than `Makefile` changes.

**Governing evidence:** Locked `lib/miniz/ChangeLog.md` is mode `100644`, blob
`3ee292d7996d902ab26ee9507a4499cbc34c96e5`, 15,807 bytes, raw SHA-256
`f6e5947c7da9eb3720a3f0abc6cf9bf30fc4b501686159b30f570c83fc8f3874`,
with 176 CRLF endings. Its effective locked-tree attributes are exactly
`text=set`, `eol=lf`. Git 2.55 independently reproduced the dirty checkout and
proved the isolation design.

## Constraints

- Preserve immutable source authority, the exact Recipe V1 transformation,
  deterministic commit identity, single-fetch/no-push policy, atomic sibling
  publication, and one-path parent-to-patch diff.
- For a new repository, write an empty tree deterministically, require the
  resulting OID to be the exact canonical empty SHA-1 tree
  `4b825dc642cb6eb9a060e54bf8d69288fbee4904`; do not rely on Git's fallback for
  an invalid `attr.tree` value.
- Require SHA-1 object format and functionally prove persistent attributes are
  empty while command-scoped locked-tree attributes remain observable.
- Reject any `.git/info/attributes` file, directory, or symlink and any
  unexpected or differently valued local configuration.
- Never add `GIT_ATTR_SOURCE` to the ordinary initializer/read-only Git
  environments. Construct a fresh audit environment for the one audit command.
- Remove the temporary `official_dirty_diagnostic_test.go` before the
  replacement C0; it is not a deliverable.
- Preserve prior C0 commits. The reviewed fix becomes a new descendant and the
  bootstrap changes only its `fogcast_base_revision`.
- This is host-only source tooling. It does not resolve, mutate, deploy to, or
  require rollback for a target; ADR 0002 and the Stage A rollback gate are not
  exercised by this fix.

## Ownership and handoff

- Root coordinator owns scope, documentation disposition, commits, integration,
  bootstrap rebinding, and final evidence. The exact writable authoring
  worktree is
  `/Users/clawzai/Developer/mister-remote/.worktrees/stage-a0-main-baseline-design`.
- One Luna implementation owner may modify only `internal/stagea0/git.go` and
  `internal/stagea0/git_test.go` and remove the temporary diagnostic test in
  that worktree. No other worker writes those files concurrently.
- Sol and Vega are read-only architecture and quality reviewers and do not
  review their own changes.
- After replacement C0 review, the designated Luna Main owner may write only
  `/Users/clawzai/Developer/Main_MiSTer` through the reviewed initializer. It
  stops on any mismatch and performs no repair or push.
- Each handoff records base/branch/worktree, governing decision, exact diff,
  commands/results, reviewer/model, evidence class, unresolved risk, and next
  safe action. The final local-fork handoff includes the same fields and the
  immutable identities/hashes required by the parent plan.

## Task 1: RED fixtures for raw checkout isolation

**Files:** modify `internal/stagea0/git_test.go`; remove
`internal/stagea0/official_dirty_diagnostic_test.go`.

1. Add a real-Git fixture with root `.gitattributes` containing
   `*.md text eol=lf`, a tracked CRLF `lib/miniz/ChangeLog.md`, and the normal
   LF-only `Makefile` recipe source.
2. Require initialization to preserve the ChangeLog's exact raw SHA-256/blob,
   leave ordinary `check-attr --all` empty, recover the Makefile's exact
   `text=set`/`eol=lf` records with the patch tree as command-scoped attribute
   source, and return empty porcelain immediately. Explicitly invalidate the
   fixture's cached stat metadata, require Git to restat/rehash it, require
   empty porcelain again, repeat `checkout-index`, and require empty porcelain
   and raw hash identity before the exact initializer rerun.
3. Add hostile fixtures for a noncanonical/missing or nonempty `attr.tree`, a
   non-SHA-1 object format response, `.git/info/attributes`, and audit-source
   leakage into ordinary commands. Require failure before publication.
4. Extend the exact command transcript, exact environment assertions, and
   closed-config tests for the new config/capability/audit operations.
5. Run the focused tests and record RED against C0 `aeecab26f1ed70a5a28c7d3b715591b33ca8f4c8`.

   ```sh
   mise exec go@1.26.5 -- go test ./internal/stagea0 \
     -run 'Test(ExecRunnerPreservesRawAttributedCRLF|RawAttributeIsolation|ExistingForkRawAttributePolicy|InitializeForkAbsentDestinationUsesExactOrderedTranscript)' \
     -count=1 -v
   ```

   Expected: FAIL because the managed `attr.tree`, canonical-object gates, and
   command-scoped audit do not exist.

## Task 2: Minimal initializer implementation

**Files:** modify `internal/stagea0/git.go` and the Task 1 test file only.

1. Add the canonical empty-tree constant and exact `attr.tree` managed config.
2. Add fail-closed helpers that verify `rev-parse --show-object-format` is
   `sha1`; for a new repository write empty stdin with
   `hash-object -w -t tree --stdin` and require the exact canonical OID; then
   require `cat-file -e`, `cat-file -t` = `tree`, `ls-tree -z` empty, and
   `.git/info/attributes` is absent without following symlinks.
3. After reading the patch tree into the index, require ordinary
   `check-attr --all -- <source_path>` to be empty. Then run the existing exact
   validator through one command using a copied base environment plus only
   `GIT_ATTR_SOURCE=<patch-tree>`. Do not use `--cached` as source authority.
4. Keep `checkout-index --all` and post-checkout raw Makefile SHA-256 equality.
   After checkout, rerun ordinary `check-attr --all -- <source_path>` and
   require empty output before clean-status/final fork verification. This is
   the functional `attr.tree` capability gate once `.gitattributes` exists.
5. Apply every new verification to the existing-destination path under its
   before/after snapshot. It must require the exact config, SHA-1 format,
   already-existing canonical object/type/zero entries, absent
   `.git/info/attributes`, empty persistent attributes, and the scoped
   locked-tree audit without writing or repairing anything.
6. Verify focused tests, `make stage-a0-check`, `make test`, `make vet`,
   `git diff --check`, and explicit no-index checks for untracked deliverables.
   The focused GREEN command is the exact Task 1 command above.
7. Obtain independent Sol architecture/spec review and Vega quality review;
   resolve every Critical or Important finding.

## Task 3: New replacement C0 and fork retry

1. Before implementation C0, independently review and commit the design/spec,
   parent-plan update, and this untracked execution plan as a documentation-only
   commit. Record the untracked plan's full SHA-256 and preserve the historical
   implementation-only C0 allowlist.
2. Stage only the reviewed Go implementation/test files and commit a new
   replacement C0 without amending history.
3. Build and hash the tool from a clean detached worktree at that exact commit.
4. Change only `fogcast_base_revision` in the bootstrap; validate and
   independently review the resulting bytes.
5. Require the sibling destination absent, retry the official immutable HTTPS
   initializer, review the exact one-path commit/tree/config/worktree, and run
   the initializer a second time to prove idempotence.
6. Write and independently review the hashed local-fork handoff, including
   unresolved risks and the next safe Stage A action. Do not push.
