# Repeatable Nano upload and actual cold serial handoff

Base: `448cffd8f61fc18de8b5248ef2d56cb35974f623`. This continues the
[Ethernet/SD diagnostic](../2026-10-31-de25-ethernet-sd/README.md). Everything
in this record is off-board: host execution, cross-builds, artifact inspection
and regular-file card packaging. No UART, kit lease, JTAG, physical card,
factory system or board network was touched in this change.

## Repeatable upload

The host uploader operates through an already running matching recovery
prompt. Its default is a file-only plan. Execution claims the exact top board,
checks the recovery/card/RAM identities and starts a bounded read-only TFTP
transfer for one file/client. The final acknowledgement and RAM SHA-256 must
pass before its sole write to `FES/UPLOAD.BIN`. Separate-buffer SD readback,
SHA-256, byte comparison and repeated protected-prefix/root-FIT checks must
pass before the receipt says verified. It never activates a candidate.

Nine host tests include actual localhost UDP transfers, option negotiation,
write/wrong-name rejection, final empty block, cancellation and lost final
acknowledgement. Injected console cases cover wrong media, prefix, live RAM
reservations, hashes, transfer failure, short/uncertain SD writes, readback,
boot-file changes and lease loss. Failed operations are not automatically
replayed. An uncertain prompt leaves the uploader's claim held.

The actual recovery kit v3 and final native FIT pass file-only planning. The
new uploader has not run on hardware. Its guards preserve recovery bytes in
successful tests, but FAT writes are not atomic and there is no power-loss
durability guarantee. The original factory card remains the rollback medium.

## Actual SPL to BL31 producer

The optional `owned-cold-serial` firmware now links a strong board provider,
not the default weak unready provider. Its matched SPL samples the SDM boot
kind, cause, peripheral/bridge resets and secondary CPU inventory before
initialization. The exact pinned vendor DDR source defines POR/cold/nCONFIG/
JTAG kinds 0/2/3/4; warm, RSU, unknown and mismatched histories reject.
The unchanged default controlled component remains unready.

Native SPL reads `FES/UPLOAD.BIN`, verifies its exact whole-FIT size/SHA-256
before parsing and serves subsequent parser reads from the retained RAM copy.
Its generated seal is installed after the FIT build and followed by an
SPL-only rebuild; the FIT must remain unchanged. The sealed FIT preloads BL31,
full U-Boot and the native entry. There is no post-containment SD fetch.

SD is the sole used device, synchronously in PIO. After verifying the native
entry, SPL masks SD interrupts, observes SDHCI idle/buffer state independently,
disables the card clock and checks readback. Functional resets precede OCP
resets, with barriers and readback. Reset bits do not substitute for drain.
Unused masters, secondary CPUs and bridges must stay held throughout.

SPL passes a copied record in loaded OCRAM data to early EL3 through x2/x3.
BL31 checks pointer bounds/alignment, cookie, checksum, exact source chain,
owner/epoch and current scope before consuming it once. The real platform
`owns`/`quiesce`/`quiescent` callbacks verify continued cold containment without
accessing the reset SD controller. Active/restarted masters or bridges reject.
The cold firmware exposes only read-only private Snapshot, denies bridge
mutations/configuration and reports zero configuration features.

Full U-Boot does not probe SD and has no USB/network/DMA or flash commands.
Before entry and after device removal it requires the same nonzero owner,
generation 1 and exact flags `0xdf`. Existing RAM, CPU, timer, watchdog, UART
and guarded EL2-to-EL1 entry checks remain. The prior failing legacy bridge
disable path is removed from this native profile; its historical diagnostics
are unchanged.

This is reviewed, source-bound diagnostic sequencing with an actual EL3
consumer, not authenticated/signed/measured boot or protection against hostile
non-secure/debug code. There is no active USB/Ethernet/FPGA stop provider,
native peripheral resume, new production target admission or physical launch.

## Validation and exact artifacts

- Strict-warning/UBSan execution passes 35 cold producer/consumer cases, 12
  actual early-capture/platform/SiP cases and 11 actual SPL seal cases. These
  cover ownership/replay, all boot kinds, changed scope, PIO/DMA, independent
  idle timeout, clock/reset failures, record mutation and verified-buffer bounds.
  The seal test substitutes the vendor hash API while checking every fixture
  byte; it does not revalidate the vendor's SHA-256 implementation.
