# Early secure capture trace: software passes, native boot still stops after SPL

The matched TRWU8CBL test verifies both network uploads and reaches the SPL
handoff, then stops at the unchanged 50-second prompt bound. Neither new EL3
capture marker appears. No native command, FPGA DDR request, active drain or
runtime core launch occurs. The [experiment](experiment-result.json) remains
failed; the [independent audit](hardware-evidence.json) verifies its retained
trace and cleanup, rather than promoting the boot result.

Tested source: `01a80fd996b747447210d6d1de43183d4e65cb4a`.
Native boot ID: `cef122831c4e8d13`.
The preceding [DDR read candidate](../2026-11-02-de25-ddr-read-candidate/README.md)
and its failed trace remain unchanged.

## Firmware and trace change

The cold-record copy now follows the vendor console registration and scope
setup. Paired `FES_NANO_EL3_CAPTURE phase=before` and `phase=after` messages
surround it, forwarding the original pointer and 64-bit cookie. Stream-ID
setup, provider validation, BL33 and bridge authority retain their existing
order and gates. The messages identify progress only.

Current cold builds explicitly select `secure_capture_trace=true`. Their
decoder requires the ordered pair with aligned bounded pointer, exact cookie
and matching arguments. Missing or malformed observations cannot grant loader
readiness. Archived raw captures use an explicit historical decoder format;
missing current markers never select that format automatically.

[Selection proof](selection-proof.json) checks the real linked strong DDR
symbols and both format strings in the ELF and binary. All 32 selected vendor
assembly sources/includes match the pinned TF-A source. The vendor entry
object, including both executable entry sections, is byte-identical to the
previous candidate before relocation. Removing only the reviewed additions
recovers the exact pinned vendor setup. Linked layout still changes: the new
BL31 binary is 90,173 bytes, with its load segment covering
`0x80000000..0x80020c00`. This comparison establishes neither the stop location
nor a memory-overlap cause.

The native SDK and entry are reused only after checking every selected source
and receipt against the current tree. The [matched build](native-build-result.json)
rebuilds BL31, SPL/U-Boot and the HPS-first composition. The sealed FPGA package
and read contract remain unchanged: package
`07b2d01003dc92e4993ba7f72761444ae801a1a005f87e0a1b65f9aad9dfeb7d`,
build `d2435ab5d8221892162a1044d81dfb76`, ABI `0x10203041`.

## Software result

All ten [affected commands](software-result.json) have recorded successful exits
on the tested clean source: 1,268 parent tests with two skips, 1,577 FPGA tests
with 75 skips, native launch/return and failure paths, producer and firmware/RTL
integrations, planner, whitespace and consistency. [Generated checks](make-check.log)
cover 35 consumers and 37 fixture copies. Emitted-C checks cover vendor setup
order and live argument forwarding; trace tests cover current and archived
formats. [Helper checks](helper-verification.json) cover owned private-service
cleanup, exact clock-response parsing, compilation and undefined globals.

The [first plan](software-first-result.json) contains a 300-second parent
limit and a tenfoot model request deadline failure during concurrent builds.
The parent and integration subsequently pass without that build. The
[serial plan](software-serial-incomplete.json) records nine successful commands;
its [overall 900-second limit](affected-serial-result.json) expires before it
records the final command's exit, although that log reports all tests passing.
Only that final command is rerun separately, retaining its 840-second limit,
and [completes successfully](affected-final-result.json). No test or hardware
bound is relaxed; all interrupted and failed logs remain in the retained session.

## Private JTAG observation and bounded trial

A [read-only filtered observation](jtag-readonly-observation.json) finds only
TRWU8CBL and FPGA ID `4362C0DD` using an owned foreground JTAG service and a
private client configuration. The test then uses the same top-only service
pattern, without the idle-stop option, for its entire bounded lifetime.
[Enumeration](private-top-enumeration.log) and the
[transport record](jtag-transport.json) identify the private connection.
No shared daemon or client configuration is modified. This separates usable
transport in this run from the earlier discovery failures; their original
cause remains unproven.

The physical test runs from 09:16:04 to 09:19:36 UTC on 2026-11-02. Two fresh
recovery boots upload and independently read back the exact 6,499,492-byte FIT
and 1,645,568-byte package through Ethernet, preserving protected boot files
and recovery manifest. All three volatile programming operations complete.
Native SPL passes cold CPU, DDR, sealed FIT and final SD containment checks.
The [1,602-byte raw trace](native-console.log) ends at
`FES_NANO_SPL HANDOFF`, with neither capture marker nor a loader prompt.
No `fesboot` is sent and no native SD write or DDR read/drain is reached.

The private service remains live while the original 15 MHz rate is restored
and [read back](clock-restored-readback.log). It then stops, original USB
permissions are restored and the audit independently verifies a free serial
port and continuing claim. The failed diagnostic and spare card remain on the
exclusive top kit under the user's retention policy. No factory restoration,
recovered prompt, claim release or physical core acceptance is claimed.

Moving capture after console setup did not restore boot. The next diagnostic
must observe the earlier EL3 entry/console interval while retaining vendor
startup and strict ownership gates. The present trace supplies no program
counter or exact fault location. Active DDR qualification, trusted runtime
admission and real launch/return remain separate open gates.
