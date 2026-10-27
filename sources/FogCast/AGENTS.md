# FogCast working policy

Development lives only in the [FES repository](https://github.com/DeanoC/fes),
under `sources/FogCast/`. The former standalone repository is archived.
Use a FES worktree and open PRs against FES; do not clone or update the old repository.

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
- FES native launches use the target agent and the local libmister-runtime daemon.
- `misteross` and `libmister-runtime` are modules in this FES repository:
  misteross produces RBF artifacts; runtime owns physical hardware transitions.
  Main_MiSTer is a comparison reference. FogCast owns host selection, transfer,
  and network/session coordination.

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
- Leave the operator-selected shared host available. Run branch diagnostics in
  private hosts with separate configuration, ports, catalogs and evidence.
  Clean up only your host/session; follow FES `docs/kit-sharing.md`.
- Check supported API/ABI contracts and record each exact artifact independently.
  Different source revisions alone do not require paired host/kit updates.
  Reuse compatible installed kit software. Diagnostics pin its active bundle ID
  and base-image identity; sealed base-image runtime provenance does not identify
  the running bundle. Record source revisions separately, without aligning them.
- Managed kits use immutable software bundles through the authenticated software
  API. Install without disrupting the owner; queue activation for a free lease.
  Successful selections remain installed. When a shared-kit task installs a
  bundle, install the PR bundle on the running image. Software goes before
  the image: before an image change, activate a bundle the new image accepts.
  After use, if the booted image is not the named baseline, first activate
  the designated baseline bundle named in the Powerboat status file if the
  running image accepts it, then restore that image and verify the boot;
  otherwise restore and verify the image, then activate that bundle.
  Activation does not change the booted image. Verify the active bundle
  before logging the kit restored. A
  core-only run that does not
  change the bundle does not install one. The bundle step is a
  generation-checked selection, not a file overlay and not an automatic
  stock restore (`docs/kit-sharing.md`, `docs/kit-software-releases.md`). Managed
  kits reject binary/configuration file overlays. Unmanaged adoption diagnostics
  still require target-enforced maintenance; never bypass it with SSH copying.
- Use a branch or worktree for substantial work.
- Prefer `rg` for source discovery.
- Run the narrowest relevant tests, then affected package tests and
  `git diff --check`.
- During active target/runtime development, run focused host tests and build a
  compatible kit software bundle. Use a private host for branch diagnostics and
  the managed installation/queued activation path for kit changes. A bundle
  install does not authorize deployment or confer hardware acceptance. Bootstrap,
  system-library, kernel or driver changes still require an appliance image.
- Reserve the FES two-pass `make build` / `make verify` for a stabilized
  change immediately before a major PR, merge, release, or formal hardware
  acceptance. A full image rebuild remains mandatory when Buildroot, init
  scripts, factory-image package contents, image configuration, or locked inputs
  change. Importing/selecting a compatible library package does not change the
  factory image and requires no image rebuild.
  Native image assembly lives in the FES `image/` recipe.
- Commit, push, and open PRs as needed for the requested work without additional
  approval. Only merging a PR requires review by Deano or by another agent or
  model reviewer (for example a Codex review or a domain-reviewer agent). Agents
  never merge on their own authority.
