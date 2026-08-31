# FogCast Naming Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace historical experiment names with one clear FogCast target-image workflow and delete the unused bootstrap, checkpoint, recovery, and staged-roadmap machinery.

**Architecture:** Preserve the browser-to-host-to-agent-to-Main launch path. Rename the active Buildroot toolchain as one unit, replace the deployment and HIL layers with small direct scripts, and leave history only in Git. Rename the GitHub repository only after local and hardware verification.

**Tech Stack:** Go, POSIX shell, GNU Make, Buildroot 2021.02.4, Docker, MiSTer Linux, GitHub CLI.

**Spec:** `docs/superpowers/specs/2026-08-31-fogcast-naming-cleanup-design.md`

## Global Constraints

- The canonical project, repository, and Go module name is `FogCast` / `DeanoC/FogCast` / `github.com/DeanoC/FogCast`.
- The canonical Linux image name is `target-image`; do not add compatibility aliases.
- Preserve the current host API, target API, transient MGL, `/dev/MiSTer_cmd`, and resident Main-compatible execution path.
- Permit `/media/fat/MiSTer` and `/media/fat/menu.rbf` to change; require their presence but do not pin their hashes.
- Use `/media/fat/fogcast` for the target agent and configuration.
- The dedicated `192.168.10.239` target is disposable; do not add rollback, failover, ownership, attestation, or recovery systems.
- Keep only one source-revision lock. Generated hashes live beside generated outputs.
- Delete temporary planning documents after canonical documentation is updated.

---

### Task 1: Canonical Go module and retained command names

**Files:**
- Modify: `go.mod`
- Modify: every tracked `*.go` import of `github.com/DeanoC/FogCast-POC`
- Rename: `cmd/remote-play-spike/` to `cmd/remote-play-sender/`
- Modify: `cmd/remote-play-sender/main.go`
- Modify: `cmd/remote-play-sender/main_test.go`
- Modify: `Makefile`

**Interfaces:**
- Consumes: existing Go package layout and remote-media sender modes.
- Produces: module `github.com/DeanoC/FogCast` and executable `remote-play-sender` with unchanged `devices`, `sender`, and `probe` modes.

- [ ] **Step 1: Rename the command and update its test expectations**

```sh
git mv cmd/remote-play-spike cmd/remote-play-sender
```

Change command diagnostics from `remote-play-spike:` to
`remote-play-sender:` and change the Make target/output to
`build-remote-play-sender` and `bin/remote-play-sender`.

- [ ] **Step 2: Change the module and all internal imports**

```text
module github.com/DeanoC/FogCast
```

Replace every Go import prefix `github.com/DeanoC/FogCast-POC/` with
`github.com/DeanoC/FogCast/`, then run `gofmt` on changed Go files.

- [ ] **Step 3: Verify the retained Go tree**

```sh
go test ./...
go vet ./...
git diff --check
```

Expected: all commands exit zero and no diagnostic contains the former sender
name.

- [ ] **Step 4: Commit**

```sh
git add -A
git commit -m "refactor: adopt FogCast project naming"
```

### Task 2: Delete superseded experiment subsystems

**Files:**
- Delete: `cmd/package-poc1a/`
- Delete: `cmd/poc2-lock/`
- Delete: `cmd/mister-hil/`
- Delete: `cmd/fogcast-hil/`
- Delete: `internal/packagepoc/`
- Delete: `internal/hil/`
- Delete: `internal/imagepoc/poc2.go`
- Delete: `internal/imagepoc/poc2_test.go`
- Delete: `deploy/poc1a/`, `deploy/poc1b/`, `deploy/poc2/`
- Delete: `scripts/capture-poc1a-lock.sh`
- Delete: `scripts/install-poc1a-target.sh`
- Delete: `scripts/install-poc1a.sh`
- Delete: `scripts/install-poc1b.sh`
- Delete: `scripts/install-poc2.sh`
- Delete: `scripts/package-poc1a.sh`
- Delete: `scripts/restore-poc1a-sd.sh`
- Delete: `scripts/restore-poc1b-sd.sh`
- Delete: `scripts/verify-poc1a-lock.sh`
- Delete: `scripts/tests/install-poc1a-target_test.sh`
- Delete: `scripts/tests/install-poc1b-target_test.sh`
- Delete: `scripts/tests/install-poc2-target_test.sh`
- Delete: `scripts/tests/poc2-rootfs_test.sh`
- Delete: `scripts/tests/restore-poc1b-sd_test.sh`
- Delete: `build/sources.poc1a.lock.toml`
- Delete: `build/outputs.poc2.lock.toml`
- Modify: `Makefile`

