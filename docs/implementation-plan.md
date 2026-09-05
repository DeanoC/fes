# Native Parent Implementation Plan

**Goal:** One command builds the pinned Linux host and native image.
**Architecture:** Git submodules, GNU Make entry points and small Python standard-library scripts. Child image machinery owns compilation and verification.
**Spec:** native-parent-design.md

## Constraints

Linux amd64 builder; Python 3.11+; Go toolchain selected by child go.mod; Docker-compatible runtime. No child modifications or deployment. Submodule gitlinks are the component revision authority; FogCast's runtime lock must agree.

## Tasks

- [x] Add integration tests using disposable real Git repositories: accept pinned clean sources; reject a moved pin, dirty source or incompatible runtime lock. Run `python3 -m unittest discover -s tests -v` before implementation and observe missing checker failure.
- [x] Implement `scripts/inputs.py`: `validate(root)` reads staged gitlinks using `git ls-files --stage`, checks each child HEAD and porcelain status, and parses the native runtime TOML lock. Return source revision mapping or raise ValueError. Run the same tests.
- [x] Add `Makefile`, `profiles/native-dev.toml`, and `scripts/build.py`: doctor, build, image, host and verify entry points; use a workspace-specific Docker volume; invoke the child's build-fogcast with Linux amd64 overrides and target-image-native with the absolute runtime directory; publish files with SHA256 receipts. Add a corruption/invalidation test for receipt reuse.
- [ ] Run `make doctor`, `make test`, `make build`, `make verify`, then rerun `make build` to verify reuse. Compare rebuilt image with the recorded baseline hash and inspect the host ELF identity.
- [ ] Review the parent diff and fresh-clone behavior; document exact commands and evidence in README.md. Leave local work reviewable without publishing.