- Eight native memory/export/private-Snapshot tests and nine uploader tests
  pass. Existing ownership, recovery and firmware regressions pass.
- The explicit controlled native/tenfoot integration passes with the race
  detector, actual transformed vendor/C service and Verilated FPGA endpoint.
  Ownership, reset, memory and SDM inputs are injected. It is separate evidence
  from the cold serial provider, which intentionally cannot program/open FPGA.
- `make check` passes: 31 generated consumers and 37 fixture copies match.
- All ten affected checks pass (`--base 448cffd8f --jobs 4`). The final
  uploader length checks and SPL alignment exclusion were added after that
  run started; focused uploader/native-export tests, matched cross-builds,
  actual vendor alignment-helper tests and package validation were completed
  afterwards. `evidence.json` records that exact scope.
- The pinned Agilex 5 BL31 and U-Boot/SPL cross-build. ELF inspection confirms
  strong provider/capture/ownership/stop symbols, not weak stubs. The 168-byte
  handoff record is in loaded OCRAM at `0x211f0`. The exact final FIT digest is
  present in final SPL binary; the actual exported capability query compiles
  and returns zero configuration features.
- Actual PFG and mtools assembly/readback prepare a combined recovery/native
  image. The root recovery FIT, manifest and protected 16 MiB prefix remain
  exact. No physical card write or independent embedded-SPL SOF decode occurred.

| Artifact | SHA-256 |
| --- | --- |
| Chain identity | `2f23ec149a2568ccec02e31c1b3e4e99cb4cde329f006f7ba4022d334a542457` |
| BL31 | `c20a099dd721a6b3d72148fe75dea7a7cc64b7920810b35c06c5510fd7e9efec` |
| Native FIT | `9926773577fcb139b4a379cc293646d1e178619e3a4c8190f843e17d172bbf26` |
| SPL hex | `36b2e1beeadb6ab2397edabb8ccf5cab798ea914672af1278a8b1b05aba0c016` |
| Native SOF | `a0fb02797b0c109115ba81352c151529ce3b2b47fedcbf9001db137d1d6b3f6c` |
| Combined SD image | `9e5e50c4a1c414cb82587a41deb5469b6e62e2f979be8a9182c7c866ddf10719` |
| Preserved prefix | `2586b521d2e8b69622c6c7d52156a540ef5a1c695ae410a38f2c1483054cad71` |

Initial cross-build attempts exposed pinned vendor defaults that reenabled
FAT/flash commands, unconditional RSU flash objects, storage environment macros
and SPL-specific integer/barrier definitions. The native export now removes
those consumers and adapts the shared header only inside its private copy.
The target's compulsory FPGA library remains; its commands and cold firmware
configuration access are blocked. Failed candidates and logs are retained.
A later rebuild added positive SDM boot history, then the experimental filename
and pre-parser FIT seal. The final source audit found vendor FAT DMA rounding
would request four bytes beyond the sealed file. Native FAT reads now use byte
granularity for verified RAM copies. Executing the real vendor alignment
helpers on every final FIT component reproduces the rejected old range and
passes all final ranges. The matched pair was rebuilt again. Four actual
packager rejection cases also fail before PFG/output creation for wrong seals,
unready firmware, entry identity and stale producer sources.
Older successful candidates are superseded and carry
no acceptance for the final hashes above. No timing/ownership/seal gate was waived.

The matched kit, build receipts, logs and source evidence are retained under
`/home/deano/fes-retained/de25-nano-cold-upload-20261031`.
[evidence.json](evidence.json) records exact inputs and validation outputs.
The [Nano guide](../../../platform/embedded/nano/README.md) has commands and the
next bounded board test: recovery upload/readback, volatile switch to the
matched native SOF, cold/SPL/EL3 proof, one `fesboot`, then factory restoration.
Only that test can establish whether the source-derived cold/reset/MMIO
assumptions hold on TRWU8CBL. Full native device and FPGA launch/return remain
subsequent work.
