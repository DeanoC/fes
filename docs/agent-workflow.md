# Working with agents through FES

Every agent starts with [AGENTS.md](../AGENTS.md),
[the project map](project-map.md) and the owning module's instructions. The
first-party modules under `sources/` share the FES repository and commit history.
Keep their architectural ownership separate while making cross-module changes
in one branch and PR.

## Repository retirement

The former DeanoC/FogCast, DeanoC/libmister-runtime, DeanoC/mister-packages
and DeanoC/misteross repositories are archived. All development and PRs go to
DeanoC/fes. Module names describe ownership, not separate repositories.

Do not resume old component worktrees as active development. Preserve unique
commits or edits, then port the required change into a current FES worktree.
Old orchestrator handoffs and parent-pin instructions are superseded by this guide.

Historical import provenance, dated evidence, immutable artifact URLs and Go
module import paths may retain the old repository names. These are not clone
instructions or development destinations. Yosys, nextpnr and Mistral remain
independent compiler dependencies.

## 1. Route the task

| Example task | Primary owner | Likely coordination |
| --- | --- | --- |
| Browser UI, game library or host API | FogCast | Target agent only if a contract changes |
| Target HTTP requests, content transfer or session coordination | FogCast agent | Runtime for physical operations |
| FPGA programming, media/input delivery or recovery | libmister-runtime | Agent for exposed protocol changes |
| Shared wire definitions or generated constants | mister-packages | Every affected consumer |
| Core RTL, simulation or package production | misteross | Shared definitions and FES artifact selection |
| Image assembly, cache policy or integration evidence | FES | The affected modules |

A cross-module change needs one integrator to reconcile contracts, generated
consumers and validation. Separate workers may implement independent scopes;
delegation is useful when work is genuinely independent, not a requirement.

## 2. Assign a bounded scope

```text
Task: <concrete resulting behavior>
Owner: <logical module and exact files/directories>
Base: <FES commit>
Worktree: <absolute FES task worktree>
Interface: <contracts coordinated with other workers>
Validation: <focused tests and integration checks>
Handoff: <commit or diff, evidence and remaining work>
```

Agree file ownership before concurrent edits. Workers sharing a FES task
worktree must not edit overlapping files independently. Separate tasks use
separate FES worktrees, including when both tasks affect the same module.

## 3. Work in one repository

