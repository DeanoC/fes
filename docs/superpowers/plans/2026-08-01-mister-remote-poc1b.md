# MiSTer Remote POC 1B Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Boot the unchanged MiSTer Remote v1 service on a reproducible, read-only, reduced Buildroot image, first beneath the accepted binary kernel and then beneath a kernel/device tree rebuilt from pinned official source, passing the same hardware suite at both checkpoints.

**Architecture:** Retain the MiSTer Pi bootloader and all accepted FAT-side FPGA assets. Buildroot produces only `/media/fat/linux/linux.img`; `Main_MiSTer`, Menu, the two cores, ROMs, controller map, agent token, and generated MGL remain under `/media/fat`. Checkpoint 1 swaps only `linux.img` and verifies the accepted `zImage_dtb` hash before boot. Checkpoint 2 keeps that accepted root image and swaps only a `zImage_dtb` assembled from a pinned upstream kernel commit and its unmodified `MiSTer_defconfig`.

**Tech Stack:** Go 1.26.5, Buildroot 2021.02.4, ARMv7 Cortex-A9 hard-float glibc userland, BusyBox init/mdev/udhcpc, Dropbear in the development image only, ext4 loop-root image, Docker-compatible pinned Linux build container, official MiSTer 5.15 kernel source, shell deployment gates, and the existing macOS HIL runner.

## Global Constraints

- The SuperStation One is out of scope and must not be modified.
- The dedicated complete MiSTer Pi is the only POC 1B hardware target.
- The v1 HTTP protocol, public `host` package, CLI behavior, two game IDs, target ROM paths, core registry, MGL output, and HIL sequence do not change.
- The accepted binary kernel is `/media/fat/linux/zImage_dtb`, SHA-256 `a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae`, size `7380857`, release `5.15.1-MiSTer`.
- The accepted FAT artifacts remain byte-identical: `MiSTer` `7ca3cd2f224b9264d0889f593a0d77aafa5adda61910baba92c5ae401e26fcce`, `menu.rbf` `821bcf66181a00ff550e4a4110dc11c9fa8e68d38e9cb5558b3ddb99ca938934`, Mega Drive core `0cd43ea2c96e726999f04924713ca090ae73829f3ab08109c6b552cebeba0839`, SNES core `4960eab619aef92596237dff46403a0e5e112c16b486ce659e8ee2e4a9b4bdcd`, and controller map `7300816d6f58971e266d800a6a780b10459115374aab9f321b28a29e75173d31`.
- Official image inputs are pinned to `MiSTer-devel/Linux_Image_creator_MiSTer` commit `8aba321b2162e54b56522aa30758b22d97eec8da`; its accepted `rootfs.tar.bz2` is `65d968c566e5971debd6f822ebf13cc4555ad1bd69fa4a81fa30367f2323c344`, `modules.tar.gz` is `62086a04e09b98162cc5db6d4ac607a2fdb41bbec02035f3318074a85ab9f2fe`, and `zImage_dtb` matches the accepted binary kernel.
- Buildroot is tag `2021.02.4`, dereferenced commit `004a792dcf10e6c474070c9571f7504411e786cc`.
- The reproduced kernel source candidate is official `MiSTer-devel/Linux-Kernel_MiSTer` commit `d7adb20b4ca595838289406c083fff78f004a8c3`, the last source commit before the accepted `2025-04-02` binary release. It is accepted as the exact source pin only if its unmodified `MiSTer_defconfig` produces release `5.15.1-MiSTer` and the final hardware checkpoint passes.
- Do not prune kernel drivers, change `MiSTer_defconfig`, rebuild `Main_MiSTer`, rebuild either FPGA core, add Wi-Fi/Bluetooth, add NAS access, or add streaming.
- The production root filesystem has no SSH server. The development root filesystem has Dropbear, but final gameplay must pass with Dropbear stopped.
- `/` is mounted read-only. `/dev`, `/dev/shm`, `/proc`, `/run`, `/sys`, `/tmp`, and `/var/log` are volatile; persistent configuration, token, cores, ROMs, saves, and controller maps stay on `/media/fat`.
- Neither image, package, report, build log, nor Git history may contain the bearer token or ROM content/path outside the already ignored local host manifest.
- A deployment must make one-time FAT-side backups before replacing `linux.img` or `zImage_dtb`; it must refuse an unexpected current hash and provide an offline restore command suitable for a Mac-mounted SD card.

