# Native Go embedded base

This module (`github.com/DeanoC/fes/platform/embedded/tamago`) builds the
Linux-free AArch64 TamaGo runtime, board drivers, Tenfoot launcher and QEMU
off-board regression harnesses. Set `FES_EXPERIMENTAL_DE25_EMBEDDED=1` before
invoking `run.py` or `make embedded`.

## Build and run

TamaGo and its modified Go compiler are pinned to **1.26.5** in
[toolchain.lock.toml](toolchain.lock.toml); stock Go cannot build `GOOS=tamago`.
Extract the verified compiler archive under `out/embedded-base-tools/tamago`
and populate the module cache and host fixture binary:

```sh
cd platform/embedded/tamago
GOTOOLCHAIN=local \
GOMODCACHE="$PWD/../../../out/embedded-base-tools/tamago/mod" \
../../../out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go mod download
cd ../../..
(cd sources/FogCast && GOWORK=off go build \
  -o ../../out/embedded-base-tools/fogcast-host ./cmd/fogcast-embedded-host)
```

`run.py` requires Python 3, QEMU AArch64 with `ramfb`, and `mtools`. It writes
only ordinary files under a fresh directory beneath `out/`, and compiles with
`GOPROXY=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local` and `-mod=readonly`.

### Tenfoot two-boot QEMU regression

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py tenfoot \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 \
  --output out/native-go-system
```

Boots twice on the same FAT32 image with QEMU `ramfb`, USB boot keyboard
(`--input uart` selects serial), VirtIO FAT32 and VirtIO TCP/IP to a private
FogCast host, verifying catalog browsing, layout change persistence and
screenshot/hash receipts. Add `--interactive --display sdl` and `--disk
out/my-boot.img` for manual use. `run.py probe` runs the minimal CPU/timer/GC
sanity check without storage or networking.

### Native Nano build (`fpgaload`)

`run.py fpgaload --board de25-nano` builds the DE25-Nano Tenfoot launcher image
with paired menu, shared USB keyboard, owned FAT32 installer and SD preference
checkpoints. See [../nano/launcher.md](../nano/launcher.md) for the complete
matched-chain build and SD image assembly steps.

### Content fetch over VirtIO TCP (`programfetch`)

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py programfetch \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 \
  --export-root /path/to/nano-export --program-id zx81-rom \
  --output out/native-program-fetch
```

Exercises `nativefabric.ProgramSource.Fetch` over QEMU VirtIO/`lneto` HTTP
against a private host export while leaving the FAT image unchanged. Nano
hardware networking remains disabled in the board profile.

## Platform contract

- **QEMU `virt` memory map:** Enters at EL1 linked at `0x40020000` (256 MiB Go
  RAM at `0x40000000`, 64 MiB noncacheable DMA arena at `0x50000000`, 8 MiB
  framebuffer arena). DMA buffers stay pinned until completion or reboot.
- **Cooperative single-core execution:** Interrupts stay masked; drivers poll
  with explicit deadlines and yield cooperatively. The Tenfoot software
  renderer yields every four rows and splits large copies into 32 KiB chunks.
- **No QEMU constants on DE25:** QEMU PCI/MMIO addresses are test-only. Nano
  memory maps, EL1 page tables, clocks and drivers live under
  [board](board/README.md),
  [board/arm64map](board/arm64map/README.md), [dwc2](dwc2/README.md),
  [sdhci](sdhci/README.md), [sdcontrol](sdcontrol/README.md),
  [scanout](scanout/README.md) and [xgmac](xgmac/README.md).
- **FAT32 & preferences:** [fat](fat/README.md) updates preallocated files and
  creates new files/directories without rename or power-loss atomicity.
  TamaGo's default `os` filesystem is volatile RAM; only `PreferenceStore` /
  `sd-checkpoint` writes FAT32.
- **Subsystem READMEs:** [packagestore](packagestore/README.md),
  [storagepolicy](storagepolicy/README.md), [usb](usb/README.md),
  [usbcontrol](usbcontrol/README.md), [network](network/README.md),
  [computer](computer/README.md), [sdm](sdm/README.md),
  [fabricmodel](fabricmodel/README.md) and
  [nativefabric](nativefabric/README.md).

## Package inspection without a host OS

`run.py packages` validates the shared format-2/3/4 conformance corpus from a
fresh FAT32 image via `corepackage.RetainArchive` (same validator as FogCast),
verifies source-mutation isolation of retained RAM bytes, and checks unchanged
disk hashes.

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py packages \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 \
  --output out/native-package-inspection
```

## Runtime admission of verified packages

`run.py admission` connects retained archives to the software fabric lifecycle
and checks [runtimeadmission](runtimeadmission/README.md) (pure Go
compatibility checker) against the host C++14 oracle
([../runtime/admission_oracle.cpp](../runtime/admission_oracle.cpp)) built from
`libmister-runtime`'s `OpenCorePackage` and `CheckCoreCompatibility`.

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py admission \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 \
  --output out/native-runtime-admission
```

The same fixture corpus can run on the host under `-race`:

```sh
FES_ADMISSION_FIXTURES="$PWD/out/native-runtime-admission/package-fixtures" \
  GOWORK=off go -C platform/embedded/tamago test -race \
  ./runtimeadmission ./admissioncheck ./fabricmodel
```

This tests contract parity in the `qemu-model-only` scope; it grants no
physical FPGA programming authority.

## Retained ROM linking and activation receipts

`run.py admission` also links format-3 firmware/cartridge and format-4
BIOS+cartridge synthetic Mistral RBF vectors in RAM via `Archive.LinkROMs` and
activates them through `LoadLinkedModel` with an opaque content-bound
`LinkedArchive` receipt (binding source digests/roles, sealed map, package and
programmed digest). Source-file mutations on FAT32 after retention must reject
under the original receipt.

## Tenfoot Play and return through the model

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py play \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 \
  --output out/native-tenfoot-play
```

Drives Play and Back in `cmd/tenfoot` over QEMU USB keyboard against the
[modelbackend](modelbackend/README.md) software lifecycle (normal launch/return,
wrong ROM/receipt, canceled preparation, identity recovery, failed save retry
and CPU/video parts cases) without physical FPGA output.

## CPU/video parts in retained activation

`Archive.LinkROMs(ctx, sources, parts...)` accepts `PartInput` archives,
records `PartsComposition` and the pre-ROM overlay digest in the activation
receipt, and is exercised by `run.py play`. For the Nano held-ROM profile,
`fpgaload --selected-programs` requires `--firmware-rom IMAGE --rom-host-go
HOST_GO` to derive content-bound ROM receipts and export `FIRMWARE.BIN`,
`rom-selection.json`, `PROGRAM.JSN` and `CORESEL.JSN`.

## Persistent native window requests

The legacy fixed-window service flags (`fpgaload --window-memory --window-read
--window-present --window-storage-return --window-service`) remain distinct
from the paired Tenfoot launcher (`--menu-project --selected-programs
--tenfoot-serial`) documented in [../nano/launcher.md](../nano/launcher.md).
