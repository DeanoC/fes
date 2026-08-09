# Stage A0 first-build evidence capture

## Status and authority

**Status:** Design for the first software-tested implementation, dated
2026-08-09.

This is a small implementation slice below the approved [Stage A0 reproducible
Main baseline design](2026-08-08-stage-a0-reproducible-main-baseline-design.md).
It does not replace the Stage A0 final lock, build adapter, clean-build
comparison, or canonical report. It records one observed Linux/amd64 Docker
build of the local GPLv3 `Main_MiSTer` fork and is useful only as preliminary
**Software-tested** evidence. It cannot produce **Reproducible**,
**HIL-observed**, or **Accepted** evidence.

The implementation owns only a Go package and a command-line capture tool. It
does not modify the Main fork, the final-lock parser, the target, or the public
host/target protocol.

## Problem

The first real build has now been observed, but the observation is currently a
set of shell commands and a local output tree. A later worker must be able to
repeat the build and prove that the inputs and output shape are still the ones
reviewed here without accepting caller-supplied claims. The capture tool must
execute the Git, archive, Docker, toolchain, and build commands itself; derive
all identities from those observations; and stop before emitting evidence when
any locked fact has drifted.

## Decision

Add `internal/stagea0/firstbuild` and `cmd/stage-a0-firstbuild`.

The command accepts physical locations only as operational inputs:

```text
stage-a0-firstbuild \
  --source <local Main_MiSTer checkout> \
  --toolchain-archive <gcc-arm-10.2-2020.11 archive> \
  --output <new evidence output directory>
```

The image reference and Docker executable are fixed to the reviewed local
values; the command does not accept a caller-selected Docker context or
executable. The tool never trusts a caller-provided commit, tree, archive
hash, compiler version, image digest, VDATE, artifact hash, or output
inventory. It observes each value, compares it with the locked facts below,
and fails closed on a mismatch.

### Locked facts from the first real build

The initial implementation uses the exact facts already observed on the local
development host:

| Input | Observed value |
| --- | --- |
| Main branch | `fogcast/stage-a-baseline` |
| Main fork commit | `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d` |
| Main fork tree | `efb9c24e8e27945a75d8c497b4b99ec249129075` |
| Direct parent | `7b5c8de5d3fb16f9cccc1f274a2ff1b481637e42` |
| Main source files | `420` tracked files |
| Source patch | `stage-a0: make VDATE reproducible` |
| `VDATE` / locked epoch | `260808` / `1786215171` UTC |
| Toolchain archive size | `104607124` bytes |
| Toolchain archive SHA-256 | `102825ae56c9e00142d06f35d2bdd3299edb6060e84a275a25b095e66fd3fc2a` |
| Compiler | `arm-none-linux-gnueabihf-gcc`, GNU A-profile 10.2.1 (20201103) |
| Container | local prepared `stage-a0-firstbuild:debian12-arm102-v1`, Linux/amd64 |
| Container image ID | `sha256:24045e0e800b0ce7df88076ccab628387b149f1bd0786fab46fffae07a859d0c` |
| Final stripped artifact | `bin/MiSTer`, 1157996 bytes, SHA-256 `f9e6fd646740449186b74821a3684686d5dbc7b33e28052e9de28b8f4c751f2e` |
| Final unstripped artifact | `bin/MiSTer.elf`, 1380136 bytes, SHA-256 `51a9864bb12ebdf8961b30ac0a45d533fd2a2885a96d81fc29f8363d1804706d` |

The complete `bin/` inventory currently contains 227 regular files. The
implementation records and hashes the full sorted inventory, not just the two
final binaries. The inventory hash is derived by the implementation from
relative path, mode, size, and file SHA-256; the observed baseline digest is
`7a2e77ffa919e504a4baa638d6b820085a4464a15281449f1abc2f4d66419cd5`.

### Execution and observation

1. Verify the source checkout is a clean Git worktree on the locked branch and
   that `HEAD`, its tree, its direct parent, tracked-file count, and patch
   subject match the table above.
2. Hash and size-check the archive, inspect its members for absolute or
   traversal paths, extract it into a fresh temporary directory, and verify the
   expected archive root and compiler executable.
