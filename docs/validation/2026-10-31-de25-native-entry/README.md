# DE25 native entry: QEMU and off-board evidence

This record covers an exploratory native entry path on base commit
`e4c21bcd54226a1962ba180c8a9ec90053827eb5`. It is QEMU/host evidence, not a
physical boot, a qualified Nano handoff, or hardware acceptance.

The pinned Terasic U-Boot source at
`d0f8813fd6bf8191d49c8b8d2035ffd2ae10c413` was built with its QEMU ARM64
configuration and run on a one-CPU Cortex-A53 QEMU `virt` machine. The real
`booti` path cleaned up and entered the loader at EL2. The loader validated and
copied the actual TamaGo Go probe, transitioned to EL1, and the probe passed its
timer, goroutine, allocation/GC and volatile filesystem checks. The probe's
normal direct-QEMU boot also passed. Three further guests rejected a bad ELF,
a segment targeting the live loader, and entry at EL1 instead of EL2. The
loader's staging is outside Go RAM; the target's early tables and initial
stack are reserved separately. Host tests exercise additional malformed and
overlapping ELF segments before any load plan is published.

Changing the Go link address preserved a full tenfoot model launch and return.
All 11 model cases also passed through the new U-Boot loader, including rejected
ROM/receipt/part selections, programming and identity failures, and a save
retry. The first attempted U-Boot tenfoot run correctly stopped: U-Boot had
enabled the emulated xHCI controller and the native driver refused its active
PCI bus master. A second run confirmed shutdown and PCI command readback but
the host prompt parser reported a false failure; the corrected runner verifies
the emulated controller's identity, stops it, clears and reads back its PCI
command, and completes all cases. This is a QEMU-specific handoff, not physical
USB/DMA qualification, gameplay, or FPGA programming.

A Nano Go probe and entry image were cross-built with the unit tests' explicitly
synthetic schema-7 profile. Separate negative builds rejected a changed profile,
staging over Go RAM, and staging over a declared firmware reservation. Neither
the profile, UART selection, staging address, nor output is an assertion about
the physical board. The artifacts must not be installed or booted on a Nano.

An offline Quartus PFG experiment combined the retained vendor FPGA-first SOF
with the SPL from the separately retained Terasic U-Boot/BL31 build. PFG reports
the expected device and FPGA-first order and rejects a nonexistent SPL input.
This output has not been independently decoded to verify its embedded SPL,
does not contain the native entry or selected FIT, has no sealed endpoint, and
was never sent to hardware. The vendor U-Boot configuration reads its next
stage from raw MMC sector `0x200`; the off-board FIT cannot be assumed to be on
the board's existing card. A successful SOF conversion is not a matched boot
chain or recovery proof.

`evidence.json` records exact file hashes and the simulation cases. Detailed
receipts, build logs, console logs and diagnostic artifacts are retained under
`/home/deano/fes-retained/de25-native-entry-20261031/`; they include paths from
the original worktree and are diagnostic records, not signatures. The prior
vendor and recovery inventories are retained separately in
`/home/deano/fes-retained/de25-embedded-vendor-20261031/` and
`/home/deano/fes-retained/de25-top-recovery-inventory-20261031/`.

No kit was claimed or contacted in this work. Before a board attempt, establish
the exact SPL/FIT/FPGA combination and safe load path, the actual U-Boot and
secure-firmware DDR reservations, a separate staging window, UART/counter
readiness, secondary-CPU and DMA quiescence, and a restoration procedure for
TRWU8CBL. Same-SOF JTAG recovery was observed previously on the other board;
it does not prove factory-boot recovery for the top board.
