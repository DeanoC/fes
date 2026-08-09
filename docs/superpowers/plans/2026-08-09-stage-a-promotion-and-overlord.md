# Stage A Promotion and Overlord Handoff Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the current Stage A0 candidate evidence into a deterministic promotion decision and a pinned, fail-closed handoff for the Overlord DE10-Nano/Cyclone V slice.

**Architecture:** The promotion report is an evidence classifier, not a lock editor: it consumes the candidate lock, six policy bytes, material catalog, and two-build comparison, then emits a canonical report without upgrading evidence status. The Overlord handoff records exact external repository identities and compares the requested board/resource capabilities against the checked-out catalogs; missing DE10-Nano/Cyclone V resources remain an explicit blocker rather than being inferred.

**Tech Stack:** Go 1.26.5, canonical JSON, existing Stage A0 TOML/JSON schemas, pinned Git repositories, shell-level provenance checks.

## Global Constraints

- Preserve the roadmap vocabulary exactly: Designed, Software-tested, Reproducible, HIL-observed, Accepted.
- Never promote candidate policy/material evidence solely because automated syntax checks pass.
- Do not record target addresses, credentials, private paths, or ROM/user content.
- Keep `Main_MiSTer` as the GPLv3 compatibility and rollback source.
- Use the existing local-only status until durable retrieval and the stated reproducibility gate are proven.
- Do not make Overlord mandatory for the retained POC6 path before its narrow slice passes.

---

### Task 1: Canonical Stage A0 promotion decision report

**Files:**
- Create: `internal/stagea0/promotion/report.go`
- Create: `internal/stagea0/promotion/report_test.go`
- Create: `cmd/stage-a0-promotion-report/main.go`
- Create: `cmd/stage-a0-promotion-report/main_test.go`
- Modify: `Makefile`

**Interfaces:**
- Consumes candidate lock bytes, a six-file policy inventory, candidate `materials.json`, and a canonical precompare report.
- Produces `fogcast.stage-a0.promotion-report.v1` canonical JSON with sorted checks and blockers. V1 is deliberately a blocked-only candidate report; a later promoted-evidence schema must add any eligible decision.

- [x] **Step 1: Write the failing report tests.**

  Cover a blocked candidate lock, a valid synthetic lock with incomplete policies, canonical encode/decode, deterministic check ordering, and rejection of non-canonical report JSON.

- [x] **Step 2: Run the focused tests and confirm they fail because the report API is absent.**

  Run: `mise exec go@1.26.5 -- go test ./internal/stagea0/promotion ./cmd/stage-a0-promotion-report`

- [x] **Step 3: Implement the report evaluator and canonical JSON codec.**

  The evaluator must keep the decision blocked when lock parsing, policy promotion, material status, or comparison status is incomplete. It must never rewrite the lock or turn `candidate-observed` into `complete`.

- [x] **Step 4: Implement the CLI with atomic output and stable exit codes.**

  Exit `1` for the valid blocked candidate decision and `2` for malformed arguments/input. Do not include physical paths in the JSON report; an eligible decision belongs to a later promoted-evidence schema.

- [x] **Step 5: Add the Makefile target and run focused tests.**

  Run: `mise exec go@1.26.5 -- go test ./internal/stagea0/promotion ./cmd/stage-a0-promotion-report`

---

### Task 2: Produce the reviewed candidate promotion record

**Files:**
- Create: `docs/stage-a0/main-lock-reviews/<candidate-lock-sha256>.md`
- Modify: `docs/stage-a0/main-lock-candidate-handoff-2026-08-09.md`
- Modify: `docs/ROADMAP.md`

**Interfaces:**
- Consumes the current candidate lock, the generated promotion report, two fresh adapter captures, and the pinned external repository observations.
- Produces a hash-named immutable candidate review record; it must say blocked/local-only and list exact next gates.

- [x] **Step 1: Regenerate the six policy candidates and `materials.json` from the retained reviewed capture.**
- [x] **Step 2: Run the promotion report against the candidate lock, candidates, material catalog, and precompare report.**
- [x] **Step 3: Hash the raw candidate lock and write the review record with commands, evidence status, reviewer roles, and unresolved blockers.**
- [x] **Step 4: Verify all new documentation links and no-index whitespace/hash checks.**

---

### Task 3: Pinned Overlord vertical-slice handoff

**Files:**
- Create: `build/stage-a0-overlord.lock.toml`
- Create: `docs/stage-a0/overlord-slice-handoff-2026-08-09.md`
- Create: `scripts/stage-a0-overlord-probe.sh`
- Create: `scripts/tests/stage-a0-overlord-probe_test.sh`

**Interfaces:**
- Consumes pinned `DeanoC/overlord` and `DeanoC/ikuy_std_resources` checkouts plus the existing Main hardware/build evidence.
- Produces a deterministic capability report and exits non-zero when the required DE10-Nano/Cyclone V definitions are absent.

- [x] **Step 1: Write the probe test for both the expected missing-resource result and a synthetic complete catalog fixture.**
- [x] **Step 2: Run the test to confirm the probe is absent/fails.**
- [x] **Step 3: Implement the probe with no network access and no private paths in output.**
- [x] **Step 4: Pin the observed Overlord/resource commits and record the missing-resource result.**
- [x] **Step 5: Run the probe and shell/no-index checks.**

---

### Task 4: Integrated verification and handoff

**Files:**
- Modify: `docs/stage-a0/first-build-precompare-2026-08-09.md`
- Modify: `docs/stage-a0/main-lock-candidate-handoff-2026-08-09.md`

- [x] **Step 1: Run focused Go tests, race tests, vet, formatting, and the promotion/probe commands.**
- [x] **Step 2: Run the complete `make stage-a0-check` and `mise exec go@1.26.5 -- go test ./...`.**
- [x] **Step 3: Inspect `git diff`, `git status`, and all untracked deliverable hashes.**
- [x] **Step 4: Record the exact evidence classification and stop at the next major external gate; do not claim Stage A complete.**

## Completion audit

This plan intentionally does not mark Stage A complete by itself. Completion still requires a promoted lock, durable source/material retrieval, two independent clean builds from that lock, the Overlord-generated DE10-Nano/Cyclone V slice, and the separately recorded HIL/compatibility evidence required by `docs/ROADMAP.md`.