**Interfaces:**
- Consumes: the approved deletion list from the design.
- Produces: a build and test graph containing only current FogCast runtime and target-image work.

- [ ] **Step 1: Remove deleted code from the Make graph first**

Remove `build-hil`, `build-fogcast-hil`, `build-poc2-lock`, `package-poc1a`,
`package-test`, and the deleted POC test targets and prerequisites from
`.PHONY`, `build`, and `test`.

- [ ] **Step 2: Delete the superseded files and packages**

Use `git rm` on the exact paths listed above. Do not retain wrappers, aliases,
redirects, archived copies, or deprecation notices.

- [ ] **Step 3: Verify no retained package imports deleted code**

```sh
rg -n 'internal/(hil|packagepoc)|cmd/(mister-hil|fogcast-hil|package-poc1a|poc2-lock)' --glob '*.go' .
go test ./...
git diff --check
```

Expected: `rg` has no matches; Go tests and whitespace checks exit zero.

- [ ] **Step 4: Commit**

```sh
git add -A
git commit -m "chore: remove superseded deployment experiments"
```

### Task 3: Simplify and rename the source lock

**Files:**
- Rename: `internal/imagepoc/` to `internal/targetimage/`
- Modify: `internal/targetimage/lock.go`
- Modify: `internal/targetimage/lock_test.go`
- Rename: `cmd/poc1b-lock/` to `cmd/target-image-lock/`
- Modify: `cmd/target-image-lock/main.go`
- Modify: `cmd/target-image-lock/main_test.go`
- Rename: `build/sources.poc1b.lock.toml` to `build/target-image.sources.lock.toml`
- Modify: `Makefile`

**Interfaces:**
- Consumes: pinned container, Buildroot, image-creator, and kernel metadata.
- Produces: `target-image-lock resolve` and `target-image-lock verify-inputs`; package functions `LoadSourceLock`, `WriteSourceLock`, and `VerifyFile`.

- [ ] **Step 1: Write the reduced lock tests**

Tests must prove that the source lock strictly loads the four retained input
sections, round-trips deterministically, rejects unknown/output sections, and
verifies cached image-creator files. Remove tests for accepted device
inventories and recorded build outputs.

- [ ] **Step 2: Run the focused tests and observe failure**

```sh
go test ./internal/targetimage ./cmd/target-image-lock
```

Expected before implementation: compile failures for `LoadSourceLock` and the
renamed command/package interfaces.

- [ ] **Step 3: Implement the reduced source lock**

Keep only `Container`, `Buildroot`, `ImageCreator`, `Kernel`, and `Sources`.
Remove `Artifact`, `Runtime`, `Library`, `Accepted`, `Outputs`,
`LoadPOC1A`, `LoadPOC1B`, `WritePOC1B`, and `record-outputs`. Rename messages,
binary outputs, and Make targets to `target-image-lock`,
`target-image-resolve`, and `build-target-image-lock`.

- [ ] **Step 4: Run focused and repository tests**

```sh
go test ./internal/targetimage ./cmd/target-image-lock
go test ./...
git diff --check
```

Expected: all commands exit zero.

- [ ] **Step 5: Commit**

```sh
git add -A
git commit -m "refactor: simplify target image source locking"
```

### Task 4: Rename the complete target-image toolchain

