# Correct Nano recovery trace console

The previous [bounded trial](../2026-11-07-de25-recovery-return-hardware/README.md)
verified its UART upgrade and five SD writes/readbacks. Its network diagnostic
was invalid: `serial0` aliases UART1 while the trace helper addressed UART0.
No network SD write or native activation occurred. That consumed attempt stays
terminal; the original intermittent network stall remains unexplained.

Implementation: `ca87b32780372567507f17d9b765087fecdde69d`.

The helper now addresses UART1 at `0x10c02100`, status `0x10c02114`. The builder
and explicit trace package checker resolve `/chosen/stdout-path` through its
alias independently in the actual compiled U-Boot and extracted FIT FDTs. Both
must select the enabled DW APB port with register shift two and four-byte access.
The route, DTB hashes and inspection-tool identity are bound into the receipt.

The regression fixture records the actual Nano alias rather than assuming its
name is a hardware index. Tests reject wrong/disabled ports, altered shift,
width, baud, aliases and either changed device tree. The actual helper C runs
against injected MMIO; these host tests do not execute the ARM artifact.

The fresh U-Boot build passes. Linked ARM inspection confirms UART1 addresses,
100,000 polls, 96-byte string limit, trace call sites and terminal WFE loops.
Both actual device trees select `/soc/serial1@10c02100`. Strict BL31 and SPL
remain byte-identical to the qualified original. The FIT is 954,288
bytes, SHA-256 `ccb6a597052c69f982713c327eeaab02adda4bd8ef21905deed2db7e7d47f353`. No FPGA/PFG build was needed.

The explicit validator accepts the new 150,994,944-byte regular-file image;
the ordinary uploader rejects it. The current trace checker rejects the consumed
v1 package after its profile changed. A file-only comparison from that v1 image
to the corrected prefix changes only the fixed raw FIT mirror. This comparison
is a preparation check, not a fresh observation of the physical card.

All 28 focused recovery tests and the selected affected checks pass, including
1,602 parent tests (three skips), planner/consistency and native integration
checks. Full logs and terminal receipts identify the actual commands and scope.

The user reported an accidental power cut and restoration after the previous
hardware attempt was terminal. Read-only host checks saw exact TRWU8CBL USB and
serial return. No new UART/JTAG command or card write was issued in this step.
CPU/FPGA/boot state and card contents after the cut remain unverified. The
continuing claim is retained; the factory card and bottom kit are untouched.

Next is an explicit UART-only transition from the exact last-verified v1 FIT,
manifest and prefix to this corrected package, preserving R1107A rollback in
place and using fresh repair backup names. The existing original-to-trace
updater rejects v1 and must not be repurposed by changing its baseline pin.
A fresh frozen operator, failure tests, bounded boot and live card proofs are
required before a repair write. No native/core-load, menu/video/input/network
or timing acceptance is claimed here.

Raw evidence: `out/nano-recovery-console-fix-20261107`.
Retention: `/home/deano/fes-retained/de25-recovery-console-fix-20261107-r1`, layered on the consumed physical archive.
