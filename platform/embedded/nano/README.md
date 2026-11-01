# Nano recovery uploads and cold serial boot

This prototype joins a repeatable recovery upload tool to a matched Linux-free
SPL, BL31 and native serial probe. The [current validation](../../../docs/validation/2026-10-31-de25-cold-upload/README.md)
records the first host tests, cross-builds and regular-file packaging. The
[subsequent TRWU8CBL trial](../../../docs/validation/2026-10-31-de25-upload-cold-trial/README.md)
passed the production upload core/transport with a 4.7 MB FIT and independent
SD readback. Native SPL stopped at its initial ownership check before FIT/DDR
loading or EL3/native entry. The POR mask is corrected from the official CS/R1
register map; the observed CPU reset value still rejects. The follow-up adds
MPIDR and fresh CPU/reset diagnostics and remains hardware unqualified. The earlier manual
[Ethernet and SD scratch trial](../../../docs/validation/2026-10-31-de25-ethernet-sd/README.md)
passed with the separate recovery loader; that result does not qualify these
new artifacts.

The subsequent [CPU-only TRWU8CBL observation](../../../docs/validation/2026-11-01-de25-cpu-state/README.md)
programmed successfully but captured no UART bytes within 40 seconds. It
provided no new CPU/reset evidence. Factory boot was restored by reference-SOF
JTAG and authenticated without a card swap or physical power cycle.
The [next trace candidate](../../../docs/validation/2026-11-01-de25-cpu-trace-preparation/README.md)
preserves the actual vendor startup and adds earlier checkpoints. Its
[bounded TRWU8CBL trial](../../../docs/validation/2026-11-01-de25-cpu-trace-trial/README.md)
programmed successfully but again captured no UART bytes. JTAG still responds;
the user permits retaining this exclusive kit's diagnostic without factory
restoration. A subsequent [debug readback and console control](../../../docs/validation/2026-11-01-de25-console-control/README.md)
found that the SOF disables HPS DAP access; no OCRAM dump was obtained. Removing
the pre-console cold sample also left UART silent. The control diagnostic is
retained and the claim is held. Target execution and park remain unconfirmed.

The same spare card carries two paths:

| Volatile JTAG image | SPL boot file | Purpose |
| --- | --- | --- |
| `network-recovery.sof` | `/u-boot.itb` | Recovery loader, manual prompt, Ethernet download and experimental file writing |
| `native-probe.sof` | `FES/UPLOAD.BIN` | Exact sealed native FIT, cold serial ownership handoff, manual `fesboot` |

Recovery's raw prefix, root boot FIT and `FES/RECOVERY.JSON` stay unchanged.
Uploading a file does not activate it. A different native FIT needs its matched
SPL/SOF because SPL checks the whole file's compiled SHA-256 **before parsing**.
The parser then reads the verified RAM copy at byte granularity, without FAT
DMA rounding beyond the sealed file or rereading FAT. This is
source-bound diagnostic sequencing, not signed or measured secure boot.

## Repeatable network upload

`upload.py` defaults to a file-only plan. It supports one experimental file,
`FES/UPLOAD.BIN`, of 1 byte through 8 MiB. For a native candidate, supply the
matched build's `build/u-boot.itb`:

```sh
python3 platform/embedded/nano/upload.py \
  --payload out/nano-uboot/build/u-boot.itb \
  --recovery-kit out/nano-recovery-kit \
  --host-ip 192.168.10.202 --output out/nano-upload-plan
```

Use the **current** local Ethernet address for `--host-ip`. For an authorized
live run, boot the exact recovery loader with the spare card and leave it at
`SOCFPGA_AGILEX5 # `. Add `--execute` and choose a fresh output directory.
The operator needs access to the Nano UART and permission to bind UDP port 69
(normally run the same command using `sudo`). No system TFTP service is changed.
The tool claims `~/kit-status/de25-TRWU8CBL-STATUS.md` and selects only the stable
TRWU8CBL UART `if02` link. It does not shut Linux down, program JTAG or boot a
candidate. The default volatile MAC is `02:fe:25:08:cb:01`; DHCP supplies the
board address, so no board IP is assumed.