---

## File Map

| Path | Responsibility |
|---|---|
| `build/sources.poc1b.lock.toml` | Immutable source revisions, container digest, input hashes, and output hashes |
| `internal/imagepoc/lock.go` | Parse and validate both accepted POC 1A inventory and POC 1B source lock |
| `cmd/poc1b-lock/main.go` | Resolve/verify source material and emit the deterministic POC 1B lock |
| `containers/poc1b/Dockerfile` | Pinned x86_64 Linux build environment |
| `scripts/poc1b-container.sh` | Single container-runtime boundary for all image/kernel builds |
| `buildroot/external.desc`, `Config.in`, `external.mk` | Buildroot external-tree declaration |
| `buildroot/configs/mister_remote_poc1b_{prod,dev}_defconfig` | Reproducible production/development configurations |
| `buildroot/board/mister-remote/rootfs-overlay/` | Read-only init, mounts, DHCP, Main, agent, and supervision policy |
| `buildroot/board/mister-remote/post-build.sh` | Enforce rootfs allowlist, permissions, symlinks, and secret absence |
| `scripts/fetch-poc1b-sources.sh` | Fetch pinned Buildroot, official image inputs, and kernel source |
| `scripts/build-poc1b-image.sh` | Build production and development ext4 images twice reproducibly |
| `scripts/build-poc1b-kernel.sh` | Build unpruned pinned kernel, modules, DTB, and combined `zImage_dtb` |
| `scripts/verify-poc1b-image.sh` | Mount-free ext4 inspection, dependency closure, read-only, and QEMU smoke gates |
| `deploy/poc1b/install-target.sh` | Hash-gated target-side checkpoint install and one-time backup |
| `scripts/install-poc1b.sh` | Mac-to-MiSTer transport wrapper |
| `scripts/restore-poc1a-sd.sh` | Offline recovery for a Mac-mounted FAT volume |
| `docs/runbooks/poc1b-deploy.md` | Flash, recovery, checkpoint, and physical acceptance procedure |

---

### Task 1: Lock Parser and POC 1B Provenance

**Files:**
- Create: `internal/imagepoc/lock.go`
- Create: `internal/imagepoc/lock_test.go`
- Create: `cmd/poc1b-lock/main.go`
- Create from verified inputs: `build/sources.poc1b.lock.toml`
- Modify: `.gitignore`

**Interfaces:**
- Consumes: `build/sources.poc1a.lock.toml` and fetched files below `build/cache/poc1b/`.
- Produces: `imagepoc.LoadPOC1A(path string) (Accepted, error)`, `imagepoc.LoadPOC1B(path string) (Sources, error)`, `imagepoc.VerifyFile(path, sha256 string, size int64) error`, and a secret-free format-1 POC 1B lock.

- [x] **Step 1: Write failing table-driven lock tests**

Test these cases using `t.TempDir()` fixtures: the committed POC 1A lock loads six artifacts and fourteen libraries; required accepted hashes match the Global Constraints; duplicate names fail; malformed SHA-256 fails; relative target paths fail; a POC 1B lock missing the immutable container digest fails; a valid source lock round-trips in stable TOML field order; `VerifyFile` accepts exact bytes and rejects changed size or digest.

- [x] **Step 2: Run the lock tests and verify red**

Run: `mise exec go@1.26.5 -- go test ./internal/imagepoc -v`

Expected: FAIL because `internal/imagepoc` does not exist.

- [x] **Step 3: Implement strict typed locks**

Use `github.com/pelletier/go-toml/v2`; reject unknown fields with `toml.NewDecoder(r).DisallowUnknownFields()`. Require lowercase 64-character hex digests, positive sizes, absolute accepted target paths, full 40-character Git commits, an OCI digest beginning `sha256:`, and unique artifact/library names. Do not add a second TOML dependency.

The POC 1B lock schema is:

```toml
format = 1

[container]
image = "docker.io/library/debian:12.11-slim"
platform = "linux/amd64"

[buildroot]
version = "2021.02.4"
commit = "004a792dcf10e6c474070c9571f7504411e786cc"

[image_creator]
commit = "8aba321b2162e54b56522aa30758b22d97eec8da"
rootfs_sha256 = "65d968c566e5971debd6f822ebf13cc4555ad1bd69fa4a81fa30367f2323c344"
modules_sha256 = "62086a04e09b98162cc5db6d4ac607a2fdb41bbec02035f3318074a85ab9f2fe"
kernel_sha256 = "a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae"

[kernel]
commit = "d7adb20b4ca595838289406c083fff78f004a8c3"
defconfig = "MiSTer_defconfig"
dtb = "socfpga_cyclone5_de10_nano.dtb"
release = "5.15.1-MiSTer"
```

