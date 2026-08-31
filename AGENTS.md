# FogCast working policy

## Start here

Read `README.md` and `docs/ARCHITECTURE.md` before changing an execution path.
Trace the current UI-to-hardware path in code before proposing a replacement.
The user's current request defines the scope.

## Clarity is part of correctness

1. A fresh clone must state what works now, the current execution path, the
   current goal, and the main source entry points.
2. Documentation describes present code in present tense. Proposed or
   experimental work is labelled as such.
3. A commit that changes an execution path updates `README.md` or
   `docs/ARCHITECTURE.md` in the same commit.
4. Git history is the archive. Delete superseded code and documents from the
   active tree instead of keeping duplicate, deprecated, or compatibility
   implementations beside current code.
5. `docs/ARCHITECTURE.md` is the one canonical current architecture
   description. Do not create competing roadmaps, status reports, or handoff
   documents.
6. Extend the proven path with the smallest useful change. Do not redesign a
   working subsystem to add one feature.
7. If documentation, code, and hardware observations disagree, resolve and
   document the discrepancy before unrelated development continues.
8. Do not add coordinators, supervisors, ownership databases, attestation,
   rollback systems, security frameworks, failover, or recovery machinery
   without a demonstrated current need and explicit user approval.
9. Tests must be proportional to risk and exercise the feature's real path.
   Large synthetic test frameworks are not a substitute for running relevant
   behavior on the disposable hardware.
10. Commits are coherent and their messages describe the working result. Do
    not replace implementation progress with status-report or evidence-only
    commit chains.

## Project boundaries

- Keep the public host API, target-agent API, and local Main command path
  distinct in code, names, and documentation.
- The working FPGA path uses the target agent, a transient MGL, and the
  resident Main-compatible process through `/dev/MiSTer_cmd`.
- `Main_MiSTer` native-coordinator branches and the removed fpgadev machinery
  are not the current runtime.
- `misteross` produces RBF artifacts. FogCast owns host selection, transfer,
  and target launch.

## Disposable development hardware

The designated MiSTer Pi is disposable local hobby-development hardware.
SSH, the stock `root`/`1` login, changing SSH host keys, rebooting, reflashing,
and rebuilding are normal development conditions. Do not turn them into
product security or failover features. Keep credentials and private target
addresses out of tracked files and logs.

Target deployment, reboot, wipe, and rebuild are authorized for that exact
designated development kit. Do not assume the same authorization for another
device.

## Working practices

- Preserve unrelated user changes.
- Use a branch or worktree for substantial work.
- Prefer `rg` for source discovery.
- Run the narrowest relevant tests, followed by the affected package tests and
  `git diff --check`.
- Do not commit, push, or open a pull request unless the user authorizes it.