Use the [FES worktree commands](development.md#isolate-component-work). Edit the
owning module's tracked files there. There is no separate child checkout or
internal gitlink update for each implementation change. Do not edit generated
build snapshots under `out/work/`.

Run focused module tests during iteration. The
[affected test command](test-changed.md) includes working-tree edits and dependent
consumers; runtime changes also run host protocol tests, and shared contracts
select all consumers. Use `make generate` for mapped definitions and fixtures,
then inspect the diff and run `make check-generated`. Canonical source definitions
and runtime serializer fixtures remain owned by their respective modules.

Parent builds require committed module sources. Their disposable snapshots
retain the real FES commit, module path and tree identity. Do not claim that a
parent build tested an uncommitted edit, or that a reused FPGA package was built
from the current commit: its original manifest and the separate selection
provenance receipt identify the actual build and reuse.

## 4. Integrate once

Reconcile worker diffs in the task's FES branch, review the shared contracts and
consumers, and make one compatible commit. No component SHA-only
PRs are needed. FES owns `image/build/native-inputs.toml` and derives the concrete
runtime assembly lock inside disposable build snapshots.

Run `make check`, then the appropriate host or incremental image build. Reserve
cold `make build` / `make verify` for stabilized integration. Shared compiler and
artifact caches default to the primary FES checkout's `out/cache`; an absolute
`FES_CACHE_ROOT` selects a different stable location. Each worktree retains its
own mutable outputs. One parent build operates in a checkout at a time; do not
have every worker rebuild the full image or run the same expensive simulation.

Ordinary kit tests require the existing target lease. Coordinate disruptive
maintenance under [kit sharing](kit-sharing.md), preserve the exact device
qualification and authorization, and report cleanup and release. A passing
software test or build does not authorize deployment to another device.

Commit, push, and open PRs as needed for the requested work without additional
approval. Only merging a PR requires review by Deano or by another agent or
model reviewer (for example a Codex review or a domain-reviewer agent). Agents
never merge on their own authority. The imported source
histories and original URLs are recorded in
[config/source-imports.toml](../config/source-imports.toml). Those URLs are
historical import provenance. Do not open day-to-day PRs against standalone
`DeanoC/misteross`; work in FES `sources/misteross`. That module has two
jobs: OSS place-and-route experiments
(`sources/misteross/docs/oss-pnr.md`) and described cores
(`sources/misteross/docs/cores.md`). Do not use one as the instructions for
the other. Preserve old component
worktrees and branches during migration; a separate fresh checkout of the reviewed
import revision avoids destructive replacement of local component work. The
presence of the import mapping does not claim published cutover or hardware
acceptance of the new layout.

## 5. Return a useful handoff

```text
Changed: <behavior, module and bounded scope>
Source: <base FES commit; result commit or worktree plus uncommitted diff>
Checked: <commands and actual results, including skipped checks>
Integration: <contract, consumer, source-selection or artifact-policy effects>
Hardware: <not exercised, diagnostic, or exact-artifact evidence link>
Remaining: <specific next dependency or none>
```

Keep host-only validation, hardware diagnostics and exact-image acceptance
separate. Historical evidence applies to its recorded artifacts, not automatically
to a new commit, imported layout or assembled image. The designated kit and its
operating instructions remain in FogCast's
[development guide](../sources/FogCast/docs/DEVELOPMENT.md).

## GitHub delivery coordination

Use the existing [FES project](https://github.com/users/DeanoC/projects/4) as the
shared view, [M1](https://github.com/DeanoC/fes/milestone/1) as the delivery group,
and [#357](https://github.com/DeanoC/fes/issues/357) as the accepted outcome.
GitHub issues, native sub-issues and blocked-by links hold tasks and dependencies;
PRs hold implementation and review. Architecture stays in the existing repo
documents. Slack is for discussion and optional notifications. Copy decisions,
assignments and evidence back to the issue; a chat message is not a durable
assignment or proof that an agent received it.

Every vendor uses the same issue brief: outcome; owner and file scope;
dependencies; agent/team; exact host, absolute worktree, branch and base;
required capabilities; checks/acceptance; PR, evidence and remaining work.
The [agent task form](../.github/ISSUE_TEMPLATE/agent-task.yml) supplies this brief.
A GitHub account may represent several agents, so an assignee alone is not a
runner claim.

### States and claims

Use exactly one canonical state label on each managed issue. Project Status is
a convenience projection: its existing Todo/In progress/Done automation must
not substitute for these states or hardware evidence.

| Label | Meaning |
| --- | --- |
| `task:ready` | Bounded, startable work; prerequisites satisfied |
| `task:working` | A named runner has acknowledged and is doing the work |
| `task:review` | Reviewable PR/diff and focused check results exist |
| `task:validation` | Reviewed work awaits its stated integration/target checks |
| `task:blocked` | A named dependency or explicit external blocker prevents progress |
| `task:done` | This task's stated checks passed and evidence is linked |

For M1, **Bob (Grokbot coordination) is the integration lead**, continuing the
coordination role in [the mesh plan](mesh-lan.md). Deano owns product priorities
and final acceptance. Bob assigns bounded work, arbitrates conflicting claims,
reconciles shared files/contracts and checks handoffs. A successor must be named
explicitly on [#357](https://github.com/DeanoC/fes/issues/357); neither a stale
timestamp nor a report transfers this role.

The runner acknowledges **before** changing Ready to Working, then re-reads the
issue and dependencies at each start/resume. Check other acknowledgement
comments first; claiming is a convention, not an atomic lock. Resolve competing
claims with the lead before editing. A stale timestamp does not transfer
ownership. Reassignment needs a visible handoff or an explicit takeover by the
lead. Keep worker file ownership and the existing physical kit lease separate.

Post acknowledgement as a comment with the following marker and JSON block.
Copy the whole example below as one comment, including the plain marker line.
Replace every example value with the actual execution context before posting.
Keep the full context for repository work, including documentation: it makes
parallel checkouts and reproducible handoffs unambiguous.

````text
FES-TASK-ACK
```json
{"runner":"codex-mesh-session","vendor":"Codex","host":"deano-mac","worktree":"/Users/deano/Projects/fes/out/dev/mesh-task/fes","branch":"codex/mesh-task","base":"9f1d85ca3fd295f0210ff07cfcaad9d607a22f5d"}
```
````

For progress, post `FES-TASK-UPDATE` followed by a JSON block containing
`runner` (the acknowledged identity), `state`, `pr` (URL or empty string),
`evidence` (links/results) and
`remaining`. Replace the canonical state label as well. Post at state changes
and before handing off or pausing. Working claims without a runner update for
24 hours are flagged for human inspection, never automatically reassigned.
The report's `--stale-hours` option can select a different inspection interval.
State labels are the authority; comment state is a record, not an automatic
transition. Do not put credentials in any brief or comment.

A separate reviewer posts `FES-TASK-REVIEW` as a plain marker line followed
directly by a fenced JSON object containing `reviewer` and `url` pointing to the
actual review. Bot teams can keep their existing PR verdict comments: the runner
or integration lead links that comment on the task issue using the same record,
with the actual independent reviewer identity. The report does not infer a
verdict from arbitrary comment prose or a shared GitHub account. For example:

````text
FES-TASK-REVIEW
```json
{"reviewer":"independent reviewer session","url":"https://github.com/DeanoC/fes/pull/123#issuecomment-456"}
```
````

Replace the example values with a completed approving review of the current PR.
A review request alone is not completed review. The integration lead verifies
the review and checks.
Code tasks may close after their own stated checks pass. The milestone outcome
remains open until the dedicated acceptance task has reviewed exact-artifact
evidence. Closing an issue or merging a PR does not establish that evidence.

### Reconciliation and wakeups

Run from any FES checkout with Python 3 and an authenticated GitHub CLI:

```sh
python3 -m scripts.coordination --repo DeanoC/fes --milestone 1 \
  --output out/coordination/m1-report.md
```

This read-only report lists startable, unclaimed Ready work as informational
dispatch candidates. It flags conflicting or missing state labels, unmet native
dependencies, missing runner claims, 24-hour stale runner updates, unparseable
records, recorded reviews still missing, and acceptance issues still open.
A recorded review is not independently certified by the
report. Dependency closure likewise requires the lead to inspect its evidence.
API failures fail the run; they never produce an all-clear report. Comments are
parsed as data and are never executed. An invalid JSON block, non-object JSON
value or marker without its JSON fence produces a diagnostic identifying the
comment; correct that comment rather than leaving a malformed historical record.
Reports do not assign workers, change labels, post messages or program hardware.

The `FES coordination` workflow runs on manual dispatch with a milestone
number. Its report is retained as a workflow artifact and job summary. Between
dispatches, use the command above. The lead checks the report at
planning/handoff and dispatches work through a runner's actually supported
trigger. A GitHub issue, project edit or readable Slack connector does not
guarantee an idle Codex, Grok or Claude session wakes up. Do not promise vendor
event delivery without an observed acknowledgement.

Keep scheduling initially limited to this report. Add Slack notification or
vendor-specific event bridges only when their destination, authority and wakeup
behavior are verified; GitHub remains the durable record.