The CLI has `resolve --container-runtime docker`, `verify-inputs`, and `record-outputs --prod PATH --dev PATH --kernel PATH`. `resolve` asks the runtime for the immutable manifest digest, validates it as `sha256:` followed by 64 lowercase hexadecimal characters, and writes it as `container.digest` in an atomic lock with the `outputs` table omitted. `record-outputs` refuses dirty/missing inputs, computes output hashes, and atomically adds `outputs.prod_rootfs_sha256`, `outputs.dev_rootfs_sha256`, and `outputs.reproduced_kernel_sha256`.

- [x] **Step 4: Add generated-cache exclusions**

Add `/build/cache/` and `/build/output/` to `.gitignore`. Keep both `build/sources.poc1a.lock.toml` and `build/sources.poc1b.lock.toml` tracked.

- [x] **Step 5: Run focused and repository tests**

Run:

```bash
mise exec go@1.26.5 -- go test -race ./internal/imagepoc ./cmd/poc1b-lock -v
mise exec go@1.26.5 -- make check
git diff --check
```

Expected: PASS with no writes outside ignored build output.

- [x] **Step 6: Commit provenance support**

```bash
git add .gitignore internal/imagepoc cmd/poc1b-lock
git commit -m "build: add strict POC 1B provenance lock"
```

---

### Task 2: Pinned Container and Source Fetch Boundary

