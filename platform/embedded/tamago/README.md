# Native Go embedded base (`tamago/`)

This module (`github.com/DeanoC/fes/platform/embedded/tamago`) contains the
bare-metal AArch64 TamaGo runtime, board BSPs (`qemuvirt` and `de25nano`),
device drivers, Tenfoot launcher wiring, and QEMU regression harnesses.

- **System architecture, boot chain & delegation diagrams:**
  [../ARCHITECTURE.md](../ARCHITECTURE.md)
- **DE25-Nano SD launcher workflow:** [../nano/launcher.md](../nano/launcher.md)
- **Board selection & schema-8 handoff:** [board/README.md](board/README.md)

Set `FES_EXPERIMENTAL_DE25_EMBEDDED=1` before running `run.py` or
`make embedded`. Host `go test` runs need no opt-in and touch no hardware.

## Toolchain setup

TamaGo and its modified Go compiler are pinned to **1.26.5** in
[toolchain.lock.toml](toolchain.lock.toml) (stock Go cannot build
`GOOS=tamago`). Extract the verified archive under
`out/embedded-base-tools/tamago`, download modules, and build the host fixture:

```sh
cd platform/embedded/tamago
GOTOOLCHAIN=local \
GOMODCACHE="$PWD/../../../out/embedded-base-tools/tamago/mod" \
../../../out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go mod download
cd ../../..
(cd sources/FogCast && GOWORK=off go build \
  -o ../../out/embedded-base-tools/fogcast-host ./cmd/fogcast-embedded-host)
```

`run.py` requires Python 3, `qemu-system-aarch64` with `ramfb`, and `mtools`.
It writes only under a fresh `out/` directory and builds with `GOPROXY=off`,
`GOSUMDB=off`, `GOTOOLCHAIN=local`, and `-mod=readonly`.

## Entrypoints and suites (`run.py`)

| Target | Board(s) | What it exercises |
| --- | --- | --- |
| `probe` | `qemu`, `de25-nano` | Minimal EL1 CPU, timer, and GC sanity check |
| `mapcheck` | `qemu` | EL1 page-table / `arm64map` verification |
| `fabric` | `qemu` | `fabricmodel` + `sdm.ConfigModel` lifecycle checks |
| `keyboard` | `qemu` | xHCI + shared HID keyboard decoder |
| `display` | `qemu` | `ramfb` + `gfx.Software` framebuffer output |
| `storage` | `qemu`, `de25-nano` | FAT32 mount, CoW `ReplaceFile`, and owned SD storage |
| `packages` | `qemu` | Format-2/3/4 archive validation (`corepackage.RetainArchive`) on FAT32 |
| `admission` | `qemu` | `runtimeadmission` Go parity against host C++14 `libmister-runtime` oracle + ROM linking |
| `network` (`--dhcp`) | `qemu` | VirtIO-net + `lneto` static/DHCPv4/DNSv4/SNTP + HTTP catalog |
| `tenfoot` | `qemu` | Two-boot Tenfoot UI + `ramfb` + USB keyboard + FAT32 prefs + FogCast host |
| `play` | `qemu` | Tenfoot Play/Return over `modelbackend` (ROMs, parts, errors, saves) |
| `programfetch` | `qemu` | `nativefabric.ProgramSource.Fetch` over VirtIO/`lneto` HTTP |
| `fpgaload` | `de25-nano` | Full DE25-Nano Tenfoot launcher image (paired MENU, USB, FAT32, EL3 SMCs) |

### Example: Tenfoot two-boot QEMU regression

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py tenfoot \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 \
  --output out/native-go-system
```

Add `--interactive --display sdl --disk out/my-boot.img` for interactive use
(`--input uart` selects serial input instead of USB).

### Example: Runtime admission & host `-race` oracle parity

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py admission \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 \
  --output out/native-runtime-admission

FES_ADMISSION_FIXTURES="$PWD/out/native-runtime-admission/package-fixtures" \
  GOWORK=off go -C platform/embedded/tamago test -race \
  ./runtimeadmission ./admissioncheck ./fabricmodel
```

`admission` validates format-2/3/4 packages, links format-3/4 synthetic Mistral
RBFs via `Archive.LinkROMs` (with content-bound `LinkedArchive` receipts), and
checks pure-Go [runtimeadmission](runtimeadmission/README.md) against the C++14
oracle ([../runtime/admission_oracle.cpp](../runtime/admission_oracle.cpp)).

## Tenfoot Play and return through the model

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/tamago/run.py play \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu /path/to/qemu-system-aarch64 \
  --output out/native-tenfoot-play
```

Drives Play and Back in `cmd/tenfoot` over QEMU USB keyboard against
[modelbackend](modelbackend/README.md) (normal launch/return, wrong ROM/receipt,
canceled preparation, identity recovery, failed save retry, and CPU/video
`PartInput` composition via `Archive.LinkROMs`) without physical FPGA output.

## Runtime contract and CI

- **Cooperative execution + GICv3 IRQs:** Single-core cooperative Go with
  bounded driver deadlines (renderer yields every 4 rows / 32 KiB copies).
  `cmd/tenfoot` and DE25-Nano `cmd/fpgaload` enable [gic](gic/README.md) GICv3
  + `WFI` idle (PPI 30) and device SPI wakeups.
- **DMA & storage:** DMA buffers come from uncached [dmaops](dmaops/README.md)
  or boot-pinned arenas (never the Go heap). [block](block/README.md) provides
  a lease-invalidating LRU write-through cache; [fat](fat/README.md) coalesces
  up to 64 KiB and updates `FES/PREFS.JSON` via copy-on-write `ReplaceFile`.
- **CI lane:** `scripts/ci_tamago.sh` and `scripts/tamago_ci.py` run host
  `go vet` + `-race` tests, dual-board cross-builds (`qemu` and `de25-nano`),
  and all 11 QEMU suites above (including static and `--dhcp` network runs).

## Package index

| Area | Packages |
| --- | --- |
| Nano orchestrator & FPGA | [nativefabric](nativefabric/README.md), [computer](computer/README.md), [scanout](scanout/README.md), [sdm](sdm/README.md), [fpgairq](fpgairq/README.md) |
| QEMU / host software model | [modelbackend](modelbackend/README.md), [fabricmodel](fabricmodel/README.md), [runtimeadmission](runtimeadmission/README.md) |
| Storage & packages | [packagestore](packagestore/README.md), [storagepolicy](storagepolicy/README.md), [fat](fat/README.md), [fatcheck](fatcheck/README.md), [block](block/README.md), [sdcontrol](sdcontrol/README.md), [sdhci](sdhci/README.md) |
| Input & USB | [input](input/README.md), [usb](usb/README.md), [usbcontrol](usbcontrol/README.md), [dwc2](dwc2/README.md), [cmd/usbcheck](cmd/usbcheck/README.md) |
| Networking | [network](network/README.md), [xgmac](xgmac/README.md), [maccontrol](maccontrol/README.md) |
| Board & core services | [board](board/README.md), [board/arm64map](board/arm64map/README.md), [gic](gic/README.md), [dmaops](dmaops/README.md), [display](display/README.md), [psci](psci/README.md), [rtc](rtc/README.md), [wdt](wdt/README.md) |
