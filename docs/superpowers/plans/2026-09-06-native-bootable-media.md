# Native Bootable Media Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add deterministic, verified `make media` output that wraps the current cold-built native rootfs in a flashable DE10-Nano disk image.

**Architecture:** Keep FogCast as the rootfs authority and add a separate FES media layer. A host orchestrator validates cold-build evidence and locked boot inputs, runs an unprivileged pinned container twice, verifies the resulting MBR/FAT/raw-boot image, and publishes a content-addressed immutable generation through an atomic `current` symlink.

**Tech Stack:** Python 3.11+, TOML/JSON, `unittest`, Docker-compatible containers, pinned Debian Bookworm, dosfstools, mtools, e2fsprogs/debugfs, MBR/FAT32.

**Spec:** `docs/superpowers/specs/2026-09-06-native-bootable-media-design.md`

## Global Constraints

- Only profile `native-integration-dev` supports `media` and `verify-media`; historical profiles and `development.json` cannot satisfy the prerequisite.
- The rootfs input is the byte-identical verified `out/native-integration-dev/linux.img`; media assembly never rebuilds the host, FPGA cores, compiler, kernel, or rootfs.
- Sector size is 512 bytes; partition 1 starts at sector 2,048 and has 524,288 sectors, type `0x0c`, active; partition 2 starts at sector 526,336 and has 2,048 sectors, type `0xa2`; total size is 528,384 sectors.
- MBR disk identifier is `0x46455331`; both partition entries use `fe ff ff` for start and end CHS; FAT serial is `0xf35d0001` and label is `FESDATA`.
- Assembly-owned paths are `/menu.rbf`, `/linux/zImage_dtb`, `/linux/linux.img`, `/fogcast/`, plus optional `/fogcast/agent.toml`.
- `uboot.img` is written byte-for-byte at the first byte of partition 2 and its remaining bytes are zero.
- The exact pinned `uboot.img` SHA-256 is `e2d46cf9fe1ec40ca2c9c7409870249f267e06f70e5736dc6d30b4e21fe62a64`, size 515,141; `zImage_dtb` SHA-256 is `a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae`, size 7,380,857.
- Assembly is unprivileged, uses no loop devices, never writes a host block device, runs twice independently, and publishes only identical results.
- Provisioned configuration is snapshotted once, never logged, and all provisioned scratch/directories/files use owner-only permissions.
- Verification may write only inside private disposable scratch; it cannot mutate published generations or hardware.
- A media generation begins with hardware status `not-run`; hardware acceptance is separate evidence tied to the exact image hash.
- All production behavior follows red-green-refactor: the covering test is run and observed failing before its implementation is written.

---

### Task 1: Separate cold-build and media evidence fingerprints

**Files:**
- Modify: `scripts/build.py`
- Modify: `tests/test_receipt.py`
- Modify: `tests/test_native_dev.py`

**Interfaces:**
- Produces: `recipe_fingerprint(paths: tuple[Path, ...]) -> dict[str, str]`
- Produces: `build_fingerprint(revisions: dict, profile: dict, toolchain: str) -> tuple[str, dict]`
- Produces: `load_verified_image(output: Path, fingerprint: str) -> dict` returning the exact rootfs SHA and evidence digests or raising `ValueError`
- Preserves: existing host/image receipt values for unchanged build-relevant inputs except for the deliberate removal of future media-only sources from their recipe set

- [ ] **Step 1: Add failing fingerprint-boundary and cold-evidence tests**

```python
def test_media_sources_do_not_invalidate_cold_build_fingerprint(self):
    before, _ = build.build_fingerprint(REVISIONS, PROFILE, "go test")
    with mock.patch.object(build, "MEDIA_RECIPE_FILES", (Path("changed-media.py"),)):
        after, _ = build.build_fingerprint(REVISIONS, PROFILE, "go test")
    self.assertEqual(before, after)

def test_verified_image_rejects_missing_or_mismatched_evidence(self):
    output = self.make_cold_output()
    (output / "verification.json").unlink()
    with self.assertRaisesRegex(ValueError, "run make verify"):
        build.load_verified_image(output, "cold-fingerprint")
```

- [ ] **Step 2: Run the focused tests and confirm RED**

Run: `python3 -m unittest tests.test_receipt tests.test_native_dev -v`

Expected: FAIL because `build_fingerprint`, `MEDIA_RECIPE_FILES`, and `load_verified_image` do not exist.

- [ ] **Step 3: Implement explicit recipe sets and fail-closed evidence loading**

