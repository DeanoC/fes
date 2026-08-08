# Architecture Survival Documentation Implementation Plan

**Status:** Implemented and independently reviewed on 2026-08-08; no commit,
deployment, or hardware operation was performed.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the approved portable-target-runtime architecture and Sol/Luna development process survive future sessions, agents, POC history, and repository growth.

**Architecture:** Add one canonical current architecture document, one active migration roadmap, and accepted ADRs derived from the approved design and its disposable-development-target qualification. Route every entry point and historical POC document to those authorities, then enforce the reading order, agent roles, evidence boundaries, and subtree-specific safety rules through top-level and scoped `AGENTS.md` files.

**Tech Stack:** Markdown, repository-local `AGENTS.md` instructions, Git, `rg`, shell-based content checks, and existing Go verification.

## Global Constraints

- Governing design: `docs/superpowers/specs/2026-08-08-fogcast-portable-target-runtime-design.md`.
- Preserve POC result/evidence facts; label historical documents and link forward instead of rewriting old evidence.
- `docs/ARCHITECTURE.md` is canonical current architecture; `docs/ROADMAP.md` is the active migration sequence; the ADR records why.
- `IDEA.md` remains product vision; `README.md` remains current capability/orientation.
- Sol and Luna are responsibilities with preferred models when available, not persistent personas or reasons to block work.
- Sol owns architecture decisions; Luna is the default implementation worker; Vega is the independent read-only reviewer; the root coordinator owns integration and HIL authority.
- One writer owns each file set. Reviewers do not edit the work they review.
- No commit or push without explicit user authorization. Target operations need
  explicit authorization except for the exact privately designated disposable
  local MiSTer Pi kit, whose standing authorization and limits are recorded by
  ADR 0002.
- No secrets, private target details, ROM data, local evidence artifacts, or credentials enter Git.
- Report exact verification results and anything not run.

---

### Task 1: Canonical Architecture, ADR, and Active Roadmap

**Files:**
- Create: `docs/ARCHITECTURE.md`
- Create: `docs/ROADMAP.md`
- Create: `docs/adr/0001-portable-target-runtime.md`
- Create: `docs/adr/0002-disposable-local-development-target.md`

**Interfaces:**
- Consumes: the governing design and accepted POC6 evidence boundary.
- Produces: canonical links and authority statements used by later tasks.

- [x] **Step 1: Create `docs/ARCHITECTURE.md`**

Include authority/status, current POC6 baseline, destination boundaries, target modes, resource ownership, public protocol/private IPC/C ABI separation, Linux-first portability, invariants, and links to the design, ADR, roadmap, results, and retained runbook.

- [x] **Step 2: Create ADR 0001**

Record status `Accepted`, date `2026-08-08`, context, decision, alternatives, consequences, superseded future guidance, and links. Historical POC1/POC2 decisions remain valid for their scope but no longer govern future architecture.

- [x] **Step 3: Create `docs/ROADMAP.md`**

Translate Stages A-E into active gates. Require the tracked POC6 rollback
lock/runbook before mutation of a non-exempt target. Record the exact ADR 0002
kit's physical rollback gate as not required, without weakening reproducibility,
comparison, HIL, or provenance gates. Distinguish designed, software-tested,
reproducible, HIL-observed, and accepted status. Claim no unsupported dates or
progress.

- [x] **Step 4: Verify and review Task 1**

Run:

```sh
rg -n '^#|Status|POC6|Main_MiSTer|libmister-runtime|Overlord|Linux|rollback|HIL|Accepted' docs/ARCHITECTURE.md docs/ROADMAP.md docs/adr/0001-portable-target-runtime.md
rg -n '\b(TBD|TODO|FIXME|XXX)\b' docs/ARCHITECTURE.md docs/ROADMAP.md docs/adr/0001-portable-target-runtime.md
git diff --check
```

Expected: required concepts are present, placeholder scan is empty, and diff check passes. Sol checks fidelity; Vega independently checks authority, contradictions, evidence boundaries, and links.

### Task 2: Durable Agent Governance

**Files:**
- Create: `AGENTS.md`
- Create: `docs/AGENTS.md`
- Create: `docs/superpowers/AGENTS.md`
- Create: `internal/AGENTS.md`
- Create: `buildroot/AGENTS.md`

**Interfaces:**
- Consumes: canonical documents from Task 1.
- Produces: instructions future root agents and scoped workers must follow.

- [x] **Step 1: Create top-level `AGENTS.md`**

Include mandatory reading order and authority, architecture capsule, safety/evidence invariants, Sol/Luna/Vega/coordinator/HIL-verifier roles, dispatch table, model fallback, worktree/file ownership, handoff contracts, escalation, verification, and Git/hardware limits.

- [x] **Step 2: Create documentation instructions**

`docs/AGENTS.md` classifies architecture/roadmap as normative, IDEA as vision, results/handoffs as historical evidence, old roadmaps as historical scope, and runbooks as operations. `docs/superpowers/AGENTS.md` marks old specs/plans historical unless explicitly current and prohibits blindly executing old checklists.