**Files:**
- Create: `containers/poc1b/Dockerfile`
- Create: `scripts/poc1b-container.sh`
- Create: `scripts/fetch-poc1b-sources.sh`
- Create: `scripts/tests/poc1b-sources_test.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes: immutable fields in `build/sources.poc1b.lock.toml`, a Docker-compatible runtime, and network only during `fetch`.
- Produces: verified source trees at `build/cache/poc1b/{buildroot,image-creator,linux-kernel}` and a container invoked only by digest on `linux/amd64`.

- [x] **Step 1: Write fixture-based failing source tests**

Use fake `git`, `docker`, and `sha256sum` executables prepended to `PATH`. Assert that branch names, tags without resolved commits, digest-less images, a mismatched image-creator kernel, a changed tarball, or a source tree whose `HEAD` differs from the lock all fail. Assert that a valid fixture produces exactly the three source directories and never prints the token-like fixture string.

- [x] **Step 2: Run the shell test and verify red**

Run: `sh scripts/tests/poc1b-sources_test.sh`

Expected: FAIL because the scripts do not exist.

- [x] **Step 3: Add the pinned build container**

The Dockerfile begins `FROM docker.io/library/debian:12.11-slim@${resolved digest from the lock}` when rendered by `poc1b-container.sh`, installs only Buildroot/kernel host prerequisites (`build-essential`, `bc`, `bison`, `cpio`, `file`, `flex`, `git`, `libelf-dev`, `libncurses-dev`, `libssl-dev`, `python3`, `qemu-system-arm`, `qemu-user-static`, `rsync`, `unzip`, `wget`, `xz-utils`), creates uid/gid supplied as build arguments, and sets `/work` as the working directory. The wrapper must:

```text
1. read and validate the committed lock with bin/poc1b-lock;
2. compare `docker image inspect` RepoDigests to the locked digest;
3. build with `--platform linux/amd64` if absent;
4. run with the repository mounted read/write at /work and no token environment variable;
5. add `--network none` for every command except `fetch`.
```

- [x] **Step 4: Implement immutable fetches**

Clone with `--filter=blob:none`, fetch each exact full commit, detach at that commit, and verify `git rev-parse HEAD`. Fetch Buildroot commit `004a792...`, image-creator commit `8aba321...`, and kernel commit `d7adb20...`. Verify the three image-creator file hashes from the lock before returning. Never use GitHub-generated source archives.

- [x] **Step 5: Wire and verify the build-host gate**

Add `poc1b-resolve`, `poc1b-fetch`, and the shell test to `make test`. `poc1b-resolve` must fail with an actionable message when no container runtime exists; it must never silently fall back to native macOS compilation.

- [x] **Step 6: Run tests and commit**

```bash
sh scripts/tests/poc1b-sources_test.sh
mise exec go@1.26.5 -- make check
git diff --check
git add containers scripts Makefile
git commit -m "build: pin POC 1B container and sources"
```

---

### Task 3: Buildroot External Tree and Read-Only Appliance Init

**Files:**
- Create: `buildroot/external.desc`
- Create: `buildroot/Config.in`
- Create: `buildroot/external.mk`
- Create: `buildroot/configs/mister_remote_poc1b_prod_defconfig`
- Create: `buildroot/configs/mister_remote_poc1b_dev_defconfig`
- Create: `buildroot/board/mister-remote/rootfs-overlay/etc/fstab`
- Create: `buildroot/board/mister-remote/rootfs-overlay/etc/inittab`
- Create: `buildroot/board/mister-remote/rootfs-overlay/etc/init.d/S20mister-network`
- Create: `buildroot/board/mister-remote/rootfs-overlay/etc/init.d/S40mister-main`
- Create: `buildroot/board/mister-remote/rootfs-overlay/etc/init.d/S50mister-agent`
- Create: `buildroot/board/mister-remote/rootfs-overlay/usr/sbin/mister-supervise`
- Create: `buildroot/board/mister-remote/post-build.sh`
- Test: `scripts/tests/poc1b-rootfs_test.sh`

**Interfaces:**
- Consumes: `bin/mister-agent-linux-armv7`, FAT paths from POC 1A, and Buildroot `BR2_EXTERNAL`.
- Produces: a 64 MiB ext4 image with BusyBox init, mdev, udhcpc, the measured `Main_MiSTer` library closure, and supervised Main/agent processes.

- [x] **Step 1: Write failing static policy tests**

Assert both defconfigs select Cortex-A9, EABIhf, glibc, BusyBox init, dynamic mdev, ext4, reproducible builds, Imlib2, FreeType, libpng, bzip2, zlib, BlueZ libraries, and libstdc++; only dev selects Dropbear. Assert no Wi-Fi, Samba, FTP, Python, compiler, package manager, Bluetooth daemon, or writable-root option. Assert every service uses absolute paths and bounded waits.

- [x] **Step 2: Run and verify red**

Run: `sh scripts/tests/poc1b-rootfs_test.sh`

Expected: FAIL because the external tree does not exist.

- [x] **Step 3: Add the exact Buildroot configurations**

Both defconfigs set:

```text
BR2_arm=y
BR2_cortex_a9=y
BR2_ARM_ENABLE_VFP=y
BR2_ARM_EABIHF=y
BR2_TOOLCHAIN_BUILDROOT_GLIBC=y
BR2_INIT_BUSYBOX=y
BR2_ROOTFS_DEVICE_CREATION_DYNAMIC_MDEV=y
BR2_ROOTFS_MERGED_USR=y
BR2_TARGET_GENERIC_HOSTNAME="mister"
BR2_TARGET_GENERIC_ISSUE="MiSTer Remote POC 1B"
BR2_SYSTEM_DHCP=""
BR2_REPRODUCIBLE=y
BR2_PACKAGE_IMLIB2=y
BR2_PACKAGE_FREETYPE=y
BR2_PACKAGE_LIBPNG=y
BR2_PACKAGE_BZIP2=y
BR2_PACKAGE_ZLIB=y
BR2_PACKAGE_BLUEZ5_UTILS=y
BR2_TOOLCHAIN_BUILDROOT_CXX=y
BR2_TARGET_ROOTFS_EXT2=y
BR2_TARGET_ROOTFS_EXT2_4=y
BR2_TARGET_ROOTFS_EXT2_SIZE="64M"
BR2_ROOTFS_OVERLAY="${BR2_EXTERNAL_MISTER_REMOTE_PATH}/board/mister-remote/rootfs-overlay"
BR2_ROOTFS_POST_BUILD_SCRIPT="${BR2_EXTERNAL_MISTER_REMOTE_PATH}/board/mister-remote/post-build.sh"
```

`BR2_TOOLCHAIN_BUILDROOT_CXX` is the selectable Buildroot 2021.02.4 option; its resolved configuration must contain the internal `BR2_INSTALL_LIBSTDCPP=y` symbol.

Use `make olddefconfig` inside the pinned Buildroot tree to resolve implied options, then save the complete defconfigs with `savedefconfig`. Dev adds `BR2_PACKAGE_DROPBEAR=y`; production explicitly leaves it unset.

- [x] **Step 4: Implement read-only initialization**

`fstab` mounts `/` read-only/noatime, plus proc, sysfs, devpts, `/dev/shm`, `/run`, `/tmp`, and `/var/log` as volatile filesystems. `inittab` mounts pseudo-filesystems, runs `mdev -s`, starts `rcS`, and exposes only serial/console recovery in dev.

`S20mister-network` waits up to 10 seconds for `eth0`, then executes `udhcpc -f -q -t 5 -T 2 -i eth0 -x hostname:mister -s /usr/share/udhcpc/default.script`. It stores PID/state only under `/run`.

`S40mister-main` refuses a wrong accepted `MiSTer` or `menu.rbf` hash, then supervises `/media/fat/MiSTer /media/fat/menu.rbf`. `S50mister-agent` waits up to 30 seconds for `/dev/MiSTer_cmd`, then supervises `/usr/sbin/mister-agent --config /media/fat/mister-remote/agent.toml`. A stopped dependency remains visible through the existing health booleans.

`mister-supervise NAME COMMAND...` writes `/run/NAME.pid`, traps TERM/INT, terminates and waits for its child, logs timestamped lifecycle events to `/var/log/NAME.log`, and restarts after one second. It never prints command arguments for the agent, preventing the config path from becoming structured request data.

- [x] **Step 5: Enforce the final root tree**

`post-build.sh` copies the freshly built agent, removes SSH material from production, rejects regular files under `/root`, rejects `*.rom`, `*.sfc`, `*.smc`, `*.md`, `*.gen`, `*.zip`, `agent.toml`, and strings matching `token =`, fixes root ownership, makes init scripts executable, and verifies all fourteen logical library paths from the POC 1A lock resolve inside `TARGET_DIR`.

- [x] **Step 6: Test and commit**

```bash
sh scripts/tests/poc1b-rootfs_test.sh
mise exec go@1.26.5 -- make check
git diff --check
git add buildroot scripts/tests Makefile
git commit -m "build: define reduced MiSTer appliance rootfs"
```

---

### Task 4: Deterministic Image Build and Image Inspection

**Files:**
- Create: `scripts/build-poc1b-image.sh`
- Create: `scripts/verify-poc1b-image.sh`
- Create: `scripts/qemu-smoke-poc1b.sh`
- Create: `scripts/tests/poc1b-image_test.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes: pinned container, fetched Buildroot, external tree, and agent binary.
- Produces: `build/output/poc1b/{prod,dev}/linux.img`, manifests, library reports, and reproducibility evidence.