```python
BUILD_RECIPE_FILES = tuple(sorted((ROOT / "scripts").glob("*.py")))
MEDIA_RECIPE_FILES = ()

def recipe_fingerprint(paths):
    return {str(path.relative_to(ROOT)): digest(path) for path in paths}

def load_verified_image(output, fingerprint):
    if not reusable(output, "image", fingerprint):
        raise ValueError("cold image receipt is missing or stale; run make build and make verify")
    verification = json.loads((output / "verification.json").read_text())
    actual = digest(output / "linux.img")
    evidence = dict(line.split("=", 1) for line in
                    (output / "reproducibility.txt").read_text().splitlines())
    required = (verification.get("image_sha256") == actual
                and verification.get("structural") == "pass"
                and verification.get("qemu_packaging") == "pass"
                and verification.get("two_pass_reproducibility") == "pass"
                and evidence.get("run_1_sha256") == actual
                and evidence.get("run_2_sha256") == actual
                and (output / "qemu-smoke.log").is_file())
    if not required:
        raise ValueError("cold image verification is missing or stale; run make verify")
    return {"rootfs_sha256": actual,
            "image_receipt_sha256": digest(output / "image.json"),
            "verification_sha256": digest(output / "verification.json"),
            "qemu_log_sha256": digest(output / "qemu-smoke.log")}
```

Keep profile fields that affect host/rootfs in `build_fingerprint`; media layout and media recipe files enter only the later media fingerprint.

- [ ] **Step 4: Run focused and full tests**

Run: `python3 -m unittest tests.test_receipt tests.test_native_dev -v`

Run: `python3 -m unittest discover -s tests -v`

Expected: all tests PASS.

- [ ] **Step 5: Commit the evidence boundary**

```bash
git add scripts/build.py tests/test_receipt.py tests/test_native_dev.py
git commit -m "refactor: separate verified media prerequisites"
```

### Task 2: Lock and cache boot payloads

**Files:**
- Create: `boot-media.lock.toml`
- Create: `scripts/media_inputs.py`
- Create: `tests/test_media_inputs.py`

**Interfaces:**
- Produces: `MediaLock.load(path: Path) -> MediaLock`, a closed-schema immutable record
- Produces: `resolve_payloads(root: Path, lock: MediaLock, run: Callable) -> Payloads`
- Produces: `Payloads(uboot: Path, kernel: Path)` whose files have already passed commit, cleanliness, size, and SHA checks

- [ ] **Step 1: Add failing lock-schema and payload-validation tests**

```python
def test_lock_rejects_unknown_fields_and_wrong_geometry(self):
    text = LOCK_TEXT + '\nunknown = true\n'
    with self.assertRaisesRegex(ValueError, "unknown boot-media lock field"):
        MediaLock.loads(text)

def test_cached_payloads_require_exact_revision_and_hash(self):
    cache = self.make_image_creator_cache(commit=IMAGE_CREATOR_COMMIT)
    (cache / "uboot.img").write_bytes(b"changed")
    with self.assertRaisesRegex(ValueError, "uboot.img digest"):
        resolve_payloads(self.root, MediaLock.loads(LOCK_TEXT), self.run)
```

- [ ] **Step 2: Run the new tests and confirm RED**

Run: `python3 -m unittest tests.test_media_inputs -v`

Expected: FAIL because `scripts/media_inputs.py` does not exist.

- [ ] **Step 3: Add the exact closed lock and validator**

The lock contains format `1`, repository URL, image-creator commit `8aba321b2162e54b56522aa30758b22d97eec8da`, the two payload identities from Global Constraints, layout `de10-nano-mister-v1`, exact sector geometry, fixed disk/FAT identifiers, and expected embedded strings `mmcroot=/dev/mmcblk0p1`, `bootimage=/linux/zImage_dtb`, `core=menu.rbf`, and `loop=linux/linux.img ro rootwait`.

```python
@dataclass(frozen=True)
class Payloads:
    uboot: Path
    kernel: Path

def verify_file(path, expected_size, expected_sha, label):
    if not path.is_file() or path.is_symlink():
        raise ValueError(f"{label} is not a regular file")
    if path.stat().st_size != expected_size or digest(path) != expected_sha:
        raise ValueError(f"{label} digest or size differs from boot-media lock")
```

Clone/fetch only into `out/work/boot-media/image-creator-<commit>`, detach at the locked commit, reject any tracked or untracked dirt, and revalidate payloads on every call. Scan the exact `uboot.img` bytes for all locked environment strings before returning.

- [ ] **Step 4: Cover missing-cache population and offline reuse**

