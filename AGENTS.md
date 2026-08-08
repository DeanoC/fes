# FogCast working agreement

## Start here

Before changing this repository, read every applicable `AGENTS.md` from the
repository root down to the target path, in order. A narrower file adds to or
authoritatively overrides a broader one only within its own scope. Then read
`docs/ARCHITECTURE.md`, `docs/ROADMAP.md`, and
`docs/adr/0001-portable-target-runtime.md`. Read
`docs/adr/0002-disposable-local-development-target.md` before target operations,
and read the portable-runtime design
when a change touches target architecture, lifecycle, hardware, portability,
or migration gates. The user's current instruction controls scope. Accepted
evidence overrides plans; an approved architecture decision overrides older
forward-looking guidance. Target authorization follows the operator policy
below; architecture or planning documents do not independently grant it.

## Architecture and safety capsule

Keep the public versioned host/target protocol independent of `Main_MiSTer`,
Linux paths/devices, codecs, and target process layout. The target-native
coordinator is the sole owner of hardware-mode transitions; every launch has a
fresh `(session, generation)`, and compatibility Main and headless runtime
never simultaneously own hardware. Keep Linux-specific behavior behind
platform interfaces, preserve the GPLv3 Main fork as the comparison/rollback
path, and do not make Overlord mandatory until its narrow Linux slice passes
the stated gates.

Do not broaden POC6 claims. Software-tested, Reproducible, HIL-observed, and
Accepted are distinct evidence states. Reports label commands run,
machine-observed counters, operator observations, and inferences separately,
with source/artifact hashes and provenance. Physical FPGA, HDMI, audio, input,
save, recovery, and latency claims require HIL evidence. Never put credentials
in arguments, writable shared paths, logs, manifests, or source control.

Hardware is operator-controlled. The user grants standing authorization for
full project access to, deployment to, reboot, software/image/configuration and
credential replacement, wipe, and rebuild of the designated local disposable
MiSTer Pi development kit. Those operations need no repeated confirmation,
rollback lock/runbook, or preservation of physical state. Before a destructive
action, resolve the kit only through operator-controlled private configuration
and verify its exact target identity. Never infer designation from device type,
hostname, IP address, or discovery, and never record its private identity or
secrets. Other and production targets require explicit authorization plus
applicable rollback and hardening controls. The standing grant never relaxes
lifecycle/reconciliation, evidence classification, or artifact provenance
gates. See `docs/adr/0002-disposable-local-development-target.md`.

## Roles and dispatch

| Role | Use for | Writable scope |
| --- | --- | --- |
| Root coordinator | Scope, integration, user communication, final evidence | Assigns disjoint ownership; validates integration |
| Luna | Normal approved-boundary implementation, tests, refactors, docs | Explicit, disjoint file set only |
| Sol | Architecture, interfaces, lifecycle/ownership, portability, security decisions | Read-only by default; writes only when expressly assigned disjoint files |
| Vega | Independent review | Read-only; must not review its own change |
| HIL verifier | Physical evidence classification and observation | Verification context only; cannot authorize mutation and receives no secrets |

Model mapping is durable: Sol prefers `gpt-5.6-sol`; Luna prefers
`gpt-5.6-luna`, otherwise the implementation fallback `gpt-5.6-terra`; and
Vega is independent and uses the strongest suitable
review/reasoning model, normally `gpt-5.6-sol`. Disclose the actual model
identifier and fallback reason in the handoff/review record. A fallback never
relaxes scope, ownership, or review requirements.

Dispatch bounded tasks with inputs, outputs, owner, files, and verification.
Keep teams flat; do not overlap writable files. Use read-only workers for
reconnaissance, planning, architecture, and review. Use Luna for ordinary work;
escalate to Sol before changing an approved architecture, a cross-boundary
contract, lifecycle/resource ownership, portability/security invariant, or
after a repeated blocker. Escalate uncertain evidence to the coordinator/HIL
verifier rather than inferring acceptance.

## Execution, review, and handoff

Use a Git worktree for substantial or parallel work. One owner per writable
worktree and file set. Preserve unrelated user changes. Run the narrowest
relevant checks, then applicable formatting, lint, type, race, provenance, and
reproducibility checks. Before handoff, run `git diff --check`, inspect `git
status`, and run an explicit full-file no-index whitespace check for every
untracked deliverable, for example:

```sh
for f in <untracked-deliverables>; do
  code=0
  result=$(git diff --no-index --check /dev/null "$f" 2>&1) || code=$?
  { test "$code" -eq 1 && test -z "$result"; } || exit 1
done
shasum -a 256 <untracked-deliverables>
```

`git diff --check` does not inspect untracked files; the no-index checks and
full SHA-256 package hashes are required to identify and validate them. Every
implementation milestone gets an independent Vega review;
architecture, concurrency, security, protocol, and hardware-boundary changes
need a high-reasoning review. Resolve Critical and Important findings before
integration.

Each handoff/review records: base commit; branch/worktree; governing decision;
files and artifacts changed; commands and results; evidence classification;
reviewer role/model/fallback; unresolved risks; and next safe action. Reviewers
also record the exact diff inspected and dispositions of Critical/Important
findings. Durable decisions belong in repository documents, not chat alone.

Do not commit, push, open a pull request, stage, or otherwise publish changes
without explicit user authorization. Luna may routinely regenerate locks,
hashes, and generated artifacts within an approved gate when provenance, tests,
and review are recorded. Escalate to Sol and obtain an approved decision only
when changing gate semantics, architecture or a cross-boundary contract,
dependency/source authority, image-layout/security/rollback invariant, or when
reproducibility fails without explanation. Do not promote a historical library
without its required provenance, license/configuration record, and verification.