- [x] **Step 1: Write failing orchestration tests**

With fake Buildroot output, assert: builds run twice in separate output directories; `SOURCE_DATE_EPOCH=1751459412` is constant; differing images fail; missing fourteen-library closure fails; prod containing Dropbear fails; dev lacking Dropbear fails; `/etc/fstab` without `ro` fails; a token/ROM signature fails; and a valid fixture promotes output atomically.

- [x] **Step 2: Run and verify red**

Run: `sh scripts/tests/poc1b-image_test.sh`

Expected: FAIL because build/verify scripts do not exist.

- [x] **Step 3: Implement two-build reproducibility**

For each variant, create clean `O=build/output/poc1b/work-{1,2}-VARIANT`, run the pinned defconfig and Buildroot, normalize ext4 creation with UUID/hash seed `9b3652c2-33f1-4a6b-9a53-9b667ab1b001`, disabled lazy inode/journal initialization, and the fixed epoch, compare the two SHA-256 values, then atomically copy the second output to `build/output/poc1b/VARIANT/linux.img`. Never run Buildroot as root.

- [x] **Step 4: Implement mount-free inspection**

Inside the container use `debugfs -R`, `file`, `readelf`, and `strings`; do not mount images on macOS. Verify:

```text
filesystem size <= 64 MiB;
/sbin/init, /usr/sbin/mister-agent, BusyBox, and all service scripts exist;
all fourteen POC 1A logical library names resolve;
Main_MiSTer's recorded DT_NEEDED entries are available;
prod has no dropbear and dev has exactly one dropbear server;
root fstab is ro and all writable runtime paths are tmpfs/devtmpfs;
no token, ROM, core, controller map, compiler, or package-manager payload exists;
agent is ELF ARM EABI5, statically linked, stripped;
manifest paths and hashes are stable and sorted.
```

- [x] **Step 5: Add the closest practical boot smoke**