```python
def test_missing_cache_fetches_once_then_reuses_offline(self):
    first = resolve_payloads(self.root, self.lock, self.recording_run)
    second = resolve_payloads(self.root, self.lock, self.rejecting_run)
    self.assertEqual(first, second)
```

Run: `python3 -m unittest tests.test_media_inputs -v`

Expected: all tests PASS.

- [ ] **Step 5: Commit locked boot inputs**

```bash
git add boot-media.lock.toml scripts/media_inputs.py tests/test_media_inputs.py
git commit -m "feat: lock boot media payloads"
```

### Task 3: Build and verify deterministic MBR/FAT32 images

**Files:**
- Create: `containers/boot-media/Dockerfile`
- Create: `containers/boot-media/packages.sha256`
- Create: `containers/boot-media/create-builder-user.sh`
- Create: `scripts/media_container.py`
- Create: `scripts/media_inside.py`
- Create: `tests/test_media_image.py`

**Interfaces:**
- Produces: `ensure_media_container(root: Path, runtime: str, lock: MediaLock) -> str` returning an immutable local image ID after label validation
- Produces: container CLI `media_inside.py assemble --rootfs ... --idle ... --kernel ... --uboot ... --output ... [--agent-config ...]`
- Produces: container CLI `media_inside.py verify --image ... --manifest ... --rootfs ... --idle ... --kernel ... --uboot ... [--agent-config-sha256 ...]`
- Produces: deterministic `fes.img` plus closed `fes-media.toml`

- [ ] **Step 1: Add failing production-geometry and corruption tests**

```python
def test_real_sparse_image_has_exact_mbr_fat32_and_payloads(self):
    image, manifest = assemble_fixture(self.root)
    self.assertEqual(image.stat().st_size, 528384 * 512)
    result = verify_image(image, manifest, self.payloads)
    self.assertEqual(result.partition_types, (0x0c, 0xa2))
    self.assertEqual(result.fat_type, "FAT32")
    self.assertEqual(result.paths, ("/fogcast", "/linux", "/linux/linux.img",
                                    "/linux/zImage_dtb", "/menu.rbf"))

def test_verifier_rejects_nonzero_boot_tail(self):
    image, manifest = assemble_fixture(self.root)
    write_byte(image, 526336 * 512 + UBOOT_SIZE + 1, 1)
    with self.assertRaisesRegex(ValueError, "boot partition tail"):
        verify_image(image, manifest, self.payloads)
```

- [ ] **Step 2: Run the focused test and confirm RED**

Run: `python3 -m unittest tests.test_media_image -v`

Expected: FAIL because the container/image interfaces do not exist.

- [ ] **Step 3: Add the pinned unprivileged tool container**

Base it on `docker.io/library/debian:12.11-slim@sha256:b1a741487078b369e78119849663d7f1a5341ef2768798f7b7406c4240f86aef`. Install only `dosfstools`, `e2fsprogs`, `mtools`, `python3`, and the package dependencies, record the sorted dpkg package-set SHA-256 in `packages.sha256`, label the image with base/context/package hashes, create a matching non-root builder user, and validate those labels on every reuse.

```dockerfile
USER builder
WORKDIR /work
ENTRYPOINT ["python3", "/work/scripts/media_inside.py"]
```

- [ ] **Step 4: Implement deterministic assembly**

`assemble` must create a zero-filled 270,532,608-byte regular file, write the exact two MBR entries and signature, create a FAT32 filesystem with fixed serial/label and invariant mode, copy source files whose mtimes were normalized to `SOURCE_DATE_EPOCH=1751459412` in fixed order, and write the locked boot blob at offset `526336 * 512`. It extracts `/usr/share/mister-runtime/idle.rbf` from rootfs using debugfs without mounting it. It emits canonical TOML with sorted closed fields and exact payload/image hashes.

```python
PART1_OFFSET = 2048 * 512
PART1_SIZE = 524288 * 512
BOOT_OFFSET = 526336 * 512
DISK_SIZE = 528384 * 512

def write_region(image, source, offset):
    with image.open("r+b", buffering=0) as target, source.open("rb") as payload:
        target.seek(offset)
        shutil.copyfileobj(payload, target, length=1024 * 1024)
```

Use mtools offset syntax (`image@@1048576`) and never attach a loop device. Run the full assembler twice in separate scratch directories and assert byte-identical SHA-256 output before returning success.

- [ ] **Step 5: Implement a fail-closed independent verifier**

Parse MBR bytes with `struct`, check all unused/padding/tail regions in bounded chunks, ask `fsck.fat -vn` to validate FAT32, list paths with mtools, extract every owned path, and compare each byte/hash with its locked input. Parse the manifest with a closed field set and reject duplicate TOML keys through `tomllib` errors.

