# Native Parent Implementation Plan

Historical implementation record. For current usage, start with the
[documentation index](README.md) and [development guide](development.md).

**Goal:** One command builds the pinned Linux host and native image.
**Architecture:** Git submodules, GNU Make entry points and small Python standard-library scripts. Child image machinery owns compilation and verification.
**Spec:** native-parent-design.md

## Constraints

Linux amd64 builder; Python 3.11+; Go toolchain selected by child go.mod; Docker-compatible runtime. No child modifications or deployment. Submodule gitlinks are the component revision authority; FogCast's runtime lock must agree.

## Tasks

- [x] Add integration tests using disposable real Git repositories: accept pinned clean sources; reject a moved pin, dirty source or incompatible runtime lock. Run `python3 -m unittest discover -s tests -v` before implementation and observe missing checker failure.
- [x] Implement `scripts/inputs.py`: `validate(root)` reads staged gitlinks using `git ls-files --stage`, checks each child HEAD and porcelain status, and parses the native runtime TOML lock. Return source revision mapping or raise ValueError. Run the same tests.
- [x] Add `Makefile`, `profiles/native-dev.toml`, and `scripts/build.py`: doctor, build, image, host and verify entry points; use a workspace-specific Docker volume; invoke the child's build-fogcast with Linux amd64 overrides and target-image-native with the absolute runtime directory; publish files with SHA256 receipts. Add a corruption/invalidation test for receipt reuse.
- [x] Run `make doctor`, `make test`, `make build`, `make verify`, then rerun `make build` to verify reuse. Compare rebuilt image with the recorded baseline hash and inspect the host ELF identity.
- [x] Review the parent diff and fresh-clone behavior; document exact commands and evidence in README.md. Leave local work reviewable without publishing.

## Source-built Mega Drive extension

- [x] Pin the tested misteross bundle-export branch as a third submodule.
- [x] Test bundle identity, payload hash, lock overlay restoration, and profile volume isolation before implementation.
- [x] Add `native-source-dev`, run the pinned Quartus rebuild/export, inject only the validated Mega Drive input, and preserve the bundle beside the image.
- [x] Run the complete source build, structural verification, QEMU packaging smoke, and verified-output reuse.
- [x] Deploy the exact verified image to the designated MiSTer, prove launch/input/Stop/relaunch with a known-good capture control, and pass native idle/reboot smoke. See source-build-validation.md for evidence and limits.
