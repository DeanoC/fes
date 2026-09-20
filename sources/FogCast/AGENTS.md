# FogCast working policy

## Start here

Read `README.md` and `docs/ARCHITECTURE.md` before changing an execution path.
Trace the current UI-to-hardware path in code before proposing a replacement.
The user's current request defines the scope.

## Clarity is correctness

1. A fresh clone states what works now, the current execution path, the current
   goal, and the main source entry points.
2. Documentation describes present code in present tense. Proposed work is
   labelled as proposed.
3. A commit that changes an execution path updates `README.md` or
   `docs/ARCHITECTURE.md` in the same commit.
4. Git history is the archive. Delete superseded code and documents from the
   active tree; do not leave duplicate, deprecated, or compatibility copies.
5. `docs/ARCHITECTURE.md` is the one canonical architecture description.
6. Names describe current use. Do not use numbered stages, temporary
   experiment labels, or abandoned project names for active code, scripts,
   images, or documentation.
7. Extend the proven path with the smallest useful change. Do not redesign a
   working subsystem to add one feature.
8. If documentation, code, and hardware observations disagree, resolve and
   document the discrepancy before unrelated development continues.
9. Do not add coordinators, ownership databases, attestation, rollback,
   security frameworks, failover, or recovery machinery without a demonstrated
   current need and explicit user approval.
10. Tests are proportional to risk and exercise the real path. Synthetic
    frameworks do not replace relevant operations on the disposable hardware.
11. Commits are coherent and describe the working result, not a status report.

## Project boundaries

- The public host API, target-agent API, and local MiSTer command path remain
  distinct in code, names, and documentation.
- The working FPGA path uses the target agent, a transient MGL, and the
  MiSTer/Main-compatible process through `/dev/MiSTer_cmd`.
- `Main_MiSTer` and `misteross` are related repositories with separate
  responsibilities: the former supplies target runtime code, and the latter
  produces RBF artifacts. FogCast owns host selection, transfer, and launch.

## Disposable development hardware

The designated MiSTer Pi is local hobby-development hardware. SSH, the stock
`root`/`1` login, changing host keys, rebooting, reflashing, and rebuilding are
normal conditions. Do not turn them into product security or failover
features. The exact fixture details are intentionally documented in
`docs/DEVELOPMENT.md` so a fresh checkout is operable.

Target deployment, reboot, wipe, and rebuild are authorized for that exact
designated kit. Do not assume the same authorization for another device.

## Working practices

- Preserve unrelated user changes.
- Use a branch or worktree for substantial work.
- Prefer `rg` for source discovery.
- Run the narrowest relevant tests, then affected package tests and
  `git diff --check`.
- During active target/runtime development, prefer the fast loop: run the
  host tests, cross-build only the changed self-contained runtime artifact,
  inject it into a disposable copy of the last verified image, and deploy it
  for bounded hardware diagnostics. Keep the original verified image
  untouched and label derived-image results diagnostic only; they are not
  reproducibility, release, or formal acceptance evidence.
- Reserve the FES two-pass `make build` / `make verify` for a stabilized
  change immediately before a major PR, merge, release, or formal hardware
  acceptance. A full image rebuild remains mandatory when Buildroot, init
  scripts, package contents, image configuration, or locked inputs change.
  Native image assembly lives in the FES `image/` recipe.
- Do not commit, push, or open a pull request unless the user authorizes it.
