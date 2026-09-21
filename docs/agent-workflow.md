# Working with agents through FES

Every agent starts with [AGENTS.md](../AGENTS.md),
[the project map](project-map.md) and the owning module's instructions. The
first-party modules under `sources/` share the FES repository and commit history.
Keep their architectural ownership separate while making cross-module changes
in one branch and PR.

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
Handoff: <diff or authorized commit, evidence and remaining work>
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
consumers, and make one compatible commit when authorized. No component SHA-only
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

Follow the user's authorization for commits, pushes and PRs. The imported source
histories and original URLs are recorded in
[config/source-imports.toml](../config/source-imports.toml). Those URLs are
historical import provenance. Do not open day-to-day PRs against standalone
`DeanoC/misteross`; work in FES `sources/misteross`. Preserve old component
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
