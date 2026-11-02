# Native boot and fixed FPGA DDR read/drain pass with contained SPL logging

TRWU8CBL completes the matched native boot, retained SD package load, sealed
FPGA configuration, fixed 32-byte HPS DDR read, reader drain, complete master
stop and bridge reclosure. The [experiment](experiment-result.json),
[independent trace/cleanup audit](hardware-evidence.json) and
[strict hardware postcheck](hardware-postcheck.json) pass. This qualifies the
fixed read-only diagnostic contract. Runtime package admission, general DDR
use and emulation-core launch/return remain open.

Tested/compiled source: `6dd356157dac111852b0f943ca4c0c799adb5de1`.
Native boot ID: `4b94b8a323e5da52`.
Package: `07b2d01003dc92e4993ba7f72761444ae801a1a005f87e0a1b65f9aad9dfeb7d`.
FPGA build: `d2435ab5d8221892162a1044d81dfb76`; control ABI `0x10203041`.
The preceding [checkpoint trial](../2026-11-02-de25-spl-barrier/README.md)
and its stopped trace remain unchanged.

## Console repair and compiled checks

The SPL handoff and final ATF diagnostics now use a private UART1 sink after
device containment. It accesses only the already configured TX/status
registers, bounds each wait to 100,000 polls and formats at most 383 bytes in
a 384-byte buffer. It drains the transmitter after output. Invalid status,
timeout, bad formatting or an oversized message parks without retry. Its park
loop invokes no delay, scheduler or device callback. Earlier vendor console
startup and the final `DSB SY`, CPU/argument/digest checks remain in place.

[Compiled selection](selection-proof.json) checks the actual fixed UART1 map
against the pinned vendor device tree: base `0x10c02100`, register shift 2,
32-bit I/O and status at `0x10c02114`. All 17 functions reachable from the
contained logging helpers have no scheduler, timer, watchdog, serial-uclass or
indirect callback call. The formatter has one local switch jump: a checked
21 upper bound selects 22 signed table entries, whose complete destinations
are verified to remain aligned instructions inside that formatter. The first
verifier rejected this unresolved jump; its failure remains in the retained
session. Explicitly resolving every destination preserves the callback gate.

Actual linked barrier, guard-entry and raw-hash checkpoint order passes.
Six linked BL31 startup/setup/console/capture functions retain their earlier
instruction bytes and addresses, and pinned vendor assembly remains unchanged.
The [matched build](native-build-result.json) rebuilds BL31, SPL/U-Boot and
the HPS-first composition. [Input verification](native-input-verification.json)
checks all selected sources and the unchanged cached SDK/entry/package.
The FPGA RTL/package is unchanged; no new synthesis or timing signoff is claimed.

## Software checks

All six [selected software scopes](software-result.json) pass. The exact
parent selection contains 1,273 tests with two skips, run in three intact-module
partitions capped at 300 seconds each. [Coverage](parent-coverage.json) verifies
selection, uniqueness, child exits and log hashes. The parent scope uses that
adapter to its planned aggregate command; the other five retain their planned
commands. The wrapper and semantic report use separate paths. Generated checks
cover [35 consumers and 37 fixture copies](make-check.log).

The actual UART sink runs 15 injected cases covering normal output, CR/LF,
formatting, busy and invalid status, transmitter drain timeout, oversized and
empty messages, null input, and failure after partial output. Joined handoff
and loaded-byte guard tests use that same sink. Existing barrier/hash stops and
ownership/argument/digest rejection still fail closed. The
[strong C/MMIO and recursive decoder check](atf-pipeline-verification.json)
retains the checked descriptor throughout complete DDR decoding and failures;
its boot/checkpoint prefix is synthetic. The runner also passes
[44 orchestration cases](experiment-fixtures.json),
[16 readback cases](readback-fixtures.json), six transport/clock/coverage cases
and compilation/global-name checks without opening hardware.

## Bounded physical result

The single run lasts 12:03:55–12:07:22 UTC on 2026-11-02. One qualified recovery
boot uploads and independently reads back the new 6,499,492-byte native FIT,
then freshly reads/hashes the unchanged 1,645,568-byte package. Protected boot
files and the recovery manifest remain unchanged. The package is not rewritten;
native code issues no SD write. Both volatile programming operations complete.

The [47,273-byte trace](native-console.log) now contains all five SPL checkpoint
records, the actual loaded BL31 digest and state, both EL3 capture records,
accepted secure ownership, matched loader prompt and native EL2/EL1/Go entry.
The loaded BL31 SHA-256 is
`50323d4935036c83376460d79b1c299d9104d162d30b428db41d1429fd704595`.
Changing contained output restores boot in this run. The earlier stopped CPU's
program counter and a particular scheduler callback were never observed, so
this result does not identify that earlier fault instruction.

Native SD/FAT retains and contains the exact package in 58.475 seconds, within
the existing 100-second observation bound. The native CPU/secure-firmware path
configures 1,642,496 core bytes, observes four descriptor credits and the
bounded write/completion window, and matches the sealed live identity.
The observed configuration-marker interval is 13.292 seconds, including
9.419 seconds of reported hash work. The phase-two deadline stays 20 seconds.
These timings describe this diagnostic, not general package throughput.

Fresh SMMU/firewall checks and two complete checks of the 2,048-word pinned
pattern precede DDR-only release. One 32-byte/two-beat FPGA read targets
`0x95000000`; the 8 KiB window and 2 MiB reservation stay pinned until reboot.
The consumer remains blocked until reader DISABLE forces completion/drain.
Generation 1 and cookie 7 match, with checksum `0xf1ffefc0` and final words
`369ca8ab 59542ed2 fb0d9419 1dc51a40`.

| Closure observation | Value |
| --- | --- |
| Complete reader/probe/held-maintenance stop | accepted |
| Hardware DDR drain mask | `0x8` |
| Final bridge reset / ownership generation | `0xf` / `7` |
| Producers quiescent | `1` |
| Data continuously contained | `0`, truthfully recording the active read |
| Optional fabric-warning acknowledgement | `0`, remains unqualified |

The independent decoder rechecks every ordered record and returned value from
the saved bytes. It reports `ddr_read_closed`, no errors, 158 diagnostic reads,
nine writes and four polls. The writes are reader/control operations; this
does not qualify FPGA writes into HPS memory. Neither firewall coverage, the
fixed read nor the drain receipt grants arbitrary DDR or runtime permission.

## Cleanup and remaining work

The original 15 MHz rate is restored and [read back](clock-restored-readback.log)
while the private top-only JTAG service is live. That service then stops, exact
USB permissions are restored, and the independent audit checks a free serial
port and continuing exclusive claim. The successful diagnostic and spare card
remain on the top kit under the user's retention policy. No factory restore or
claim release is claimed, and the test is not repeated.

This supplies the missing native fixed-read/drain evidence. The next work is a
trusted runtime loading contract for an actual emulation core, including its
real memory/media requirements and load/stop/return failures. The diagnostic
profile continues to deny ordinary runtime admission. Full tenfoot and normal
DE10-equivalent core operation remain the objective.
