# Bounded native DDR read candidate: software passes, boot stops before firmware startup

The guarded read/drain path is implemented and passes software and actual RTL
checks. The first matched TRWU8CBL boot stops after the SPL handoff, before any
secure-provider marker or native command. No FPGA DDR read, active master stop
or hardware bridge drain was reached. This candidate does not qualify core
loading or runtime admission.

Implementation commit: `0b66f8de23f0239d18126ee655de3a046da92bb9`.
Final tested source: `97b52e38d8ee7bf09fcc343e162a70171f66162f`.
The final commit bounds trace numbers and repairs a test-selection assertion;
the complete affected checks and matched boot chain were rerun on that clean
head. The earlier failed software plan and diagnostic fixture attempts remain
retained under `out/nano-active-ddr-20261102`.

## Implemented path

`platform/embedded/tamago/run.py fpgaload --board de25-nano --ddr-read` selects
the fixed diagnostic explicitly. It requires the source-derived SD profile and
the existing sealed DDR project. The default DDR configuration still stops after
its contained identity observation.

The builder compares the entire generated F2SDRAM adapter with its sealed
vendor template and binds the resulting attributes, control map and reader
memory map into both native Go and the SPL/BL31 chain. The secure provider:

1. Checks the idle reader, inherited probe and held maintenance masters.
2. Samples both SMMU control banks and the DDR firewall, and verifies all 2048
   words of the fixed CPU pattern. It repeats the translation, firewall and
   pattern gate immediately before DDR release.
3. Opens DDR alone, offers one two-beat read at `0x95000000`, then issues
   DISABLE after the address is accepted while consumer delivery is held.
4. Checks 32 returned bytes, their XOR checksum, the final 16 data bytes and
   the matching generation/cookie acknowledgement. It accepts immediate or
   delayed response drain, independently stops all masters, then closes control
   and DDR through the existing mandatory hardware drain.

Ownership, generation, reset state, CPU parking and cache/translation state
remain checked throughout. Failed attempts cannot replay; memory remains pinned
until reboot. Recovery asserts control reset only, rather than inventing a DDR
drain. A successful closure must report hardware drain mask `8`, continuous
containment false, all producers quiescent, and optional FPGA warning ACK false.
No shared wire/schema changes or timing exceptions were introduced.

The address choice follows the documented
[F2SDRAM address view](https://docs.altera.com/r/docs/813752/26.1/hard-processor-system-component-reference-manual-agilextm-5-socs/fpga-to-sdram-subordinate?contentId=QrZ7xg9oEn_mu8lPK61t4w).
Checking both translation banks follows the independent secure/non-secure
[global bypass controls](https://docs.altera.com/r/docs/814346/26.1.1/hard-processor-system-technical-reference-manual-agilex-5-socs/bypass-mode?contentId=QIJ9GMpmZoB~yEbAc_oatQ).
Those documents and sampled registers do not establish physical access by
themselves.

## Off-board evidence

All ten [affected commands](software-result.json) pass on the final source:
1,266 parent tests with two skips, 1,577 FPGA tests with 75 skips, native
runtime/firmware/producer integrations, planner, whitespace and consumer checks.
[Generated consistency](make-check.log) covers 35 consumers and 37 fixtures.

The [actual C and RTL fixture](reader-rtl-result.json) runs 311 complete
master-stop cases and 933 read/drain cases, including failures at 278 read
positions. Its memory model uses independent literal data. The strong platform
fixture also covers already-closed preparation, successful active closure,
changed translation/firewall/pattern, lost CPU/master ownership, bad data, stale
ACK, missing response and raw close failure. Trace validation consumes the
actual C provider's modeled register trace; missing, repeated, reordered and
malformed records cannot grant completion. These fixtures have no hardware
authority.

The [matched chain](native-build-result.json) builds native Go, entry, BL31,
U-Boot/SPL and an HPS-first SOF. [Selection proof](selection-proof.json) verifies
the active build flags and strong symbols in the actual AArch64 firmware.
Its [read contract](ddr-read-contract.json) remains bound to package
`07b2d01003dc92e4993ba7f72761444ae801a1a005f87e0a1b65f9aad9dfeb7d`,
build `d2435ab5d8221892162a1044d81dfb76`, ABI `0x10203041`. The FPGA payload
is unchanged from the [contained prerequisite trial](../2026-11-02-de25-ddr-prerequisites/README.md).

## Bounded TRWU8CBL result

The trial ran from 08:01:06 to 08:03:59 UTC on 2026-11-02, with native boot ID
`330e25f17cd96c75`. Two fresh recovery boots uploaded and read back the exact
FIT and package over Ethernet, preserving the protected boot files and recovery
manifest. JTAG loaded the matched diagnostic at 6 MHz. Native SPL passed its
cold/CPU, DDR, sealed FIT and final SD handoff checks.

The [raw trace](native-console.log) ends at `FES_NANO_SPL HANDOFF`. The
50-second matched-prompt bound expired without secure-provider output or a
U-Boot prompt. No `fesboot` was sent and the read/drain recipe did not run.
The [experiment](experiment-result.json) remains failed. Its
[independent postcheck](hardware-failure-postcheck.json) reproduces the same
incomplete trace and verifies the upload evidence and UART/USB cleanup.

The experiment restored and read back the original 15 MHz setting before
restoring USB ownership/mode. Two subsequent read-only JTAG parameter queries
reported `No devices`, including with temporary top-node access restored. USB
still identifies TRWU8CBL; UART is free and permissions are back to root/0/0664.
Fresh JTAG availability remains unresolved. No further native attempt, factory
restore, recovered prompt or kit release is claimed. The failed diagnostic,
spare card and exclusive claim remain retained under the user's permission.

The [ELF comparison](firmware-layout.json) shows BL31 growing from 81,981 to
86,077 file bytes, with its loaded memory ending at `0x8001fc00`, below the
vendor DTB address constant `0x80020000`. The selected FIT has no explicit
`fdt-0` load address, and the pinned BL31 GIC setup says the DTB is unavailable;
that constant is not proof of an actual loaded reservation. The comparison
is structural evidence only; it does not
identify the startup failure. Next, preserve the vendor entry sequence and add
earlier bounded trace points around secure entry/cold-record capture, while
investigating JTAG discovery before another hardware run. Runtime admission and
physical launch/return remain open.

Full working artifacts are under `out/nano-ddr-cycle-20261102`; their retained
source/archive receipt is recorded separately after independent verification.
