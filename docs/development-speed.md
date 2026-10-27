# Development speed and independent installation

FES is an unreleased game system. The normal workflow should be edit, test the
affected part, install it and play. A first release is a later integration
milestone, not a condition every development change must satisfy.

This change removes playable cores from the default image and narrows several
CI and hardware-plan triggers. It does not finish the installer redesign below.
Existing boot recovery, package integrity and interface checks remain useful.
They should protect an installation without tying unrelated components together.

## What the audit found

The audit started at FES `e307c466e35e03b2acf345eefc7ee8e1ae6e291d` on
20 October 2026, with a clean assigned worktree and a refreshed `origin/main`.

| Finding | Consequence | Change here |
| --- | --- | --- |
| Default profile selected nine FPGA packages | An OS build could wait for eight playable/diagnostic cores and their video parts | Bootstrap selects MENU only; playable packages remain catalog installations |
| Planner/gate tests and agent instructions selected every core | Non-RTL edits launched up to 38 simulation jobs | Planner tests run as parent software; agent instructions are docs |
| Parent Makefile/workflow/shared software-runner edits selected RTL | Software CI maintenance waited on all simulations | Software checks run; actual simulation infrastructure still exercises simulations |
| Compiler bootstrap and contract Go tooling selected all RTL | Build-tool fixes consumed simulation capacity without changing simulated inputs | Software tests and generated-consumer checks cover them; changed generated RTL still selects consumers |
| Every parent lane provisioned platform and ran platform/image tests | A room or host edit paid OS test setup costs | Those steps follow the media mode |
| Unlisted CI tools became `FULL_IMAGE` in the HIL classifier | A dead-code checker could request a kit image | Explicit tool-only paths return `NO_DEPLOY_CHANGE` |