Before the only FAT write, the tool checks the loader banner, live RAM and LMB
reservations, spare-card capacity/partition layout, protected first 16 MiB,
root recovery FIT and card manifest. It serves one randomized TFTP filename to
one observed client, requires the final acknowledgement and verifies RAM SHA-256.
It writes `FES/UPLOAD.BIN`, reads it into a separate buffer, checks SHA-256 and
compares bytes, then rechecks the protected prefix and recovery FIT. The receipt
contains the commands, outputs and completion state.

Timeouts, wrong cards, hashes, ownership loss and uncertain writes stop the
attempt. There is no automatic replay or activation. If the serial prompt is
uncertain, the tool leaves its claim held for recovery. A completed write is
verified readback, not an atomic FAT transaction or a power-loss guarantee;
FAT metadata damage can still prevent recovery boot. Keep the factory card safe.

## Actual cold ownership handoff

For an observation that cannot proceed into the native chain, `cpu_diagnostic.py`
builds a separate SPL from a completed cold serial build and its exact pinned
tools/reference SOF. It keeps the first sample and fresh MPIDR/reset report,
then unconditionally prints `FES_NANO_CPU_DIAG PARK no-ddr no-storage no-bl31`
and spins with `yield`, including when admission would pass. The private
`board_init_f` ends at that report; later DDR/device initialization is removed.
SPL storage and watchdog options are disabled and the linked park and excluded
symbols are checked. The vendor's `SPL_ATF` and its required `SPL_LOAD_FIT`
selection stay enabled; the later ATF invocation is independently replaced by
a stop and no firmware payload is supplied. The build checks the actual Nano
startup sources and emitted startup text against the seed. No native CPU/reset
admission rule changes.

The trace records 18 checkpoints from first C entry through the cold sample,
device setup, managers/pins, timer, mailbox, clocks, console, report and park.
Its 304-byte record lives in loaded OCRAM data. Before clock setup returns it
only writes that record. After clocks, guarded raw UART output can replay the
record before normal console setup if UART1 is already released and in 8N1.
Otherwise it tries after normal console initialization. It does not change
UART reset, clock, divisor, framing or interrupts, and caps TX polling per line.
Complete serial lines carry the build ID, stage, value, first-C MPIDR and EL.
Partial, reordered, mixed-identity or inconsistent-affinity traces never count
as a complete capture. None of these observations grants native admission.

`--console-control` prepares a separate contained comparison. It retains the
seed configuration except for the build identity and performs no cold proof
or tracing before normal console initialization. Both watchdog-start paths
are removed: the first board initializer ends in the unconditional diagnostic
park, and the later SPL initializer independently stops on entry. The linked
image is checked for absent watchdog-start, cold-proof and firmware-entry
symbols. DDR/storage drivers can remain linked with this configuration; the
two initializers cannot proceed to their later initialization/loading paths.
This control emits only checkpoints `10`, `11` and `12`. Six bracketed register
observations run between the report checkpoint and park. Its MPIDR/EL scope is
after console initialization, not first C entry. Those observations never
supply the initial trusted ownership proof or grant native admission.

If execution stops before clock setup, a silent serial result can still be
inconclusive. Earlier checkpoints remain in OCRAM, but their retrieval over
JTAG has not been qualified. The retained reference SOF has HPS DAP disabled;
an HPS debug configuration would need a separately built and checked image.
A host timeout also cannot preempt a stalled target MMIO operation.

```sh
python3 platform/embedded/nano/cpu_diagnostic.py \
  --reference-repo out/vendor-uboot --seed-build out/nano-uboot \
  --entry-build out/nano-entry --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --vendor-sof /absolute/path/golden_top_hps.sof \
  --quartus-pfg /absolute/path/quartus_pfg --output out/nano-cpu-observation
```

This prepares files only. A separately authorized bounded volatile JTAG run
can keep the factory card inserted after a clean Linux shutdown. Capture the
exact SPL banner, `BEGIN`, `SCOPE` and unconditional park. Restore and authenticate
the factory system when recovery is needed or the baseline is required. For
TRWU8CBL the user explicitly permits leaving the diagnostic loaded; record the
actual state and hold the claim when execution/park are uncertain. A responding
JTAG chain does not prove CPU execution or a factory boot. This fixture neither
boots BL31 nor qualifies native execution, DDR, storage, ownership or drain.

