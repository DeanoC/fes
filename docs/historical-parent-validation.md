# Historical parent validation

These records describe the original parent source combinations. They do not
claim acceptance of the current integration profile. Paths under `out/` refer
to the original build checkout (retained locally at `/home/deano/fes-parent`).

## Historical image provenance

The [recorded acceptance](https://github.com/DeanoC/FogCast/blob/249988a7a4eb37ea7f974ee6d5b4700af35bebb1/docs/hardware/native-megadrive-baseline.md) image has SHA-256 `95c9b4671e0d19781a6428d2168ab631453740215b194519f12788ade03c7c2e`. Its acceptance record names FogCast `cd85971`, but the embedded agent's Go build metadata names `19a3d5e`. An independent reconstruction using clean cd85971 source in a linked worktree beneath the old 19a3d5e checkout reproduced the historical agent byte for byte (SHA-256 d5cfab61a45e79205a5aab922bbed46b4905968a665f8432f1121a426dcb60c1). A standalone cd85971 build produces agent SHA-256 7790789857b4bb4cbb3f6083f799811f4fcf4a12eb86b15599ea571acd8fb928 with the correct source stamp. Building directly inside a nested submodule also stamped the parent's Git state during this implementation.

The parent uses standalone source copies so the agent embeds the selected FogCast revision correctly. Therefore the new image is not assumed to byte-match the historical image. `make verify` records `historical_baseline_match` in `verification.json` and independently requires matching hashes from both new build passes, structural verification and QEMU packaging checks. The first image's file-content manifest differs from the historical image only in `usr/sbin/mister-agent` and `usr/share/mister-runtime/build-inputs`; the latter differs only in its agent SHA-256. The runtime and both RBFs match the historical hashes. No new physical-hardware acceptance is claimed.

See [design](native-parent-design.md) and [implementation plan](implementation-plan.md).

## Validated on powerboat, 2026-09-05

- Six parent tests pass, including source identity, lock compatibility, source staging and corrupt-output rejection.
- Both independent native image passes produce SHA-256 `03197cb4fa867df1fa8fe6d0ef4f42eefecc2a7a47b8e87b73f0323e6a6a5a8d`.
- Structural checks and QEMU startup packaging pass.
- A subsequent `make build` reuses both verified outputs.
- A fresh recursive clone passes doctor and tests and produces byte-identical Linux CLI/API binaries.
- A forced host rebuild with ambient `GOAMD64=v3 GOFLAGS=-race VERSION=9.9.9` retains the profile's amd64/v1 target and version 0.1.0 and produces the same host hashes.

Local logs and evidence are under `out/`; baseline build products and verification results are under `out/native-dev/`. The parent repository is [DeanoC/fes](https://github.com/DeanoC/fes).

The source profile also passed ten parent tests, two identical image passes,
structural checks, QEMU packaging smoke and output reuse. Its exact image was
deployed to the designated MiSTer and passed a bounded Sonic 2 launch,
direction/jump, Stop/relaunch and idle/reboot check. See
[source build validation](source-build-validation.md) for hashes,
evidence paths and the distinction from the older hardware acceptance.
