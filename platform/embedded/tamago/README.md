# Native Go embedded base

This exploratory image runs the existing tenfoot application directly on
QEMU's single-core AArch64 `virt` machine through
[TamaGo](https://github.com/usbarmory/tamago). There is no guest Linux, libc,
WebAssembly interpreter or dynamically loaded executable. It is an emulator
integration prototype, not a flashable DE25-Nano image.

The `tenfoot` program mounts a real FAT32 disk through VirtIO, reads its API
configuration and preferences, connects to a private FogCast host through a
real VirtIO NIC and TCP stack, and draws the existing software renderer into
QEMU ramfb at 1280×720. A real xHCI USB boot keyboard supplies live navigation
and settings commands; PL011 remains an explicit diagnostic input option.
The host runs the normal scanner/catalog/API against a private library. Boot
configuration supplies the gateway URL and loopback Host header expected by
the existing private API policy. The runner's sample files are synthetic browsing
data, not
playable ROMs. Physical core launch and FPGA programming are unavailable.

The separate `probe` program checks time, goroutine scheduling, allocation/GC
and TamaGo's volatile in-memory filesystem.
The QEMU-only `mapcheck` program checks live EL1 CPU state, page tables and
hardware translations for its heap, UART and uncached DMA, then checks an
unaligned DMA copy. It uses the same [mapping implementation](board/arm64map/README.md)
as the Nano transport; its success is emulator evidence.

Board selection is now explicit: `--board qemu` (default) runs the emulated
hardware drivers; `--board de25-nano --profile FILE` cross-builds the separate
Nano UART/timer, cold read-only SDHCI and direct DWC2 keyboard paths. Nano also has an XGMAC static-IPv4 prototype with explicit DMA/PHY firmware
requirements. Optional display copies into a boot-firmware-owned window; it
requires a matching Nano scanout artifact and provides no physical drain or FPGA
activation support. See the
[board selection and firmware contract](board/README.md) for exact scope and
bring-up commands. The bounded [Nano boot and read-only SD diagnostics](../nano/README.md)
now pass; other physical device and core-launch qualification remains open.
The explicit `fpgaload --window-memory` mode accepts only the sealed
`hps-ddr-window` personality and its complete 4 MiB reservation. It links the
existing tenfoot renderer, publishes one XRGB frame under closed ownership,
then configures and checks the matched endpoint while keeping FPGA data access
held. Native entry and cold firmware carry the same opt-in and map. This mode
cannot use `--ddr-read` or the computer-control modes, and grants no display,
buffer reuse or ordinary runtime admission.

Adding `--window-read` binds the sealed full-frame access contract into Go,
native entry and secure firmware. It reads the published frame through the
existing native CSR driver and returns through firmware's independent
all-master stop and DDR drain. Both the full memory window and its contents
remain pinned until reboot, including on failure. Software and RTL model
checks do not establish physical display or normal core-load acceptance.

Adding `--window-present` selects the owned presenter in the native command.
It renders twelve changing tenfoot states and waits for the matching full-frame
CRC and local reader drain before updating the same pinned arena. Each frame
records its generation, publication hash, read checksum and drain cookie.
The final return uses secure firmware's independent master stop and bridge
closure; its cached reader acknowledgement must match frame twelve. Build,
entry and cold-firmware receipts carry this separate selection. Failure logs
use cached observations and keep memory pinned. Native HDMI and ordinary
runtime admission still need separate implementation and hardware acceptance.
The [matched repeated-frame diagnostic](../../../docs/validation/2026-11-04-de25-buffered-window/README.md)
now passes all twelve frames, local reader drains and final firmware closure
on TRWU8CBL. Its private 32 KiB hash scratch retains complete byte coverage;
all device bounds remain unchanged. This result covers the fixed reader scope.

Adding the explicit `--window-storage-return` option to `--window-present`
selects a separate matched SD handback prototype. After the complete display
return it resumes the original store, freshly reads/verifies the same selected
package and contains SD again. Its success marker includes closed, resumed
and newly contained generations. It performs no SD writes or second FPGA load.
The option is bound through native/entry/firmware/composition receipts; the
original frame-only mode is unchanged. The
[matched physical diagnostic](../../../docs/validation/2026-11-04-de25-window-storage-return/README.md)
passes frame return in 44.05 seconds and fresh SD retention/containment in
55.79 seconds, within their separate 45/100-second limits. Ordinary core loading
and the normal native UI loop remain open.

The separate `--window-reload` option requires `--window-storage-return` and
selects one second native configuration from that freshly retained handoff.
The first session presents twelve changing frames; the second presents two,
then closes through a new independent firmware master stop. SD stays contained
and the original frame arena stays pinned. SDK, entry, cold-profile and FPGA
composition receipts carry the explicit selection. The second transfer keeps
the existing 20-second configuration limit. This prototype still requires a
matched bounded hardware test and supplies no ordinary package admission.

The repeated-frame diagnostic emits timed `STEP` records before and after
rendering, presentation and final return. A partial trace retains the last
completed frame and the CPU step which did not return. These progress records
add no device reads and supply no frame, drain or closure receipt themselves.

Nano images omit ELF symbols and DWARF sections and now store the complete ELF
in the entry's bounded gzip format. Go's runtime PC/line metadata remains in the
decoded image. This permits the full Tenfoot command handler in the unchanged
8 MiB FIT window. The frame diagnostic exercises search/text commands; the
normal native UI loop and physical launch integration remain separate work.

The Nano-only `fpga` program cross-builds a read-only TF-A SDM mailbox probe.
It reports silicon IDCODE and configuration status, without programming,
opening bridges or establishing a live package identity. See [SDM](sdm/README.md).
The Nano-only `hpsio` program uses a sealed HPS-first project and the contained
serial profile to request two exact read-only samples of 164 HPS pinmux
registers from matched secure firmware. It verifies both digest halves and
unchanged cold source ownership before reporting the result. It has no FPGA
configuration or device-opening operation. The observation covers this fixed
I/O subset; `HPSContextVerifier` still supplies no general boot admission.

The Nano-only `bootcontext` program compares the compiled HPS-first package
descriptor with a read-only secure-firmware digest under the same closed
owner/generation. It requires `--hps-first-project` and the exact source serial
probe profile. The [matched build](../nano/README.md#source-context-readback)
does not grant physical core admission or implement `BootHandoffVerifier`.
The separate `fpgacontext` program binds the compiler-derived CPU control map
as well, requests one secure identity read with data held, and checks that
control has closed again before reporting the observed ABI/build. It uses
the same required sealed project/profile and matched boot chain. See the
[CPU identity diagnostic](../nano/README.md#cpu-fpga-identity-diagnostic) for
the exact containment and failure contract; physical runtime admission remains
unavailable.
The Nano-only `fpgastatus` program selects the separate secure configuration
query, checks unchanged closed source ownership and reports SDM readiness.
It uses the same compiled source/control-map inputs and matched boot chain,
and performs no FPGA register access or bridge opening.
The Nano-only `fpgaload` program adds a source-bound first phase-two transfer.
Its build derives whole/chunk digests and a separate upload archive from the
verified sealed package, excludes an uncached transfer arena from every other
owner, and binds that descriptor into native code, SPL and BL31. It reads and
retains `FES/PKGTEST.BIN` through owned SD/FAT, contains SD, and
then checks the actual staged bytes under that closed ownership. The core RBF
is no longer embedded in the native executable. Private preparation captures
the contained SD generation and rejects a resumed or changed storage handoff.
Before that first transfer it requires a source-derived live CPU mapping
observation covering Go RAM, control, configuration DMA and reader memory.
Actual MMIO and configuration calls also check mappings around each operation;
uncertain results retain storage and reject further progress.
The same `ConfigurationTransfer` engine used by the runtime then drives the contained
firmware protocol, followed by readiness and live identity with control
reclosed. This one-shot cold profile grants no runtime compatibility or data
release. See [contained phase-two configuration](../nano/README.md#contained-phase-two-configuration).
The QEMU `fabric` diagnostic also models the dedicated configuration protocol
under a hypothetical held-bridge firmware contract. Its generation-bound drain,
buffer ownership and identity-before-release checks do not enable Nano programming.

## Build and run

The native module pins TamaGo and its modified Go compiler to **1.26.5**,
matching FES. Exact compiler, runtime and network pins are in
[toolchain.lock.toml](toolchain.lock.toml). Stock Go cannot build `GOOS=tamago`.
Extract the verified compiler archive under `out/embedded-base-tools/tamago`,
then populate the private cache explicitly:

```sh
cd platform/embedded/tamago
GOTOOLCHAIN=local \
GOMODCACHE="$PWD/../../../out/embedded-base-tools/tamago/mod" \
../../../out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go mod download
cd ../../..
(cd sources/FogCast && GOWORK=off go build \
  -o ../../out/embedded-base-tools/fogcast-host ./cmd/fogcast-embedded-host)
```

The runner requires Python 3, QEMU AArch64 with ramfb, and mtools. It creates
only ordinary files under a fresh output directory; it never opens a physical
block device. Compiler builds use `GOPROXY=off`, `GOSUMDB=off`,
`GOTOOLCHAIN=local` and `-mod=readonly`.

```sh
python3 platform/embedded/tamago/run.py tenfoot \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu out/de25-os-tools/zephyr-sdk-0.17.4/sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64 \
  --output out/native-go-system
```

The default test boots twice on the same FAT32 image. It verifies catalog
loading, sends live USB keyboard navigation and a layout change, captures QEMU's
actual display, independently reads the saved JSON, then checks that a fresh
boot restores the changed layout. The report records tool/ELF/source hashes,
commands, disk identity, logs and screenshots. This proves a clean restart;
it is not abrupt-power-loss or hardware acceptance. Use the runner's
`--help` for interactive operation and host binary selection. USB is the
default; `--input uart` selects the independent serial diagnostic backend.

For manual use, add `--interactive` (USB keys in the QEMU window, Ctrl-] in
the terminal to exit) and choose
`--display sdl` or a supported QEMU display. Use `--disk out/my-boot.img` to retain
preferences between invocations; a per-disk lock covers preparation and both
boots. The runner refreshes boot connection/input configuration on an existing image
without replacing its saved preferences.

`probe` uses the same compiler/QEMU arguments without storage or networking.
All output directories must be fresh and beneath the current worktree's `out/`.

## Platform contract

The image enters at EL1 and links at `0x40020000`, leaving the first 64 KiB
of RAM clear for early runtime tables and vectors, including the ELF header
page immediately below the link address. Go owns 256 MiB beginning
at `0x40000000`. The combined profile provides 512 MiB physical RAM: a separate
64 MiB DMA arena starts at `0x50000000`, followed by an 8 MiB framebuffer arena.
`board/qemuvirt.InitDevices` walks the installed page tables and maps the device
arenas as Normal noncacheable, non-executable memory. They never overlap the
Go heap. Disk, network, USB and display retain DMA ownership until completion;
ambiguous device failure retains buffers and requires reboot.

One CPU is used; interrupts are masked. Drivers poll with deadlines and yield
cooperatively. The runtime has no asynchronous goroutine preemption. The
software renderer
and scanout now yield every four rows and split large copies into 32 KiB chunks.
The actual renderer/network stress test passes 30 concurrent 192 KiB responses
with identical pixels and unchanged HTTP timeouts. Other CPU-heavy work still
needs cooperative chunks; this is not a general latency guarantee. No QEMU
address or firmware assumption is a DE25 board constant.

| Component | Implemented here | Still required for DE25 |
| --- | --- | --- |
| CPU/RAM/time | AArch64 runtime; separate QEMU and Nano UART/counter hooks; validated Nano RAM profile | Actual Nano firmware/DDR/EL1/cache handoff and IRQ qualification |
| Display | Shared RGBA-to-XRGB copier; QEMU RAMFB; optional Nano boot-owned window | Matching Nano scanout firmware, HDMI qualification and proven drain/handoff |
| Input | QEMU xHCI, real USB boot-keyboard control/interrupt transfers; optional PL011; Nano DWC2 PIO direct keyboard | Nano PHY/VBUS and real input qualification, hubs and gamepads |
| Storage | VirtIO reads/writes/FLUSH; Nano hardware-tested cold read-only SD/FAT32/package retention; software-tested reopening after a verified core return | Native writes and hardware qualification of returned-core SD access |
| Preferences | Existing preallocated FAT32 JSON file; data and metadata flushes | Allocation/update strategy and power-loss-safe publication |
| Network | Shared IPv4/TCP/HTTP; QEMU VirtIO; Nano XGMAC 2.10 with explicit DMA/PHY profile | Nano RGMII/PHY/cache/SMMU qualification, throughput and qualified entropy |
| FPGA | Retained-package lifecycle connected to tenfoot Play/Back; source-bound Nano first-load prototype and read-only probes | Physical runtime admission, active drain, repeated launch/return and DMA revocation |

FAT32 writes update existing preallocated files only; they cannot create,
allocate, rename or atomically replace them. The preference record reserves
4096 bytes. TamaGo's default `os` filesystem remains volatile; only the explicit
`tenfoot.PreferenceStore` backed by this volume persists settings. Native
Linux tenfoot keeps its existing file-based implementation.

The board's deterministic entropy source only seeds runtime hash tables.
Authentication and TLS are not qualified. The private test API uses numeric
IPv4 HTTP addresses; DHCP, DNS configuration, UDP, IPv6, network recovery and
throughput qualification remain outside this prototype. The separate
[Zephyr reference](../zephyr/README.md) is not part of this boot image.

The [fabric lifecycle model](fabricmodel/README.md) runs separately through
`run.py fabric`. It checks ownership and recovery order with software-only
images. It does not replace the production runtime. The separate model-only Play path
below now connects that lifecycle to the actual UI.

## Package inspection without a host OS

`run.py packages` builds a separate native diagnostic, creates a fresh FAT32
image from the existing shared format-2/3/4 conformance corpus, and inspects
its archives through `corepackage.RetainArchive`. The validator is shared with
normal FogCast staging: canonical archive shape, manifest rules, payload/map
hashes and package identity are unchanged. Reads allocate within the declared
archive bound and reject short, extra or non-progressing input. The caller's
reader must enforce deadlines for blocking I/O; cancellation is checked between
reads and validation phases.

```sh
python3 platform/embedded/tamago/run.py packages \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu out/de25-os-tools/zephyr-sdk-0.17.4/sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64 \
  --output out/native-package-inspection
```

This run needs no remote host, display or USB device. It verifies package IDs
and payloads, corrupts one runner-owned source file, proves the retained payload
is unchanged and a fresh read fails, then restores the exact original FAT32
image. Existing `--disk` images are refused by this diagnostic. Generated
archives are synthetic test fixtures, never deployable bitstreams.

The retained object keeps private bytes in RAM; returned metadata and payload
read cursors cannot mutate it. This is not durable package installation or
hardware admission. A syntactically valid unknown ABI remains valid for
inspection, with compatibility left to the runtime. Format-3/4 ROM maps are
validated, but linking and their activation receipts are not implemented here.
The native FAT reader currently limits individual files to 64 MiB, below the
shared 65 MiB archive cap. Maximum-size memory/latency behavior is unqualified.
No FPGA is programmed; physical tenfoot Play remains unavailable.

## Runtime admission of verified packages

`run.py admission` connects retained archives to the software fabric lifecycle.
It builds a host-only [oracle](../runtime/admission_oracle.cpp) from the existing
runtime's `OpenCorePackage` and `CheckCoreCompatibility` implementation. This
keeps profile/ABI pairings and interface semantics in their current owner,
libmister-runtime. The helper constructs no physical backend. Its restricted
model-driver snapshot uses generated runtime contract identities, and its
per-package decisions bind the exact verified package ID. The oracle binary
hash identifies the policy producer in the receipt.

```sh
python3 platform/embedded/tamago/run.py admission \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu out/de25-os-tools/zephyr-sdk-0.17.4/sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64 \
  --output out/native-runtime-admission
```

The runner also needs a host C++14 compiler. It records compiler, oracle,
compiler-reported source/header dependencies, native ELF and FAT32 hashes.
It downloads nothing and refuses an existing disk. `RUNTIME.JSON` is a trusted,
runner-owned **test input**, not an authenticated admission receipt. Editing it
cannot authorize physical programming: this gate has only `LoadModel` and
accepts only the explicit `qemu-model-only` scope. A production backend must
perform its own runtime-owned checks against retained bytes and live hardware.
The separate [native Go compatibility checker](runtimeadmission/README.md)
now implements the runtime's target/profile/ABI and interface semantics over
exact retained archives. Its DE10 registry and ABI identities come from the
shared definitions. The native diagnostic compares each Go decision with the
C++ oracle before the existing model lifecycle, including linked ROM cases.
This removes C++ execution from the native contract-checking implementation;
the host oracle remains a validation reference. It supplies no qualified Nano
profile, boot proof, live identity or physical activation method.

The native gate checks the expected package ID, target, programming profile,
ABI, required interfaces and exact-package compatibility decision before calling
`Controller.Load`. Missing policy or decisions fail closed. Successful loads
copy the retained payload into the software model and bind the complete package,
core, ABI version and build identity to the identify/release sequence. Rejection
leaves the current core, input generation and prepared display frame intact.
An accepted replacement still saves the old owner and waits for its frame to
finish; identity failure recovers to idle before release.

The diagnostic runs all eleven structurally valid shared corpus entries plus
six compatibility cases: missing mandatory gamepad, newer ABI, diagnostic-only
profile, unknown required interface, unknown optional interface, and an invalid
optional persistence declaration. The last case requires the C++ semantic check;
matching required capabilities alone would miss it. An unknown optional extension
can pass. Default/absent policy, absent exact-package decisions and wrong expected
IDs are also rejected without changing ownership. A full build-identity mismatch
exercises recovery to idle.

This backend models simple-game 1.0 and a restricted application 1.0 driver
with gamepad and fixed video. Raw
format-2 payloads retain their 1 MiB bound. Format-3/4 raw loads reject; the
separate retained ROM activation path below supplies their required receipts. Existing DE10-Nano corpus targets remain
DE10-Nano targets; none become DE25-compatible. Production driver resolution,
HPS DDR layout, native display composition and physical lifecycle checks remain
outside this prototype. There is no FPGA programming, live identity proof or physical hardware
acceptance. Model-only tenfoot Play is covered below.

The same corpus can run on the host with the race detector after a native run:

```sh
FES_ADMISSION_FIXTURES="$PWD/out/native-runtime-admission/package-fixtures" \
  GOWORK=off go -C platform/embedded/tamago test -race \
  ./runtimeadmission ./admissioncheck ./fabricmodel
```


## Retained ROM linking and activation receipts

The same `run.py admission` command now also links three full synthetic RBF
fixtures: format-3 firmware, format-3 cartridge, and format-4 BIOS plus cartridge.
`Archive.LinkROMs` copies sources after checking exact IDs, roles, descriptor
order and lengths, then calls the existing target linker against the retained
sealed map. `LinkedArchive` owns those sources, the package and programmed bytes
in RAM. `Receipt`, `Inspection`, `Source` and `Programmed` expose independent
metadata/copying readers. No path lookup or filesystem staging is needed to link.

`LoadLinkedModel` accepts this opaque object plus the expected package ID and
receipt SHA256. The receipt binds source IDs/roles/digests/sizes, sealed map,
package and programmed digest/size. A reconstructed JSON receipt is insufficient.
Compatibility still requires the C++ oracle decision for the original package;
linking alone cannot grant an unsupported target, ABI or interface. Before any
owner transition, the gate verifies the derived output digest and creates a
private model image. Simulated identity includes the complete receipt digest
and programmed digest, so a different ROM for the same package cannot be released
under the original selection.

The diagnostic uses existing Mistral golden synthetic ROM vectors rather than
inventing CRAM encoding. All three linked outputs must match its ramp-vector
hash and size. Each case mutates a runner-owned FAT32 ROM source after retention,
independently links that changed source, rejects the changed object under the
original receipt, activates the original retained bytes, and restores the source.
The entire FAT32 disk must finish with its original hash. Missing/wrong receipts,
wrong package IDs, raw format-3/4 loads, missing sources and wrong roles reject
without retiring the old owner or frame. A simulated live receipt mismatch must
recover to idle. Host race tests compare their full receipts with native results.

Linked model buffers are bounded at 8 MiB to accommodate the approximately 2 MiB
synthetic bitstreams. This is an emulation limit, not a hardware-package size or
latency qualification. Sources survive for the opaque object's lifetime; the
model separately owns its programming-byte copy through completion/recovery.
Nothing is durably installed. Other expansion families, initial media, real-board resources
and live GP identity remain separate. Selected CPU/video parts now extend the
retained activation path as described below. These are
Cyclone V test vectors and supply no Agilex/DE25 programming support. Linker CPU,
heap and concurrent UI/network latency have not been qualified on the board.


## Tenfoot Play and return through the model

```sh
python3 platform/embedded/tamago/run.py play \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --qemu out/de25-os-tools/zephyr-sdk-0.17.4/sysroots/x86_64-pokysdk-linux/usr/bin/qemu-system-aarch64 \
  --output out/native-tenfoot-play
```

This builds the existing `cmd/tenfoot` with an explicitly selected model boot
configuration, then drives Play and Back using QEMU's USB keyboard. It retains
and links real synthetic Mistral fixtures from FAT32, checks the selected package,
source digests and receipt, activates the model, parks/resumes the existing UI
renderer, and returns to the same catalog selection. The session contains no
emulated gameplay or physical FPGA output. The [model adapter](modelbackend/README.md)
explains actor, frame, input and request ownership.

The test covers normal launch/return, wrong ROM, wrong receipt, canceled
preparation, programming/identity recovery and a failed save followed by retry.
It also rejects delayed input/display events. QMP screenshots and complete
state/command traces accompany the receipt. Tenfoot handles its own Quit, idle
release and backend close before the runner reaps QEMU. All input disk bytes
must remain unchanged. QMP waits drain guest serial output while assembling
possibly partial replies; their existing five-second control timeout is unchanged.

The host-only fixture helper uses an installed standard Go toolchain to derive
expected receipts with the same linker. No alternate ROM encoding is introduced.
The ordinary `run.py tenfoot` mode still browses the real private network host,
checks USB navigation and persists layout across two boots. Model mode uses an
in-process diagnostic catalog and does not claim production host/target launch
orchestration, physical programming, board DMA containment or DE25 acceptance.


## CPU/video parts in retained activation

`run.py play` now runs eleven cases. In addition to the existing seven, it
activates a synthetic Coleco application shell with one CPU expansion and one
raster video part, then rejects changed, missing and incompatible selections.
Both part payloads make actual fenced bit changes; the pre-ROM overlay and final
programmed output have different digests. Existing admission and composition
implementations enforce the exact shell/package/build and closed socket roles.
No alternate linker or compatibility policy is introduced.

`Archive.LinkROMs(ctx, sources, parts...)` accepts `PartInput` objects containing
an expected asset ID and archive bytes. It copies/parses each bounded archive
and retains it behind a copying `Part` reader. The receipt carries the canonical
`PartsComposition` and its pre-ROM overlay digest; `Overlay` exposes an independent
reader. The existing ROM identity still carries the final programmed digest.
Changing a valid part's identity changes the activation receipt even when its
payload and programmed bytes are identical. Model admission still needs the
selected full receipt, and the active session exposes its parts composition
only after release.

This exercises the existing CPU/video parts mechanism, not every multi-slot
expansion family. It provides no FPGA programming or DE25 acceptance. The
[board adaptation plan](../board-adaptation.md) describes the next real-driver
work while keeping QEMU as the regression backend.