3. Inspect the prepared Docker image and require the locked local image ID,
   Linux operating system, and amd64 architecture. Run that immutable image
   ID with `--pull=never`, `--network none`, root user, and only the temporary
   source/toolchain mounts.
   The two earlier Debian-base runs installed utilities over the network; their
   matching outputs are retained as preliminary history, while this prepared
   image is the first network-off capture input. The image remains local-only
   and is not a durable final Stage A0 lock.
4. Materialize an exact `git archive <observed commit>` source tree and confirm
   that the source checkout identity did not change during materialization. The
   container executes the observed compiler-version command and `make V=1
   VDATE=260808`; no build fact is accepted from flags or environment supplied
   by the caller.
5. Observe the output tree, reject symlinks/special files and unexpected
   locations, hash every regular file, validate both final ELF artifacts, and
   publish only the evidence payload into a new output directory by atomic
   sibling-directory rename.
6. Emit the deterministic JSON report only after all checks pass. A raw build
   log may remain in the local output directory for debugging, but it is
   run-only material and is not part of the deterministic report.

The command uses direct argument vectors, not shell interpolation, for host
commands. The one container shell fragment is constant except for the locked
`VDATE`; source, toolchain, and output paths never enter the report. The
network mode is recorded as an observed `none` input in this preliminary
schema, rather than an implicit permission.

## Deterministic report contract

The report schema is `fogcast.stage-a0.first-build.v1`. It contains fixed-order
struct fields for schema/version, evidence status, source identity, toolchain
identity, container identity, the logical build recipe, and a sorted output
inventory. It explicitly sets:

```json
{
  "status": "Software-tested",
  "canonical_stage_a0_report": false,
  "two_builds_byte_identical": false
}
```

Paths are logical relative paths such as `bin/MiSTer`; physical source,
toolchain, temporary, output, home, Docker socket, host, and target paths are
never serialized. Wall-clock timestamps, process IDs, hostnames, scheduler
ordering, credentials, and raw log text are never serialized. JSON is encoded
from structs with a fixed field order, LF termination, and no self-hash. The
inventory is sorted lexically before serialization and its digest is computed
from the same canonical records. Re-encoding the same observations therefore
produces byte-identical JSON.

The report is an observation receipt, not a final Stage A0 lock. It does not
claim durable image provenance, a second independent build, binary
reproducibility, source availability beyond the local checkout,
compiler/dependency closure completeness, image equivalence, or any physical
FPGA, HDMI, audio, input, save, recovery, or latency behavior.

## Fail-closed rules and stable errors

The capture stops without a report when any of these differ from the locked
facts or safe shape:

- source branch, commit, tree, parent, patch subject, clean status, or tracked
  file count;
- archive size, SHA-256, root, traversal safety, compiler name/version, or
  extracted contents;
- Docker image ID, operating system, architecture, observed network policy, or
  build command;
- missing/extra inventory files, symlinks/special files, final artifact mode,
  size, ELF identity, SHA-256, or complete inventory digest; or
- an existing output path (including a symlink) or an inability to hash,
  stage, and publish the payload atomically.

The package returns stable codes (`FIRSTBUILD_SOURCE_DRIFT`,
`FIRSTBUILD_TOOLCHAIN_DRIFT`, `FIRSTBUILD_CONTAINER_DRIFT`,
`FIRSTBUILD_OUTPUT_INVALID`, `FIRSTBUILD_BUILD_FAILED`, and
`FIRSTBUILD_SCHEMA_INVALID`) with concise diagnostics that do not include
physical paths or command output. Tests exercise each drift class against
synthetic observations before a real build is run.

## Verification and promotion boundary

Host-only tests cover deterministic encoding, path/timestamp exclusion,
inventory sorting/digests, and each fail-closed validator. The first real
capture is run in the pinned Docker image using the measured archive and local
fork. Its result is labelled **Software-tested** and reviewed by Vega. A
future Stage A0 implementation must independently construct two fresh output
trees, compare complete manifests and both final artifacts byte-for-byte, and
produce the canonical lock/report package before any **Reproducible** claim.

The two measured preliminary runs did produce byte-identical `MiSTer` and
`MiSTer.elf` outputs, but they are not a reproducibility claim because their
utility installation used network access. That observation is retained only
as the locked preliminary output baseline above.