```python
def require_zero(stream, start, length, label):
    stream.seek(start)
    remaining = length
    while remaining:
        block = stream.read(min(1024 * 1024, remaining))
        if not block or any(block):
            raise ValueError(f"nonzero or truncated {label}")
        remaining -= len(block)
```

- [ ] **Step 6: Run corruption, determinism, and real FAT32 tests**

Run: `python3 -m unittest tests.test_media_image -v`

Expected: PASS, including two identical real-layout sparse images and rejection of altered MBR, FAT metadata, payload, padding, boot tail, extra owned path, and truncation.

- [ ] **Step 7: Commit image mechanics**

```bash
git add containers/boot-media scripts/media_container.py scripts/media_inside.py tests/test_media_image.py
git commit -m "feat: assemble deterministic boot media"
```

### Task 4: Orchestrate provisioning, receipts, leases, and atomic publication

**Files:**
- Create: `scripts/media.py`
- Create: `tests/test_media.py`
- Modify: `scripts/build.py`
- Modify: `Makefile`

**Interfaces:**
- Produces: `python3 scripts/media.py build --profile native-integration-dev [--agent-config ABSOLUTE]`
- Produces: `python3 scripts/media.py verify --profile native-integration-dev`
- Produces: `out/native-integration-dev/media/generations/<sha256>/{fes.img,fes-media.toml,media.json}` and atomic relative symlink `media/current`
- Produces Make targets: `make media [AGENT_CONFIG=/absolute/path]` and `make verify-media`
- Delegates: the extracted embedded rootfs to the selected FogCast structural verifier and QEMU smoke in its allowed container path

- [ ] **Step 1: Add failing prerequisite, secret, publication, and CLI tests**

```python
def test_media_rejects_development_or_unverified_cold_output(self):
    with self.assertRaisesRegex(ValueError, "run make build and make verify"):
        media.build(self.root, "native-integration-dev", None, self.runner)

def test_provisioning_snapshots_secret_once_and_never_logs_it(self):
    config = self.write_config(b"agent_token = 'do-not-print'\n", mode=0o600)
    result = media.build(self.root, "native-integration-dev", config, self.runner)
    self.assertNotIn("do-not-print", result.log + result.manifest_text)
    self.assertEqual(stat.S_IMODE(result.image.stat().st_mode), 0o600)

def test_failed_republish_preserves_current_generation(self):
    first = media.build(self.root, "native-integration-dev", None, self.runner)
    with self.assertRaisesRegex(ValueError, "verification failed"):
        media.build(self.root, "native-integration-dev", None, self.failing_runner)
    self.assertEqual((self.media_root / "current").resolve(), first.generation)
```

- [ ] **Step 2: Run the focused tests and confirm RED**

Run: `python3 -m unittest tests.test_media -v`

Expected: FAIL because `scripts/media.py` and Make targets do not exist.

- [ ] **Step 3: Implement prerequisite/media fingerprints and provisioning**

Call Task 1's `load_verified_image`, Task 2's `resolve_payloads`, and Task 3's container APIs. Reject non-absolute, missing, symlink, non-regular, group/world-readable, or larger-than-64-KiB configs. Open once with `O_NOFOLLOW`, validate the opened inode against the lstat identity, read at most 65,537 bytes, and write the snapshot into a `0700` scratch directory as `0600`. Never include its contents in an exception or command line.

```python
def media_fingerprint(cold, lock, provision_sha):
    data = {"cold": cold, "boot_lock": digest(lock),
            "recipe": recipe_fingerprint(MEDIA_RECIPE_FILES),
            "provision_sha256": provision_sha}
    return hashlib.sha256(json.dumps(data, sort_keys=True).encode()).hexdigest(), data
```

During verification, extract the embedded `linux.img` into a private path that
is mounted as `/work/build/output/target-image/media-verify/linux.img` for the
selected FogCast verifier. Confirm byte identity with the parent rootfs, invoke
`verify-target-image.sh` and `qemu-smoke-target-image.sh`, and require the
already built VExpress kernel cache to match the pinned source key. Fail with an
instruction to run `make verify` if the cache is absent; do not build it here.

- [ ] **Step 4: Implement lease and atomic content-addressed publication**

Hold a nonblocking profile-scoped `flock` at `out/locks/media-native-integration-dev.lock`, and write owner/PID/host/start/expiry metadata beside it without truncating another live owner's record. Stage under a private `media/.staging-*` directory. After both assembly passes and verification succeed, rename staging to `generations/<image-sha256>` (or verify an existing identical generation), create a relative temporary symlink, fsync the directory, and use `os.replace` to publish `current`. Resolve `current` exactly once during verify.