The dedicated SPL samples SDM boot kind, reset cause, peripheral/bridge resets
and secondary CPU state before device initialization. The pinned
[vendor DDR code](https://github.com/terasic/u-boot-socfpga/blob/d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413/drivers/ddr/altera/sdram_agilex5.c)
defines POR, cold reset, nCONFIG and JTAG configuration as kinds 0, 2, 3 and 4;
these are the reviewed entry paths. Reset-status bits 0 and 2 record SDM cold
reset and the sticky last-POR flag; the latter is not independent history proof.
Warm reset, RSU, unknown kinds, other reset
causes, open bridges or released unused masters reject. Reset bits alone do
not supply history or traffic-drain proof.

Only SD is used, synchronously in PIO mode. SPL loads BL31, U-Boot and the native
entry from the sealed FIT. Before entering BL31, it verifies the entry hash,
disables SD interrupts, waits for independent SDHCI idle/buffer observations,
disables the card clock and checks readback. It then asserts functional resets
followed by OCP resets, with barriers/readback. Failed drain observations stop
before those resets. See the separate functional/OCP ordering in the
[HPS reset sequence](https://docs.altera.com/r/docs/814346/26.1/hard-processor-system-technical-reference-manual-agilextm-5-socs/reset-de-assertion-sequence?contentId=Z8zFZqWjknEOqRcvl2Xvbw).

The record stays in SPL's loaded OCRAM data, not its DDR BSS. SPL passes its
pointer/cookie directly to early EL3 setup. BL31 bounds-checks and copies it
once, checks the exact source chain, owner, epoch, boot kind and live scope,
then installs the real platform ownership/master callbacks. Those callbacks
verify continued containment without touching SD while it is in reset. Any
restarted producer, open bridge, changed CPU/reset scope or replay rejects.
The additional `SCOPE` line reports MPIDR, primary index, fresh reset/CPU
registers and reset timeout status after clock/console setup. It cannot replace
a failed first sample. The first physical tuple's CPU value `0xf` still fails
the current primary/secondary inventory contract; its semantics/timing need
qualification before native boot can continue.

This cold serial firmware permits only read-only ownership Snapshot. It denies
bridge opening, reconfiguration and release, and reports zero configuration
features. It supplies no active USB, Ethernet or FPGA traffic-drain policy.
The default controlled firmware still has an unready provider; recovery uses
the separate strict firmware. See the [firmware guide](../firmware/README.md).

Full U-Boot never probes/reopens SD and has no USB, network, DMA, flash-storage
commands or persistent environment. `fesboot` hashes the preloaded entry and
checks memory, CPU, watchdog and private ownership state. A late hook after
device removal rechecks the same owner/generation, timer, UART and resets.
The existing guarded EL2-to-EL1 entry then starts the serial Go probe. There
is one attempt per boot; uncertain failures park. Tenfoot and physical FPGA
launch/return remain later integration steps.

## Source-derived memory contract

| Resource | Enforced selection |
| --- | --- |
| Physical DDR | Require the live Nano bank `0x80000000..0xc0000000` |
| BL31 and initial U-Boot | Reserve `0x80000000..0x82000000`; initial U-Boot load is `0x80200000`, checked against BL31 ELF segments/BSS |
| SPL parser workspace | Conservatively reserve `0x82000000..0x84000000` |
| Go RAM | `0x84000000..0x94000000`, with early tables/stack checked by the entry validator |
| Verified FIT copy | `0x94000000..0x94800000`, at most 8 MiB |
| Preloaded entry | `0x96080000..0x98080000` maximum, separate from Go RAM |
| SPL/U-Boot upper DDR | Reserve `0xb0000000..0xc0000000`; SPL malloc/BSS are near its top |
| Live U-Boot | Reject Go/entry overlap with LMB, malloc, relocated code, FDT or the stack envelope |
| CPUs | Accept A55 Aff1=0 or A76 Aff1=2; require other CPUs held/OFF, retain that state through cleanup |
| Timer/console | Require 400 MHz architectural counter; preserve UART1 at `0x10c02100`, clocks/divisor and checked 8N1 state |

TF-A's larger MMU mapping and the U-Boot DTS CPU `reg` numbers do not replace
the live RAM and Aff1/PSCI checks. Unreturning MMIO/SMC operations cannot be
preempted by software poll limits or a host capture deadline.

## Build the matched candidate

Use the pinned clean Terasic references and compiler environment from the
[firmware guide](../firmware/README.md). First build the serial probe and
[native entry](../entry/README.md), then the cold firmware and native loader:

```sh
python3 platform/embedded/tamago/run.py probe --board de25-nano \
  --profile platform/embedded/nano/probe-profile.json \
  --compiler out/embedded-base-tools/tamago/usr/local/tamago-go/bin/go \
  --output out/nano-probe
python3 platform/embedded/entry/build.py --target de25-nano \
  --payload out/nano-probe/probe.elf --nano-build out/nano-probe \
  --profile platform/embedded/nano/probe-profile.json --image-address 0x96080000 \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/nano-entry
python3 platform/embedded/firmware/build_terasic.py \
  --reference-repo out/vendor-tfa --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --nano-owned-bridges --nano-cold-serial-entry out/nano-entry \
  --output out/nano-bl31
python3 platform/embedded/firmware/build_terasic_uboot.py \
  --reference-repo out/vendor-uboot --bl31-build out/nano-bl31 \
  --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --native-entry-build out/nano-entry --output out/nano-uboot
python3 platform/embedded/nano/package.py \
  --uboot-build out/nano-uboot --entry-build out/nano-entry \
  --recovery-kit out/nano-recovery-kit \
  --vendor-sof /absolute/path/golden_top_hps.sof \
  --quartus-pfg /absolute/path/quartus_pfg --output out/nano-test-kit
```

The initial SPL compilation has a zero seal and cannot boot a FIT. After the
FIT is built, the builder compiles its exact size/hash into SPL and rebuilds
only SPL, requiring the FIT to remain unchanged. The FIT does not contain SPL,
so the seal has no hash cycle. PFG combines that SPL with the inventoried
vendor SOF (`e71a72180cb77ddd05a9c2ae49afa739d74786f9366f00233fd27bbebff6ea09`)
and checks the Nano device and FPGA-first order. Embedded SPL bytes have not
been independently decoded from the resulting SOF.

Packaging takes a corrected FAT-boot recovery kit, copies its regular-file SD
image, and adds the native FIT at `FES/UPLOAD.BIN` plus `FES/BOOTCHAIN.JSON`.
It checks readback and preserved recovery files/prefix. It produces both
volatile SOFs and `nano-probe-sd.img`; it never writes a physical card.
The kit directory's `u-boot.itb` is the native upload payload. Do not copy it
over the card's root `u-boot.itb`, which belongs to recovery.

## Next bounded TRWU8CBL trial

1. Claim the top board in its current status file, verify serial/cable/UART,
   record the factory boot identity and shut Linux down cleanly. Preserve the
   original factory card and verify the spare's device identity before writing.
2. With the combined spare card, load `network-recovery.sof` through volatile
   JTAG. Run one upload/readback of the matched native FIT using `upload.py`.
   Require the exact recovery checks and successful receipt before continuing.
3. Load `native-probe.sof` through the exact top JTAG cable. Require device ID
   `4362C0DD`, the valid `FES_NANO_SPL BEGIN`, verified-before-parsing FIT marker,
   SPL handoff and `FES_NANO_OWNED_BOOT provider=1 result=0 reset=f`.
4. At the matching `-fes-nano-<build_id>` prompt issue `fesboot` once. Require
   both ownership snapshots to report the same nonzero owner, generation 1 and
   exact flags `0xdf`; then `FES_NANO_HANDOFF PASS serial-only`, native entry
   markers and `FES_NATIVE_GO_PROBE PASS os=tamago board=de25-nano`.
5. Stop on a failure or incomplete marker sequence. Restore the original card
   and `recovery-reference.sof`, verify the factory boot/media identity and
   release the claim. Use a physical power cycle if volatile recovery fails.

This trial qualifies only serial CPU/RAM and the observed handoff. Native
USB/SD/network drivers, active master stop/drain, HDMI and FPGA programming
still need their own implementation and physical checks.