Build a test-only `vexpress-a9` kernel in the same container and boot each root image under headless `qemu-system-arm -M vexpress-a9 -append 'root=/dev/mmcblk0 ro console=ttyAMA0'`. The service scripts must enter a bounded `waiting for /media/fat` state rather than crash-loop; the console must show read-only root and writable `/run`, `/tmp`, and `/var/log`. Kill QEMU after the sentinel `POC1B_SMOKE_READY` appears or fail at 45 seconds. Do not claim this emulates FPGA behavior.

- [x] **Step 6: Run build and tests**

```bash
mise exec go@1.26.5 -- make build-agent VERSION=0.1.0
make poc1b-image-test
make poc1b-images
make poc1b-verify-images
git diff --check
```

Expected: both repeated hashes match, both QEMU smokes pass, and ignored output contains no secret.

- [x] **Step 7: Commit the image pipeline**

```bash
git add scripts Makefile
git commit -m "build: generate reproducible POC 1B images"
```

---

### Task 5: Pinned Unpruned Kernel and Device Tree Build

**Files:**
- Create: `scripts/build-poc1b-kernel.sh`
- Create: `scripts/verify-poc1b-kernel.sh`
- Create: `scripts/tests/poc1b-kernel_test.sh`
- Modify: `Makefile`

**Interfaces:**
- Consumes: official kernel commit `d7adb20...`, its unmodified `arch/arm/configs/MiSTer_defconfig`, and the Buildroot ARM hard-float cross-toolchain.
- Produces: `build/output/poc1b/kernel/{zImage,MiSTer.dtb,zImage_dtb,modules.tar.gz,config,manifest.toml}`.

- [ ] **Step 1: Write failing kernel assembly tests**

Fixture tests assert the source HEAD and committed defconfig hash are verified before build; `LOCALVERSION` is not overridden; the generated `.config` equals `make MiSTer_defconfig` plus only deterministic Kconfig normalization; driver-pruning diffs fail; DTB is appended after zImage exactly once; kernel release must equal `5.15.1-MiSTer`; and two builds with the fixed epoch produce identical outputs.

- [ ] **Step 2: Run and verify red**

Run: `sh scripts/tests/poc1b-kernel_test.sh`

Expected: FAIL because kernel scripts do not exist.

- [ ] **Step 3: Implement the pinned build**

Inside the pinned container run:

```bash
make -C "$kernel" O="$out" ARCH=arm CROSS_COMPILE="$cross" MiSTer_defconfig
make -C "$kernel" O="$out" ARCH=arm CROSS_COMPILE="$cross" -j"$(nproc)" zImage socfpga_cyclone5_de10_nano.dtb modules
make -C "$kernel" O="$out" ARCH=arm CROSS_COMPILE="$cross" INSTALL_MOD_PATH="$modules" modules_install
```

Copy zImage and DTB separately, concatenate them to `zImage_dtb`, archive modules with sorted names/fixed ownership/fixed epoch, record the compiler version and hashes, and compare two clean builds before promotion.

- [ ] **Step 4: Verify provenance without demanding the old binary hash**

Require release `5.15.1-MiSTer`, the pinned source/defconfig/DTB, a nonempty DTB accepted by `dtc`, and required config symbols for SoCFPGA, dwmac Ethernet, DWC2 USB host, USB HID/input event, devtmpfs, loop, ext4, VFAT/exFAT, tmpfs, and FPGA manager. The new kernel hash is expected to differ from the accepted binary because compiler metadata may differ; record it, never disguise it as a byte reproduction.

- [ ] **Step 5: Build, verify, and commit**

```bash
make poc1b-kernel-test
make poc1b-kernel
make poc1b-verify-kernel
git diff --check
git add scripts Makefile
git commit -m "build: reproduce pinned MiSTer kernel and DTB"
```

---

### Task 6: Hash-Gated Deployment and Offline Recovery

**Files:**
- Create: `deploy/poc1b/install-target.sh`
- Create: `scripts/install-poc1b.sh`
- Create: `scripts/restore-poc1a-sd.sh`
- Create: `scripts/tests/install-poc1b-target_test.sh`
- Create: `docs/runbooks/poc1b-deploy.md`
- Modify: `Makefile`

**Interfaces:**
- Consumes: checkpoint name `binary-kernel` or `source-kernel`, accepted/current hashes, root image, optional kernel image, and SSH only for installation/recovery.
- Produces: one-time backups, atomic FAT replacements, a checkpoint marker, and an offline restore path.

