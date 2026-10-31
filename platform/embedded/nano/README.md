# Nano serial probe boot chain

The [first TRWU8CBL trial](../../../docs/validation/2026-10-31-de25-nano-serial-test/README.md)
booted the matched chain but stopped at the bridge service/readback gate;
native execution remains pending. Factory Linux was restored without a power cycle.

This prepares the first Linux-free CPU/RAM/UART test on **TRWU8CBL** using the
pinned Terasic TF-A and U-Boot sources. It produces a matched FPGA-first SOF,
SPL, FIT, native entry and regular-file test SD image. The actual memory and
device checks run on the Nano before entry; cross-compilation and packaging
do not establish that those checks will pass on hardware.

The first probe uses UART1, the architectural timer, Go allocation/GC and the
volatile memory filesystem. Native SD, USB, Ethernet, display and FPGA control
are explicitly disabled in its profile. U-Boot reads the payload from FAT32
using polled PIO; both SPL and full U-Boot have SD DMA disabled. The dedicated
U-Boot has no USB/network stack or persistent environment and no FAT/MMC writes.
It stops at its serial prompt. `fesboot` is the only supported entry command.

## Source-derived memory and device contract

| Resource | Source and enforced selection |
| --- | --- |
| Physical DDR | Nano U-Boot DTS declares `0x80000000..0xc0000000`; the command requires the same live DRAM bank |
| TF-A and initial U-Boot | TF-A declares BL31 `0x80000000..0x82000000`; FIT initially loads U-Boot at `0x80200000`; keep that entire envelope reserved |
| SPL FIT buffer | FIT loads at `0x82000000`; reserve the next 32 MiB conservatively |
| Go RAM | `0x84000000..0x94000000`, with early tables and initial stack protected by the native ELF validator |
| Entry staging | `0x96080000..0x98080000` maximum, separate from the Go allocator |
| SPL/U-Boot upper DDR | Reserve `0xb0000000..0xc0000000` conservatively; the SPL uses malloc at `0xbfa00000` and BSS at `0xbff00000` |
| Live U-Boot | Reject any Go/staging overlap with LMB reservations, malloc, relocated U-Boot, FDT, and a 1 MiB stack envelope |
| CPUs | TF-A topology uses Aff1: MPIDRs `0`, `0x100`, `0x200`, `0x300`; accept primary A55 `0` or A76 `0x200` and require every other CPU OFF through PSCI |
| Timer | Require the pinned TF-A counter frequency, 400 MHz; the loader enables EL1 counter access |
| Console | Nano DTS aliases `serial0` to UART1 at `0x10c02100`; preserve clocks/divisor and verify 8N1, reset state and transmitter empty |

The local vendor U-Boot DTS uses CPU `reg` values `0..3`, whereas its pinned
TF-A topology decodes Aff1. The command queries PSCI using the TF-A affinity
mapping directly. It does not infer CPU power state from those DTS values.
TF-A's broader DRAM MMU mapping is also not evidence of board RAM capacity.

The command checks live CPU/memory state before loading. It stops registered
watchdogs, closes all four Agilex 5 FPGA bridges using the checked TF-A service,
and holds HPS DMA, USB, Ethernet and NAND functions/interfaces in reset before
reading and hashing the entry. SD alone remains available in PIO mode for that
read. The SHA-256 is compiled into this U-Boot binary. Go RAM is reserved from
later U-Boot allocation. A late hook, after U-Boot removes its devices and
before CPU cleanup, closes/checks bridges again, resets SD, and verifies CPU,
timer, watchdog, UART and reset readback. Only then does the existing guarded
entry transition to EL1 and load the Go ELF. The dedicated U-Boot export removes
the normal secondary-CPU wake-up interrupt after cleanup, retaining the checked
OFF state through entry.

Source masks come from the pinned Agilex 5 reset bindings and TF-A reset code;
the TF-A build preserves bridge handshake failures. Register/SMC completion
and physical traffic drain still require the board trial. An unreturning SMC
or MMIO transaction cannot be preempted by a software deadline. The image has
no native peripheral or FPGA programming acceptance.

Failures after containment park and preserve the altered state. The command
does not restore uncertain devices or automatically retry. A diagnostic boot
ID binds the entry digest and handoff source hashes; hashes are not signatures.

## Build and inspect

Use the pinned compiler and clean Terasic references described in the
[firmware guide](../firmware/README.md). First build the contained BL31 using
`build_terasic.py`, then use its completed output below:

```sh
python3 platform/embedded/tamago/run.py probe --board de25-nano \
  --profile platform/embedded/nano/probe-profile.json \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --output out/nano-probe
python3 platform/embedded/entry/build.py --target de25-nano \
  --payload out/nano-probe/probe.elf --nano-build out/nano-probe \
  --profile platform/embedded/nano/probe-profile.json --image-address 0x96080000 \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/nano-entry
python3 platform/embedded/firmware/build_terasic_uboot.py \
  --reference-repo out/vendor-uboot --bl31-build out/vendor-bl31 \
  --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --native-entry-build out/nano-entry --output out/nano-uboot
python3 platform/embedded/nano/package.py \
  --uboot-build out/nano-uboot --entry-build out/nano-entry \
  --vendor-sof /absolute/path/golden_top_hps.sof \
  --quartus-pfg /absolute/path/quartus_pfg --output out/nano-test-kit
```

The FPGA input must match the previously inventoried vendor SOF SHA-256
`e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09`.
PFG combines it with this build's exact SPL and checks the device and FPGA-first
order. Its embedded SPL is not independently decoded. The test SD image places
the exact FIT at raw sector `0x200`, before a FAT32 partition at sector `32768`.
It also contains the same FIT as the SPL's filesystem fallback and the exact
entry at `FES/ENTRY.IMG`. Raw offsets and every copied FAT file are read back
and compared. The source-derived profile describes this executable recipe;
its readiness declarations become valid only after the live gate passes.

## Bounded TRWU8CBL test

1. Check the current top-board status and record the operator/claim. Verify
   physical serial `TRWU8CBL`, UART `if02` at 115200, and its exact JTAG cable.
   Record the current factory boot/media identity and halt Linux cleanly.
2. Keep the original factory SD intact. Put `nano-probe-sd.img` on an explicitly
   identified spare SD, then insert that card. The package command never writes
   a card; no physical device path is selected automatically.
3. Program **only volatile JTAG SRAM** with `native-probe.sof` on the exact top
   cable. Require JTAG ID `4362C0DD`. Preserve the original serial log throughout.
4. Require the U-Boot banner's `-fes-nano-<build_id>` suffix to match
   `BOOTCHAIN.JSON`. An unexpected loader, FIT, fallback or banner stops the test.
   At the `SOCFPGA_AGILEX5 # ` prompt issue `fesboot` once. Retain the live memory map and all gate
   output. Acceptance requires `FES_NANO_HANDOFF PASS serial-only`, the entry
   handoff markers, and `FES_NATIVE_GO_PROBE PASS os=tamago board=de25-nano`.
5. After the pass or a contained failure, restore the original factory SD and
   `recovery-reference.sof`. Same-SOF JTAG recovery was observed on the other
   Nano; if the top board does not resume its baseline, use the already offered
   physical power cycle. Verify the factory boot/media identity before releasing
   the board. No flash programming or factory-card modification is part of this
   trial.

This is a serial CPU/RAM diagnostic. Tenfoot, the sealed FPGA endpoint and
physical launch/return remain subsequent tests after the probe handoff passes.