- [x] **Step 3: Create implementation and image instructions**

`internal/AGENTS.md` protects Go package/API/privacy/lifecycle boundaries and race testing. `buildroot/AGENTS.md` protects locks, reproducibility, provenance, rollback, dev/prod separation, secrets, and physical-evidence boundaries.

- [x] **Step 4: Verify and review Task 2**

Run:

```sh
for f in AGENTS.md docs/AGENTS.md docs/superpowers/AGENTS.md internal/AGENTS.md buildroot/AGENTS.md; do test -s "$f"; done
rg -n 'Sol|Luna|Vega|worktree|owner|review|HIL|commit|push|hardware' AGENTS.md
rg -n 'historical|evidence|normative|ARCHITECTURE|ROADMAP' docs/AGENTS.md docs/superpowers/AGENTS.md
git diff --check
```

Expected: all files exist, governance/classification terms are present, and diff check passes. Vega checks executability and scope; Sol checks escalation triggers.

### Task 3: Entry Points and POC6 Forward Links

**Files:**
- Modify: `README.md`
- Modify: `IDEA.md`
- Modify: `docs/POC6-RESULTS.md`
- Modify: `docs/POC6-ROADMAP.md`
- Modify: `docs/POC6-DEVELOPMENT.md`

**Interfaces:**
- Consumes: canonical paths and authority language from Tasks 1-2.
- Produces: orientation from the root and retained POC6 testbed.

- [x] **Step 1: Update README and IDEA**

README links vision, canonical architecture, active roadmap, and POC6 evidence near the opening. It labels external FFmpeg, SSH development access, `/dev/fb0`, and the disposable Main hook as retained testbed mechanisms. IDEA receives prose corrections and the appliance/portability vision without becoming an implementation spec.

- [x] **Step 2: Banner and link POC6 documents**

Mark roadmap as completed/historical scope, results as accepted evidence, and development guide as retained-testbed operations. Link current architecture and roadmap. Preserve all measurements, hashes, limits, and accepted/deferred claims.

- [x] **Step 3: Qualify stale next-step language**

Where POC6 says the next choice is only UX versus controller work or that all intelligence stays on the host, add historical qualification and the current split: host owns catalog/policy/user intent; target owns local mechanisms, arbitration, observed state, and recovery.

- [x] **Step 4: Verify and review Task 3**

Run:

```sh
rg -n 'ARCHITECTURE.md|ROADMAP.md|historical|retained|testbed|destination' README.md IDEA.md docs/POC6-RESULTS.md docs/POC6-ROADMAP.md docs/POC6-DEVELOPMENT.md
rg -n 'sha256|packet_errors|glass-to-glass|issue #3' docs/POC6-RESULTS.md
git diff --check
```

Expected: every entry links forward, evidence anchors remain, and diff check passes. Vega checks that no historical fact or accepted claim changed silently.

### Task 4: Historical Guardrails and Final Verification

**Files:**
- Create: `docs/superpowers/README.md`
- Modify: `docs/superpowers/specs/2026-08-01-mister-remote-poc1-design.md`
- Modify: `docs/superpowers/specs/2026-08-02-fogcast-poc2-design.md`
- Modify: `docs/superpowers/plans/2026-08-01-mister-remote-poc1b.md`
- Modify: `docs/superpowers/plans/2026-08-02-fogcast-poc2.md`

**Interfaces:**
- Consumes: documentation authority from Tasks 1-2.
- Produces: direct-link protection against obsolete POC decisions being treated as current.

- [x] **Step 1: Add the history index and narrow banners**

Explain that specs/plans record historical reasoning and execution, identify current governing documents, and prohibit executing old checklists without current adoption. Add a short banner to the four highest-risk old documents linking `../../ARCHITECTURE.md` and `../../ROADMAP.md`; do not alter their bodies.

- [x] **Step 2: Run full verification**

Run:

```sh
rg -n '\b(TBD|TODO|FIXME|XXX)\b' AGENTS.md docs/ARCHITECTURE.md docs/ROADMAP.md docs/adr docs/superpowers/README.md docs/superpowers/AGENTS.md
git diff --check
git status --short --branch
env -u PYTHONHOME -u PYTHONPATH mise exec go@1.26.5 -- go test ./...
```

Expected: no placeholders in authoritative docs, diff check and Go tests pass, and status contains only intended documentation changes.

- [x] **Step 3: Inspect and review the complete diff**

Run:

```sh
git diff --stat
git diff -- AGENTS.md README.md IDEA.md docs internal/AGENTS.md buildroot/AGENTS.md
```

Confirm no evidence hashes/numbers changed unintentionally, no secrets/private paths were added, links are correct, and scoped instructions do not conflict. Sol checks architecture; Vega reviews the complete uncommitted diff. Resolve all Critical and Important findings.

- [x] **Step 4: Report without committing**

Report summary, files changed, exact verification commands/results, remaining risks, worktree, and branch. Do not stage, commit, push, deploy, or mutate hardware.