**Files:**
- Rename: `containers/poc1b/` to `containers/target-image/`
- Rename: `build/poc1b-container-packages.sha256` to `build/target-image-container-packages.sha256`
- Rename: `build/poc1b-kernel-defconfig.sha256` to `build/target-image-kernel-defconfig.sha256`
- Rename: `buildroot/board/mister-remote/` to `buildroot/board/fogcast-target/`
- Rename: both `mister_remote_poc1b_*_defconfig` files to `fogcast_target_*_defconfig`
- Rename: `scripts/build-poc1b-image.sh` to `scripts/build-target-image.sh`
- Rename: `scripts/build-poc1b-kernel.sh` to `scripts/build-target-kernel.sh`
- Rename: `scripts/fetch-poc1b-sources.sh` to `scripts/fetch-target-image-sources.sh`
- Rename: `scripts/poc1b-container.sh` to `scripts/target-image-container.sh`
- Rename: `scripts/qemu-smoke-poc1b.sh` to `scripts/qemu-smoke-target-image.sh`
- Rename: `scripts/scan-poc1b-secrets.sh` to `scripts/scan-target-image-secrets.sh`
- Rename: `scripts/verify-poc1b-image.sh` to `scripts/verify-target-image.sh`
- Rename: `scripts/verify-poc1b-kernel.sh` to `scripts/verify-target-kernel.sh`
- Rename: `scripts/verify-poc1b-source-cache.sh` to `scripts/verify-target-image-source-cache.sh`
- Rename: active `scripts/tests/poc1b-*` tests to `target-image-*` or `target-kernel-*` names by subject
- Modify: `buildroot/external.desc`, `buildroot/Config.in`, `buildroot/external.mk`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `build/target-image.sources.lock.toml` and the existing Buildroot recipes.
- Produces: `make target-image-fetch`, `target-image-dev`, `target-images`, `target-image-verify`, `target-image-qemu-smoke`, `target-kernel`, and outputs under `build/output/target-image`.

- [ ] **Step 1: Add a failing repository-naming assertion**

Extend the renamed shell tests to require canonical script/config paths,
`FOGCAST_TARGET`, `TARGET_IMAGE_*`, `/target-image-output`, and
`build/{cache,output}/target-image`. The test must reject numbered lifecycle
names and the former board/product name without embedding those terms as
contiguous text in the test itself.

- [ ] **Step 2: Run the naming and image tests and observe failure**

```sh
sh scripts/tests/target-image-sources_test.sh
sh scripts/tests/target-image-rootfs_test.sh
sh scripts/tests/target-image_test.sh
```

Expected before implementation: missing canonical files and identifiers.

- [ ] **Step 3: Apply the mechanical file and identifier rename**

Move all active assets together. Replace POC-prefixed paths, variables,
messages, image labels, pinned Git refs, fixture directories, output volumes,
and Make targets. Use `fogcast-target-image-output` as the default Docker
volume so the first canonical build cannot reuse the contaminated historical
Buildroot output.

- [ ] **Step 4: Remove stock artifact gates from the root image**

In `S40mister-main`, require regular non-empty `/media/fat/MiSTer` and
`/media/fat/menu.rbf` files and start Main. Remove embedded hashes and the
stock inventory lock. In post-build and verification scripts, assert required
libraries/packages directly from the target tree and defconfigs.

- [ ] **Step 5: Make incremental image builds configuration-safe**

Store a SHA-256 of `fogcast_target_dev_defconfig` in the persistent Buildroot
output. If it differs on the next `--fast-dev` run, remove that exact
`/target-image-output/dev-work-dev` directory before configuring. Preserve
incremental speed when the defconfig is unchanged.

- [ ] **Step 6: Run all target-image fixture tests**

```sh
sh scripts/tests/target-image-sources_test.sh
sh scripts/tests/target-image-rootfs_test.sh
sh scripts/tests/target-image_test.sh
sh scripts/tests/target-image-dev_test.sh
sh scripts/tests/target-image-dev-container_test.sh
sh scripts/tests/target-image-kernel_test.sh
```

Expected: all commands exit zero, including a red/green fixture proving that
a changed defconfig clears only the persistent development work directory.

- [ ] **Step 7: Commit**

```sh
git add -A
git commit -m "refactor: rename the FogCast target image toolchain"
```