- [ ] **Step 1: Write destructive-path fixture tests first**

Using `MISTER_REMOTE_ROOT=$(mktemp -d)`, assert the installer refuses: a root other than the fixture or empty target root in tests; missing expected FAT assets; changed accepted kernel at checkpoint 1; changed accepted root image at checkpoint 2; absent output hash in the lock; a package containing token/ROM data; repeated backups with different content; and path traversal. Assert interruption before rename leaves the prior file intact. Assert restore requires all backups and restores their exact hashes.

- [ ] **Step 2: Run and verify red**

Run: `sh scripts/tests/install-poc1b-target_test.sh`

Expected: FAIL because the deployment scripts do not exist.

- [ ] **Step 3: Implement checkpoint 1**

Verify all accepted FAT artifact hashes, copy `linux.img` once to `linux.img.pre-poc1b`, copy `zImage_dtb` once to `zImage_dtb.pre-poc1b`, fsync, upload the new development root as `linux.img.poc1b.new`, verify its locked hash on target, stop Main/agent cleanly, and atomically rename it to `linux.img`. Do not replace `zImage_dtb`, `uboot.img`, `menu.rbf`, Main, cores, ROMs, map, or `u-boot.txt`.

- [ ] **Step 4: Implement checkpoint 2**

Require the currently installed root hash equals the accepted POC 1B dev image and the current kernel still equals the POC 1A binary hash. Upload `zImage_dtb.poc1b.new`, verify the recorded reproduced hash, fsync, and atomically rename it to `zImage_dtb`. Copy matching modules into a versioned directory without deleting the accepted modules.

- [ ] **Step 5: Implement offline recovery**

`scripts/restore-poc1a-sd.sh /Volumes/MISTER` resolves the volume path, requires the precise `/linux/*.pre-poc1b` backups, displays hashes, and only then restores `linux.img` and `zImage_dtb` using same-volume temporary files plus rename. It never formats or recursively deletes a volume.

- [ ] **Step 6: Document the physical gate**

The runbook requires a freshly prepared spare SD or a verified full-card image before checkpoint 1, HDMI and controller connected, Ethernet connected, the SuperStation One powered off/untouched, the offline restore command visible on the MacBook, and the ability to remove/mount the dedicated MiSTer Pi SD if boot networking fails.

- [ ] **Step 7: Test and commit**

```bash
sh scripts/tests/install-poc1b-target_test.sh
mise exec go@1.26.5 -- make check
git diff --check
git add deploy scripts docs/runbooks Makefile
git commit -m "build: add recoverable POC 1B deployment gates"
```

---

### Task 7: Binary-Kernel Hardware Checkpoint

**Files:**
- Produce ignored: `artifacts/hil/poc1b-binary-kernel.json`
- Record output hashes: `build/sources.poc1b.lock.toml`
- Update observations only: `docs/runbooks/poc1b-deploy.md`

**Interfaces:**
- Consumes: development `linux.img`, accepted binary `zImage_dtb`, existing host config/game manifest, and unchanged HIL binary.
- Produces: the first passing appliance evidence gate.

- [ ] **Step 1: Finish and lock reproducible outputs**

Run `poc1b-lock record-outputs` for prod/dev root images and the reproduced kernel, then rerun `verify-inputs`, build both images again, and require identical locked hashes. Audit the lock for ROM paths, tokens, and bearer strings before committing it.

- [ ] **Step 2: Prepare recovery and install checkpoint 1**

Follow the runbook, confirm the accepted kernel hash on target, install only the development root image, and cold power-cycle. If health does not become ready in 45 seconds, stop; recover the SD offline; inspect serial/boot logs only after recovery; do not advance to checkpoint 2.

- [ ] **Step 3: Prove the root policy before HIL**

Over development SSH verify `/` reports `ro`; `/run`, `/tmp`, and `/var/log` are writable tmpfs; `/media/fat` is writable; `uname -r` is `5.15.1-MiSTer`; kernel file hash is still the accepted binary; production image has no SSH; and health is ready. Store no token in shell history.

- [ ] **Step 4: Stop SSH and run the identical HIL suite**

Stop Dropbear and verify its port/process is absent, close SSH, then run:

```bash
bin/mister-hil --config ~/.config/mister-remote/config.toml --output artifacts/hil/poc1b-binary-kernel.json
```

