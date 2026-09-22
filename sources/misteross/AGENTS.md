# misteross working policy

Development lives only in the [FES repository](https://github.com/DeanoC/fes),
under `sources/misteross/`. The former standalone repository is archived.
Use a FES worktree and open PRs against FES; do not clone or update the old repository.

## Start here

There are two jobs. Read the one you are doing before editing.

- OSS place-and-route testing (experiments, primitive proofs, freeze-scaffold):
  `docs/oss-pnr.md`.
- Core update or new core (described packages and the splash bitstream):
  `docs/cores.md`.

Then read `docs/architecture.md` for the build contract you are about to
change. It is not a task list. Per-experiment results are
`docs/oss-experiments.md`. Files under `docs/validation/` are dated records.
The user's current request defines the scope. Do not follow the other job's
guide, and do not treat a gap ladder as the schedule for this tree.

## Clarity is part of correctness

1. A fresh clone must state what builds now, the output RBF paths, the current
   experiments, and the boundary with FogCast.
2. Documentation describes present code in present tense. Proposed or
   experimental work is labelled as such.
3. A commit changing a build lane or artifact path updates the README or
   architecture document in the same commit.
4. Git history is the archive. Delete superseded code and documents instead of
   retaining duplicate, deprecated, or compatibility implementations.
5. `docs/architecture.md` is the one canonical current architecture
   description. `docs/oss-pnr.md` and `docs/cores.md` are the operator guides
   for the two jobs. Do not create a third architecture, competing roadmap,
   status report, evidence handoff, or task-plan archive.
6. Extend a working build lane with the smallest useful change.
7. If documentation, scripts, produced artifacts, and hardware observations
   disagree, resolve the discrepancy before unrelated development continues.
8. Do not add deployment coordinators, supervisors, ownership databases,
   attestation, rollback systems, security frameworks, failover, or recovery
   machinery without a demonstrated current need and explicit user approval.
9. Tests must be proportional and exercise the real build or simulation path.
10. Commits are coherent and describe the working result, not a chain of
    status or evidence documents.

## Source of truth

FES `sources/misteross` is the source of truth. Do **not** open day-to-day
PRs against standalone `https://github.com/DeanoC/misteross`. That URL in
`config/source-imports.toml` is historical import provenance only.

## Repository boundary

- This repository ends at the RBF artifact boundary. FogCast owns host
  selection, network transfer, target lifecycle, and UI-driven launch.
- `sim` uses Verilator.
- `oss` uses the pinned Yosys/nextpnr-mistral/Mistral flow and must not depend
  on Quartus.
- `oracle` uses only an explicitly configured Quartus 17.0.2 installation.
- No build command programs hardware automatically. FogCast
  `POST /api/v1/session/development-rbf` is the designated native-kit load
  path. `make program` is a separate JTAG maintenance diagnostic.

## Disposable hardware

The designated MiSTer Pi is disposable local hobby-development hardware.
SSH, `root`/`1`, changing host keys, rebooting, reflashing, and rebuilding are
normal. Keep private target addresses and credentials out of tracked files.

## Working practices

- Preserve unrelated user changes and upstream licensing.
- Use a branch or worktree for substantial changes.
- Prefer `rg` for source discovery.
- Run the affected unit tests, build or simulate the affected experiment, and
  run `git diff --check`.
- Do not commit, push, or open a pull request unless the user authorizes it.