### Task 5: Replace deployment and HIL with direct tools

**Files:**
- Create: `scripts/deploy-target-image.sh`
- Create: `scripts/target-smoke.sh`
- Create: `scripts/tests/deploy-target-image_test.sh`
- Create: `scripts/tests/target-smoke_test.sh`
- Modify: `buildroot/board/fogcast-target/rootfs-overlay/etc/init.d/S50mister-agent`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `build/output/target-image/dev/linux.img`, host API at `127.0.0.1:8787`, and target `192.168.10.239` using `root` / `1`.
- Produces: `make target-image-deploy` and `make target-smoke GAME_ID=... EXPECTED_CORE=...`.

- [ ] **Step 1: Write deployment fixture tests**

Use fake `sshpass`, `scp`, and `ssh` executables on `PATH`. Assert that the
script uploads to `/media/fat/linux/linux.img.new`, renames the uploaded
image, syncs, and reboots. Assert that a failed upload never issues the
rename. The script must not contain migration or compatibility behavior.

- [ ] **Step 2: Write target-smoke fixture tests**

Use fake `curl` and `sshpass` commands to assert this order: host health,
target health, launch POST with the exact game ID, expected core polling,
stop POST, and `MENU` polling. Require exactly two positional arguments:
game ID and expected core.

- [ ] **Step 3: Run both tests and observe failure**

```sh
sh scripts/tests/deploy-target-image_test.sh
sh scripts/tests/target-smoke_test.sh
```

Expected: failure because the new scripts do not exist.

- [ ] **Step 4: Implement the two direct scripts**

Use strict POSIX shell, exact target paths, bounded polling, and ordinary
errors. Do not add token management, inventory, recovery state, rollback,
attestation, or failure injection. Update `S50mister-agent` to use only
`/media/fat/fogcast`.

- [ ] **Step 5: Run fixture and shell syntax tests**

```sh
sh -n scripts/deploy-target-image.sh scripts/target-smoke.sh
sh scripts/tests/deploy-target-image_test.sh
sh scripts/tests/target-smoke_test.sh
git diff --check
```

Expected: all commands exit zero.

- [ ] **Step 6: Commit**

```sh
git add Makefile buildroot/board/fogcast-target scripts
git commit -m "feat: add direct target deployment and smoke checks"
```

### Task 6: Rewrite canonical documentation and remove historical planning

**Files:**
- Modify: `README.md`
- Modify: `IDEA.md`
- Modify: `docs/ARCHITECTURE.md`
- Modify: `docs/DEVELOPMENT.md`
- Modify: `AGENTS.md`
- Delete: `.github/workflows/stage-a0-container.yml`
- Delete: `docs/superpowers/specs/2026-08-31-fogcast-naming-cleanup-design.md`
- Delete: `docs/superpowers/plans/2026-08-31-fogcast-naming-cleanup.md`

**Interfaces:**
- Consumes: the completed canonical file paths and commands.
- Produces: one current README/architecture/development/policy set with no historical handoff vocabulary.

- [ ] **Step 1: Rewrite documentation in present tense**

Document the working game path, exact target-image build/deploy/smoke
commands, `/media/fat/fogcast`, the dedicated target, repository boundaries,
and the next development-RBF goal. Remove old branch, stage, experiment, and
recovery narratives. Keep `IDEA.md` as product vision only.

- [ ] **Step 2: Delete stale workflow and temporary plans**

Delete the stage workflow and these design/plan files. Do not create a
replacement roadmap; `docs/ARCHITECTURE.md` remains canonical.

- [ ] **Step 3: Audit tracked names**

```sh
git ls-files | rg -i 'poc[0-9]|stage-a0|mister[-_]remote|remote-play-spike'
git grep -nIi -E 'poc[0-9]|stage-a0|poc6|fpgadev|native-coordinator|mister[-_]remote|remote-play-spike' -- .
```

Expected: both audits produce no matches. Database `Rollback`, content
staging, ROM `prototype`, and current remote-play names are not failures.

- [ ] **Step 4: Run repository checks**