Answer only observations actually seen for both games, HDMI video/audio, controller playability, black idle, power-cycle recovery, agent-only restart reconciliation, invalid cases, and ten alternating launches. Do not use SSH during the runner.

- [ ] **Step 5: Verify report and independence**

Require the report has the same named checks as `artifacts/hil/poc1a.json`, `passed: true`, no failed checks, and no secret/ROM paths. With SSH still stopped, independently launch Mega Drive, launch SNES, and stop using `misterctl`.

- [ ] **Step 6: Commit the first hardware gate**

```bash
git add build/sources.poc1b.lock.toml docs/runbooks/poc1b-deploy.md
git commit -m "test: accept reduced rootfs on binary MiSTer kernel"
```

The ignored HIL JSON remains local evidence and is summarized by hash in the runbook.

---

### Task 8: Reproduced-Kernel Hardware Checkpoint and POC 1 Completion

**Files:**
- Produce ignored: `artifacts/hil/poc1b-source-kernel.json`
- Modify observations: `docs/runbooks/poc1b-deploy.md`

**Interfaces:**
- Consumes: the already accepted POC 1B root image, reproduced kernel/DTB/modules, and unchanged HIL runner.
- Produces: final POC 1 acceptance evidence with a clean, recoverable implementation branch.

- [ ] **Step 1: Install only the reproduced kernel checkpoint**

Verify the current root image hash equals the accepted checkpoint-1 hash and current kernel equals the POC 1A hash. Run the source-kernel installer, cold power-cycle, and require health within 45 seconds. On failure, restore only `zImage_dtb.pre-poc1b` offline and keep the accepted POC 1B root image for diagnosis.

- [ ] **Step 2: Verify source identity and root policy**

Over development SSH verify `uname -r` remains `5.15.1-MiSTer`, the FAT kernel hash equals `outputs.reproduced_kernel_sha256`, the loaded module tree matches the built release, root remains read-only, volatile mounts remain writable, and all accepted FAT artifact hashes remain unchanged.

- [ ] **Step 3: Stop SSH and rerun the identical suite**

Stop Dropbear, close SSH, then run:

```bash
bin/mister-hil --config ~/.config/mister-remote/config.toml --output artifacts/hil/poc1b-source-kernel.json
```

Require the exact POC 1A check-name set, all checks true, ten alternating launches, manual playable video/audio/controller confirmation for both games, black idle, invalid-request stability, power-cycle readiness, and agent restart reconciliation.

- [ ] **Step 4: Run the final no-SSH control proof**

With SSH still stopped:

```bash
bin/misterctl launch megadrive-test
bin/misterctl launch snes-test
bin/misterctl stop
bin/misterctl status --json
```

Expected: both launches become active and final state is idle with nullable game/system/core fields cleared as specified by v1.

- [ ] **Step 5: Run all fresh software/build verification**

```bash
mise exec go@1.26.5 -- make check
mise exec go@1.26.5 -- make build VERSION=0.1.0
make package-test VERSION=0.1.0
make poc1b-images
make poc1b-verify-images
make poc1b-kernel
make poc1b-verify-kernel
git diff --check
git status --short --branch
```

Expected: every command exits zero; repeat builds match locked outputs; only intended runbook evidence changes remain.

- [ ] **Step 6: Commit final acceptance**

```bash
git add docs/runbooks/poc1b-deploy.md
git commit -m "test: accept reproduced MiSTer appliance kernel"
git status --short --branch
```

Expected: clean `feat/poc1a` worktree. At this point POC 1 is complete; GUI/library discovery, NAS cataloging, host-side emulation, streaming, generic controllers, Wi-Fi/Bluetooth, OTA/A-B updates, and kernel pruning remain separate later POCs.

---

## POC 1B Stop Gates

Stop and recover instead of advancing when any of these occurs:

1. A build input cannot be verified against the committed full revision/hash.
2. Repeated root or kernel builds differ.
3. The production image contains SSH or either image contains a token/ROM.
4. The root mounts writable or a required volatile mount is absent.
5. Any accepted FAT artifact changes unexpectedly.
6. Checkpoint 1 does not pass the complete unchanged HIL suite.
7. The source kernel reports a release other than `5.15.1-MiSTer` or changes `MiSTer_defconfig`.
8. Checkpoint 2 does not pass the complete unchanged HIL suite.

Never use a failed checkpoint as the base for the next checkpoint.