In [run 39374921091](https://github.com/DeanoC/fes/actions/runs/39374921091),
PR #879 launched 38 simulation jobs. Atari ST took 18m39s; parent tests took
16m39s; host shards took 11m08s, 32s and 6m16s. These are observed job durations,
not an estimate of savings from this change. The host sharding is visibly uneven.

The active GitHub `Main Protect` ruleset required zero approving reviews and
listed no required status checks at audit time. Grokbot observes ready PRs and
performs review, host/kit checks and merging. The user confirmed that workflow;
this change leaves it in place and makes its build/test inputs smaller.

## Product boundaries

| Installable part | Contains | Change should require |
| --- | --- | --- |
| Base OS | Kernel, drivers, mounts, networking, supervisor, recovery and installer bootstrap | One base build and boot/install smoke |
| System software | Agent/runtime and UI engine; host installed independently | Relevant software tests, bundle install and changed-path smoke |
| Core | RBF, manifest, ROM map and required expansion/video dependencies | That core's simulation/build and relevant play/Stop test |
| Room pack | Lua, layout, artwork and room metadata | Pack validation and room engine/input tests; install/reopen |

MENU is display firmware used by the kit UI. Keeping it in the bootstrap image
avoids making first boot depend on a network download. It is not a playable core.
The existing sealed splash remains the recovery/idle bitstream.

Compatibility is expressed through supported APIs, ABIs and capabilities.
Git revisions are useful provenance, not a requirement that every installed
component come from the same commit. Reuse an unchanged artifact and its tests
when its actual inputs and relevant interfaces are unchanged; do not describe
that as a fresh whole-system acceptance run.

The core library, immutable software bundles and external room-directory loader
already exist. Use those implementations. A second package manager, repository
split or general-purpose dependency solver would add work without helping this
transition.

## Review and test policy

Ready PRs enter Grokbot's existing review path. A review should name a concrete
remaining check and the artifact that needs it.

| Change | Routine merge evidence | Hardware work |
| --- | --- | --- |
| Docs, tests, CI tooling | Focused regressions and changed-plan checks | None |
| Host logic/browser | Affected host tests and build | Private-host smoke when behavior depends on a target |
| Room Lua/assets | Pack/engine tests and input/render checks | Room smoke on existing software when target behavior matters |
| Agent/runtime/UI engine | Affected software and interface tests | Install software bundle, exercise changed behavior, restore |
| Core RTL | Affected simulations and core build | Test that package, including Stop/relaunch |
| Toolchain pin | Build every affected core, as the existing rule requires | Affected package qualification |
| Boot/kernel/init/storage | Relevant platform/image tests and minimal image | Boot/recovery smoke on a claimed kit |

Full image reproducibility, exhaustive race coverage and all-core compatibility
campaigns remain explicit integration work. A green source PR does not claim
that all games, every core or the complete image were physically accepted.
Conversely, an unrelated core's timing failure must not block a software fix.

Keep protection against corrupt packages, path traversal, interrupted activation
and loss of saved work. Keep device leases and rollback to protect shared kits.
Remove equal-revision requirements, unrelated rebuilds and duplicated evidence
requests wherever they do not protect the operation being performed.

## In-flight work

| Open PR at audit | Treatment |
| --- | --- |
| [#879](https://github.com/DeanoC/fes/pull/879), dead-code cleanup/check | Preserve cleanup. Its new consistency step overlaps the workflow; retain that step when reconciling. Checker-only changes must not require an image. Decide separately whether the expensive audit belongs on every software PR or the scheduled lane. |
| [#907](https://github.com/DeanoC/fes/pull/907), compiler bootstrap | Preserve its fix; bootstrap software changes no longer trigger unchanged RTL simulations. |
| [#912](https://github.com/DeanoC/fes/pull/912), nextpnr schema | Preserve option validation and per-core recipe identity. No global toolchain change is needed for this redesign. |
| [#899](https://github.com/DeanoC/fes/pull/899), [#902](https://github.com/DeanoC/fes/pull/902), boot confirmation/splash | Keep image-level testing: current trial boots execute these image binaries. The smaller image removes the unrelated playable-core builds. |
| [#888](https://github.com/DeanoC/fes/pull/888), Pong pin; [#897](https://github.com/DeanoC/fes/pull/897), [#913](https://github.com/DeanoC/fes/pull/913), ST; [#714](https://github.com/DeanoC/fes/pull/714), C64; [#836](https://github.com/DeanoC/fes/pull/836), PC/XT; [#916](https://github.com/DeanoC/fes/pull/916), RISC-V producer | Continue independently. None is a prerequisite for a new bootstrap OS or software bundle. |

## Remaining implementation, in order

1. **Separate parent regressions from product lanes.** Current host edits still
   run the whole parent Python suite and all host packages. Use package/import
   dependencies and measured timings to select and balance those tests. Retain
   a full scheduled run and validate the selector against representative diffs.
   Target: ordinary room/host PR feedback within five minutes; measure p50/p95
   before claiming that target is met.
2. **Make OS building consume installed artifacts.** `make image` now consumes
   an explicit prebuilt bootstrap artifact set; see [assembly inputs](image-artifacts.md).
   Source integration remains explicit in `make build`/`make dev`. Select
   independently produced binaries and MENU with `pack`, or export a retained
   image's MENU and userspace without rebuilding them.
   Offline first boot and an empty playable-core inventory still need kit evidence.
3. **Separate UI engine and room delivery.** Kit software currently bundles the
   engine with agent/runtime; first-party room examples are embedded in Go.
   The existing room installer now provides bounded validation, immutable
   snapshots, atomic selection, live reopen and rollback. First-party source
   packs declare their public `example.*` ids, so the installed launcher can
   update the same room without compiling Go, restarting the agent or rebuilding
   MENU. Embedded snapshots remain offline recovery defaults; refreshing those
   defaults and engine/API changes are software updates. Remote room catalog
   distribution and end-to-end kit install/reopen evidence remain separate work;
   see [room installation](../sources/FogCast/docs/rooms.md#installing-and-opening-rooms).
4. **Finish installer dependency handling.** Install a chosen core with its
   matching video/expansion packages. Show download/install/ready states and
   missing user-supplied firmware. Cancel/retry must preserve the previous
   working selection and saved data.
5. **Separate development images from release certification throughout.**
   Single-pass HIL images already exist, but bootstrap/export/receipt rules
   still impose release assumptions. Trace those consumers together before
   changing the default. Use one-pass development builds and explicit release
   reproducibility; remove the old path in the same cutover.

Acceptance of the finished redesign is a clean boot with no playable cores,
installing and playing one core, updating a room without rebuilding software,
updating software without building cores, and recovering a failed installation.
Those end-to-end checks have not been run by this host-only change.
