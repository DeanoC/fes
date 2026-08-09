# Stage A0 first Main build: Software-tested evidence

## Classification

**Status:** Software-tested.

This report records the first engineering build of the local Main fork and a
second clean build with matching final bytes. It is not the canonical Stage A0
report and does not claim durable source retrieval, Reproducible status, FPGA
launch, target behavior, or HIL evidence.

## Locked identities observed for this run

| Input | Observed identity |
| --- | --- |
| Main fork commit | `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d` |
| Main fork tree | `efb9c24e8e27945a75d8c497b4b99ec249129075` |
| Main source inventory | 420 tracked paths; 113 direct Makefile source/image inputs |
| Toolchain archive | `gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf.tar.xz` |
| Toolchain archive size | `104607124` bytes |
| Toolchain archive SHA-256 | `102825ae56c9e00142d06f35d2bdd3299edb6060e84a275a25b095e66fd3fc2a` |
| Toolchain archive MD5 (Arm `.asc`) | `14f706db78cfb43aafed9056174572b0` |
| Toolchain target | `arm-none-linux-gnueabihf` |
| Toolchain compiler | GNU Toolchain for the A-profile Architecture `10.2-2020.11 (arm-10.16)`, GCC `10.2.1 20201103` |
| Container base manifest | Debian `12.11-slim`, reference `sha256:b1a741487078b369e78119849663d7f1a5341ef2768798f7b7406c4240f86aef`; runs selected `linux/amd64` explicitly |
| Prepared local build image (run 3) | `stage-a0-firstbuild:debian12-arm102-v1`, `linux/amd64`, image ID `sha256:24045e0e800b0ce7df88076ccab628387b149f1bd0786fab46fffae07a859d0c` |
| Source date epoch | `1786215171` (UTC `2026-08-08`; Main `VDATE=260808`) |

The archive was retrieved from Arm's official GNU-A release locator:
`https://developer.arm.com/-/media/Files/downloads/gnu-a/10.2-2020.11/binrel/gcc-arm-10.2-2020.11-x86_64-arm-none-linux-gnueabihf.tar.xz`.
The official release page identifies this package as the x86_64 Linux hosted
AArch32 hard-float target and publishes the corresponding `.asc` checksum.

## Build procedure

Runs 1 and 2 used fresh disposable `linux/amd64` containers from the pinned
base, installed the build utilities, extracted the byte-verified Arm archive,
and executed the fork's unmodified build graph with the historical command
below. Run 3 used a locally prepared copy of that environment with networking
disabled; the preparation itself is not yet a durable package or lock input.
The final capture command is shown separately because it enables verbose
command observation and runs from the inspected immutable image ID:

```text
make clean VDATE=260808
make VDATE=260808
```

The network-off capture uses the same clean step followed by:

```text
make V=1 VDATE=260808
```

The build ran with the cross-toolchain first on `PATH`; its normal Makefile
source selection, language modes, optimization, link flags, library order, and
output names were retained. The second and third runs used separate clean
`bin/` output trees. The canonical fork worktree remained tracked-clean after
all runs. Run 3 also demonstrated that the build succeeds with `--network
none` once the environment is prepared, but the image's package provenance
still requires a future final-lock record.

## Result

| Final artifact | Size | SHA-256 | Two-run comparison |
| --- | ---: | --- | --- |
| `bin/MiSTer` (stripped) | `1157996` | `f9e6fd646740449186b74821a3684686d5dbc7b33e28052e9de28b8f4c751f2e` | byte-identical |
| `bin/MiSTer.elf` (unstripped) | `1380136` | `51a9864bb12ebdf8961b30ac0a45d533fd2a2885a96d81fc29f8363d1804706d` | byte-identical |

The run-2 and run-3 build logs and byte copies are retained under the ignored
`artifacts/stage-a0/first-build/` directory for local inspection. They are
not source-controlled evidence and are not the final manifest package.

## Remaining gates

This successful engineering build does not close Stage A0. The following are
still required before a lock can be promoted or a Reproducible status claimed:

- a reviewed real final lock with every consumed material, license, source,
  sysroot, container, utility, configuration, and policy record;
- durable retrieval and cache revalidation for the fork/toolchain/container and
  all bundled/prebuilt Main inputs;
- isolated fetch/build adapters with network-off enforcement and source-set,
  compile, ELF, dependency, generated-input, and intermediate manifests; and
- the canonical two-report comparator and independent lock/evidence review.

No Overlord output was consumed by this build, and no target, credential,
hardware, FPGA, display, audio, input, or lifecycle operation was performed.
