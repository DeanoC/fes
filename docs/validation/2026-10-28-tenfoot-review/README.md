# PR #1182 review follow-up

All six findings in the [review of `50fb1cc22e`](https://github.com/DeanoC/fes/pull/1182#issuecomment-6464783330)
were present. Fix commit `b52145ab66` addresses them:

| Finding | Result |
| --- | --- |
| Running Sinclair workbench depends on the visible shelf | Capture the verified launch identity or read the exact active package. Async replies match the captured session, target, package/generation and request; failed reads remain retryable without changing play. The selected model is passed to its workshop. |
| One broken installed package hides healthy siblings | Skip failed package inspections with a notice and keep usable rows. Inventory, connection and cancellation failures still fail the refresh. |
| Setup success incorrectly claims media/expansions were saved | Confirm only that the setup was added to the library. |
| Saved disk uses the model's entire media label | Name its declared role, including Disk .dsk for +3 and Disk .trd for Pentagon; preserve other model-specific labels. |
| Add system is active while the shelf is loading | Gate rendering, keyboard focus and pointer activation until the rows are ready. |
| ZX80/Timex use ZX81 workshop branding | Use the selected model for the title, missing-setup notice, illustration label and setup action, while retaining the specialist rear connector/cassette controls. |

Focused regressions cover each finding. Independent review found no material
defects in these changes. A separate fixture-only commit `3544e5ca36` teaches the
adopted-session surround test to supply and verify its exact package lookup.
It does not change production code. Shared API, runtime and FPGA contracts are
unchanged; the hostclient helper uses an existing package GET endpoint.

## Validation

The affected check against `50fb1cc22e` passed 21 planner, 31 runner and 864 parent
tests on `b52145ab66`, then failed the old surround fixture. That failure log is
retained. After the fixture correction, the exact failed host lane passed all
seven stages on clean `3544e5ca36`: 95 Go package results (90 unique), 361 Node
checks and 78 UI/DOM checks. No passing parent checks were repeated.

Focused routing and surround tests passed with race detection; the complete room
suite and hostclient tests also passed. `make check`, `make deadcode`, generated
consistency (21 consumers, 37 fixture copies) and documentation links passed.
The [inventory](inventory.json) records log hashes and the exact retained retry
command. The [deployment classification](hil-plan.json) selects kit userspace and
host consumers; it does not require an OS image rebuild.

## Artifact and remaining kit check

The [software receipt](software-receipt.json) records bundle
`f97adf1f2b65bb7185ec5395d6ef6f4bce9a5fe0d9bb429e57102bf62ebcb7a4`, built from
`3544e5ca36b4bcf8dd4d92db046225ce23fb29cf` against the retained image
`f904961e008a972db8f06f1bb47aff9631123d67215b9f4157ea1fbee83b98d4`.
The installable tar is retained at the path in the inventory. This is a software
build, not a deployment or hardware acceptance. No kit was touched in this follow-up.

The [earlier Kit A diagnostic](../2026-10-28-tenfoot-families-kit-a/README.md)
continues to cover only its named `451ade3584` executable/bundle. The new bundle
still needs a claimed-kit userspace smoke test: check Systems loading and model
workshop labels/setup, saved-media labels, and the ordinary launch/stop path, then
restore the designated baseline. Active-session routing is also covered by the
host-window regressions with filtered/adopted/replaced sessions. This does not
require an all-core build or full image campaign. PR review remains required.