```sh
go test ./...
node --test internal/hostapi/ui_metadata_test.js internal/hostapi/ui_app_test.js
node --test internal/hostapi/ui_browser_test.js
go vet ./...
git diff --check
```

Expected: Go and vet exit zero; UI reports 287 passing unit tests and the
browser fixture test passes or skips only because Chrome is unavailable.

- [ ] **Step 5: Commit**

```sh
git add -A
git commit -m "docs: make the working FogCast path canonical"
```

### Task 7: Build, deploy, and verify the dedicated target

**Files:**
- Generated: `build/output/target-image/dev/linux.img`
- External target: `/media/fat/linux/linux.img`
- External target: `/media/fat/fogcast/{mister-agent,agent.toml}`

**Interfaces:**
- Consumes: completed build/deploy/smoke commands and Sonic game ID `megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1`.
- Produces: a reboot-persistent, healthy target that launches `MegaDrive` and returns to `MENU`.

- [ ] **Step 1: Build the canonical development image from the new volume**

```sh
make target-image-fetch
make target-image-dev
```

Expected: the build exits zero, curl exists in the image, ffmpeg/SDL are
absent, and the image is published under `build/output/target-image/dev`.

- [ ] **Step 2: Run image verification**

```sh
make target-image-verify
```

Expected: development image verification exits zero.

- [ ] **Step 3: Perform the one-time FAT-directory migration**

```sh
sshpass -p 1 ssh -o StrictHostKeyChecking=no \
  -o UserKnownHostsFile=/dev/null root@192.168.10.239 \
  'mkdir -p /media/fat/fogcast && \
   mv /media/fat/mister-remote/mister-agent /media/fat/fogcast/mister-agent && \
   mv /media/fat/mister-remote/agent.toml /media/fat/fogcast/agent.toml && \
   sed -i "s#/tmp/mister-remote#/tmp/fogcast#g" /media/fat/fogcast/agent.toml && \
   rmdir /media/fat/mister-remote'
```

Expected: only `/media/fat/fogcast` remains. The currently running agent is
unaffected until reboot.

- [ ] **Step 4: Deploy and reboot**

```sh
make target-image-deploy
```

Expected: upload, FAT-directory migration, atomic rename, sync, and reboot
complete without an old-path alias.

- [ ] **Step 5: Run the real launch smoke**

```sh
make target-smoke \
  GAME_ID=megadrive-sonic-the-hedgehog-2-world-rev-a-a6e9fedc03e1 \
  EXPECTED_CORE=MegaDrive
```

Expected: both APIs report healthy, Sonic launches into `MegaDrive`, stop
returns `/tmp/CORENAME` to `MENU`, and the command exits zero.

- [ ] **Step 6: Verify final source state**

```sh
git status --short
git log -6 --oneline
```

Expected: clean working tree and coherent cleanup commits.

### Task 8: Rename the GitHub repository last

**Files:**
- External: GitHub repository `DeanoC/FogCast-POC`
- Modify: local Git remote `origin`
- Rename: local checkout directory `FogCast-POC` to `FogCast`

**Interfaces:**
- Consumes: verified local branch and verified physical target.
- Produces: GitHub and local checkout identity `FogCast`.

- [ ] **Step 1: Rename the GitHub repository**

```sh
gh repo rename FogCast --repo DeanoC/FogCast-POC --yes
git remote set-url origin https://github.com/DeanoC/FogCast.git
```

Expected: GitHub reports `DeanoC/FogCast` and the old URL redirects.

- [ ] **Step 2: Verify the renamed remote**

```sh
gh repo view DeanoC/FogCast --json nameWithOwner,url,visibility
git ls-remote origin HEAD
```

Expected: both commands resolve the renamed repository.

- [ ] **Step 3: Rename the local checkout and perform the final audit**

```sh
mv /home/deano/fes/recovery/FogCast-POC /home/deano/fes/recovery/FogCast
git -C /home/deano/fes/recovery/FogCast status --short
git -C /home/deano/fes/recovery/FogCast remote -v
```

Expected: the working tree is clean and both origin URLs use
`https://github.com/DeanoC/FogCast.git`.
