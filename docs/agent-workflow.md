# Working with agents through FES

Every agent starts with [root AGENTS.md](../AGENTS.md). Use FES to understand the
whole system, then move into the smallest component scope that owns the task.
The parent is the common starting point; it is not a requirement for each worker
to build the whole system.

## 1. Route the task

| Example task | Primary owner | Likely coordination |
| --- | --- | --- |
| Change browser launch status or library browsing | FogCast host/UI | Target agent only if an API contract changes |
| Change upload handling or network session reporting | FogCast target agent | Runtime if local protocol changes |
| Fix FPGA reset order, video initialization or input delivery | libmister-runtime | FogCast only for an exposed protocol change |
| Change RTL, simulate a core or export a new RBF | misteross | Package source pin and parent artifact selection |
| Change board/register/system definitions | mister-packages | Regenerate all affected C++/Go consumers |
| Change selected revisions, receipts or image orchestration | FES | Relevant component owners |

A cross-component change needs one integrator to reconcile the contract and
pins. Separate workers can implement independent pieces after agreeing the
interface. A small one-component task does not need a full team.

## 2. Give each worker a concrete assignment

Copy and fill this template:

```text
Task: <concrete behavior to change>
Start: Read FES AGENTS.md and docs/project-map.md, then the owning component's
       AGENTS.md, README and current architecture.
Owner: <component and bounded files/modules>
Base: <selected component commit>
Worktree: <absolute path under out/dev/task/component>
Interface: <inputs/outputs and any agreed cross-component change>
Validation: <focused tests and any necessary integration evidence>
Handoff: Explain the diff, tests, limitations and parent/consumer effects.
```

For example, one worker might own a FogCast status display while another owns
an independent runtime input fix. Give them separate component worktrees. If
both need to change the same protocol, agree the representation first and name
who owns each side and the final integration.

Workers should report newly discovered shared-file or contract overlap before
editing it. Do not have multiple agents update the parent pins independently.

## 3. Work in isolation

Use the exact [worktree commands](development.md#isolate-component-work).
Keep `sources/` clean, and do not edit the builder's `out/work/` clones. For
parent-only changes, use a parent branch or an isolated parent checkout when
concurrent edits would otherwise overlap.

Run the component's focused tests in its worktree. A bare `make` inside a
component is not the same as `make` at the FES root. Read its targets before
starting an expensive build or any hardware command.

Parent builds consume selected committed component revisions, not a worker's
uncommitted files. If the result is still a diff, hand back that diff and the
worktree path. When commits are authorized, return the result commit as well.
Do not claim that `make dev` exercised an unselected worktree change.

## 4. Integrate once

The integrator inspects worker results, checks overlapping changes, selects the
compatible component commits and stages the parent gitlinks. Follow the
[selection example](development.md#isolate-component-work). FES generates the concrete
runtime assembly lock from its selected runtime; package-definition changes must
include matching generated consumers and copied source pins.

Run `make check`, then the build appropriate to the change: `make host` for
host outputs or `make dev` for a diagnostic native image. Reserve clean
`make build` and `make verify` for stabilized integration. One parent build is
allowed per checkout; agree one operator for expensive FPGA builds and physical
kit use. Independent worker tests can continue while integration runs.

Before publishing the parent, selected component commits must be available from
their remotes so a fresh recursive clone can obtain them. Follow the user's
existing authorization for commits, pushes and PRs.

## 5. Return a useful handoff

```text
Changed: <problem, resulting behavior, owning component>
Source: <base commit; result commit or worktree + uncommitted diff>
Checked: <commands and actual results>
Integration: <parent pins, runtime lock, package consumers, or API effects>
Hardware: <not exercised, diagnostic, or exact-artifact evidence link>
Remaining: <specific limitation or next dependency; “none” if complete>
```

A passing unit test is not hardware evidence. A successful image build is not a
deployment. Link dated evidence for the exact artifact when physical testing is
part of the task. The designated kit and its operational instructions remain in
FogCast's [development guide](../sources/FogCast/docs/DEVELOPMENT.md).