```python
temporary_link = media_root / ".current.tmp"
temporary_link.symlink_to(Path("generations") / image_sha)
os.replace(temporary_link, media_root / "current")
```

Write `media.json` last. It binds the media fingerprint, input evidence digests, manifest digest, image digest, both assembly hashes, structural-media/rootfs-structural/rootfs-QEMU/reproducibility statuses, and `hardware = "not-run"`.

- [ ] **Step 5: Wire CLI and Make targets**

```make
media:
	$(PYTHON) scripts/media.py build --profile "$(PROFILE)" $(if $(AGENT_CONFIG),--agent-config "$(AGENT_CONFIG)")
verify-media:
	$(PYTHON) scripts/media.py verify --profile "$(PROFILE)"
```

Update `help` and reject `AGENT_CONFIG` for commands other than `media` through the target interface.

- [ ] **Step 6: Run focused and full tests**

Run: `python3 -m unittest tests.test_media -v`

Run: `python3 -m unittest discover -s tests -v`

Expected: all tests PASS, including republish, failure rollback, concurrent lease rejection, stale receipt rejection, provision permissions, symlink rejection, no secret leakage, and receipt reuse.

- [ ] **Step 7: Commit orchestration**

```bash
git add scripts/media.py scripts/build.py Makefile tests/test_media.py
git commit -m "feat: publish verified boot media generations"
```

### Task 5: Document and validate the operator workflow

**Files:**
- Modify: `README.md`
- Modify: `AGENTS.md`
- Modify: `docs/README.md`
- Modify: `docs/getting-started.md`
- Modify: `docs/development.md`
- Create: `docs/bootable-media.md`
- Modify: `tests/test_consistency.py`

**Interfaces:**
- Documents: artifact boundary, commands, provisioning, safe flashing boundary, verification meanings, generation rollback, and hardware-acceptance checklist
- Preserves: no generic block-device writer in `make media` or `make verify-media`

- [ ] **Step 1: Add a failing documentation contract test**

```python
def test_bootable_media_docs_and_agent_entrypoints_are_linked(self):
    required = {
        "README.md": "docs/bootable-media.md",
        "AGENTS.md": "make verify-media",
        "docs/README.md": "bootable-media.md",
        "docs/getting-started.md": "make media",
        "docs/development.md": "media/current/fes.img",
    }
    for name, needle in required.items():
        self.assertIn(needle, (ROOT / name).read_text(), name)
```

- [ ] **Step 2: Run the contract test and confirm RED**

Run: `python3 -m unittest tests.test_consistency.ConsistencyTest.test_bootable_media_docs_and_agent_entrypoints_are_linked -v`

Expected: FAIL because no bootable-media guide or links exist.

- [ ] **Step 3: Write the operator and agent documentation**

`docs/bootable-media.md` must show:

```text
make build
make verify
make media
make verify-media
make media AGENT_CONFIG=/absolute/private/path/agent.toml
```

Explain that `media/current/fes.img` is flashable, `linux.img` alone is not; the default image reaches runtime idle but has no agent credentials; provisioning is local and sensitive; generation directories are immutable rollback points; verification covers MBR/FAT/payload/rootfs/QEMU but hardware remains `not-run`; physical-card writing requires an exact separately authorized device and the kit lease; first cold-boot acceptance covers writable `/media/fat`, read-only loop root, Pong/Mega Drive/SNES input/audio/Stop, SNES save across reboot, no Main process, new boot ID, and lease release.

- [ ] **Step 4: Run documentation and repository checks**

Run: `python3 -m unittest discover -s tests -v`

Run: `make check`

Run: `git diff --check`

Expected: all checks PASS.

- [ ] **Step 5: Exercise the real already-verified cold output**

Run from the feature worktree after copying or regenerating the current verified cold receipts there:

```bash
make media
make verify-media
sha256sum out/native-integration-dev/media/current/fes.img
cat out/native-integration-dev/media/current/media.json
```

Expected: two assembly hashes match; all host verification statuses pass; hardware remains `not-run`; a second `make media` reuses or republishes the same content-addressed generation without changing its hash.

- [ ] **Step 6: Commit documentation and acceptance evidence**

```bash
git add README.md AGENTS.md docs tests/test_consistency.py
git commit -m "docs: explain native bootable media workflow"
```

- [ ] **Step 7: Run final branch verification**

Run: `python3 -m unittest discover -s tests -v`

Run: `make check`

Run: `make verify-media`

Expected: all tests/checks PASS and the exact published image hash is recorded for later hardware acceptance.
