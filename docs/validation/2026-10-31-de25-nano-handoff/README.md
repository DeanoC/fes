# Nano memory/device handoff and matched test files

Base: `b7bb73a42d55347e055ace95a41cd15d2846d153`. Scope: prepare the first
serial-only Linux-free probe on TRWU8CBL. No kit, UART, JTAG, flash or physical
card was contacted or changed during this work. The executable hardware gate
is compiled and packaged; it has not run on the board.

The new profile follows the pinned Terasic firmware sources rather than the
earlier synthetic fixture. It reserves BL31/initial U-Boot, the SPL FIT buffer,
OCRAM and upper-DDR SPL/U-Boot envelopes, assigns Go `0x84000000..0x94000000`
and stages the loader separately at `0x96080000`. The command checks live DRAM
banks, LMB reservations, malloc, relocation, FDT and stack before loading.
Native SD is explicitly disabled alongside USB/network/display/FPGA control.
The source-derived readiness declarations depend on the runtime gate passing.

The pinned TF-A topology places CPU identity in MPIDR Aff1. Both possible
primary CPUs (`0`, `0x200`) are accepted, with PSCI OFF required for the other
three. The Nano loader now accepts either primary; QEMU retains its own affinity
check. The dedicated U-Boot export removes the post-cleanup secondary wake-up
interrupt. Its counter must be 400 MHz and its console must be UART1 in 8N1.

Before loading, the command stops watchdogs, checks CPU state and contains
FPGA bridges and non-SD DMA/peripheral writers. SPL and U-Boot SD use PIO.
The entry SHA-256 is compiled into the dedicated U-Boot. A late hook runs after
device removal and before CPU cleanup, resets SD, and repeats checked bridge,
peripheral, watchdog, UART and CPU observations before the guarded EL1 entry.
The actual bridge SMC/MMIO, device reset effects, physical drain and recovery
remain hardware checks; host policy tests do not establish them.

The source export and C adapter cross-build successfully against Terasic
U-Boot `d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413`, with the previously retained
contained BL31 from TF-A `580dcb5c4e10aa7daff2bf69d493c076a6d3e665`.
The FIT extraction verifies the selected BL31 bytes and every FIT component.
The build has no USB/network stack, persistent environment, FAT/MMC writes or
SD DMA. It waits at the prompt for one `fesboot` attempt.

`out/nano-handoff-test-kit-v6/` contains the matched SPL, FIT, native entry,
FPGA-first `native-probe.sof`, baseline `recovery-reference.sof`, manifests and
144 MiB `nano-probe-sd.img`. The FIT is at raw sector `0x200`, before FAT32 at
sector `32768`; the same FIT and exact entry are copied into FAT. Packaging
checks raw FIT bytes, MBR/BPB/backup geometry, and every FAT file's readback.
PFG combines the exact selected SPL and inventoried vendor SOF and confirms
the Nano device/FPGA-first order. Embedded SPL bytes are not independently decoded.

Five host gate/export tests cover CPU state, every required reset bit, timer,
watchdog, UART, live reservation overlap/overflow, image mutation, hook ordering
and removal of the secondary wake-up. Existing profile, ELF and firmware tests
pass. The modified entry also passes the existing four QEMU probe/rejection
cases. Three altered FIT/SPL/configuration packaging attempts reject before
creating output. None of these results is physical boot or tenfoot acceptance.

Initial failed candidates are retained: one config initially left the network
choice enabled, an early C build used unsupported U-Boot standard-integer
definitions, and the first PFG report was decoded using UTF-8 instead of its
single-byte output encoding. Their fixes preserve the requested disabled
devices and do not relax an admission or timing requirement.

See [the test recipe](../../../platform/embedded/nano/README.md) for construction,
expected markers and the bounded TRWU8CBL recovery procedure. Use a spare SD
and preserve the factory card. Physical testing must verify the expected build
ID, live gate, native probe pass, and subsequent factory restoration. The
retained source/artifact set is under
`/home/deano/fes-retained/de25-nano-handoff-20261031/`; `evidence.json` identifies
the exact prepared files and keeps hardware status explicit.
