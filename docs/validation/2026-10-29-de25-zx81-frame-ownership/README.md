# DE25 ZX81 complete-frame ownership, 2026-10-29

The HPS-controlled ZX81 diagnostic now captures into two packed monochrome
M20K banks. A completed capture remains immutable until the pixel domain
switches banks at an output-frame boundary and acknowledges it. Startup and
transport-epoch reset hide unwritten RAM. A stopped reader causes whole source
frames to be skipped; a stopped source repeats the completed picture. HOLD
aborts partial capture while retaining the last completed picture.

Scope: misteross's `boards/de25nano/zx81-hps/` diagnostic and its tests.
Base: `b2070e3604f28d7515a74348a461c24404b3369c` (#1260).
The retained client proof used source `67068382b7a12047dad227077bd57c4b05e7fec3`
and compiler source `ec14fda2964fd4fba1f5d3396bc70b754a0e1d29`; its
[source match](client-source-match.json) is limited to those revisions.
The current-head Quartus build and video simulation use
`ae8b394b61d815ab69eb090f5158e8fc2a0951c1`. Their fresh
[compiler inputs](compiler-inputs.json) include the updated `video.v`,
`fes_de25_computer.v` and `clocks.sdc` bytes.
The shared DE10 renderer, generated protocols, runtime admission, boot image
and installed software are unchanged. This diagnostic retains its existing
internal 16 KiB RAM; it is not the separate reconfigurable RAM-only base.

## Digital evidence

[Video results](video.json) record 21,567,202 checked image pixels across three
independent clock ratios, with randomized unwritten RAM. Every displayed pixel
must match one completed patterned source frame. Checks cover the first/last
scaled pixels, aligned DE/sync, missing rows rendered as paper, malformed line
rejection, excess-row cropping, a partial final line, stopped source/reader
clocks, recovery, HOLD, and reset asserted/released while both clocks stop.
Writer/displayed-bank exclusion and frame-boundary-only ownership transfer
are checked throughout.

[Client proof](client-proof.json) and [inputs](client-inputs.json) bind all 20
native/static-AArch64 cases to the actual endpoint, mailbox, machine, firmware,
renderer and C++ `De25Computer` sources. Each session observes 226 complete
720p frames and issues 609 atomic GPO writes. Native and QEMU Cortex-A76 results
agree. Both full sessions exercise real BASIC, complete HID state, a chunked
910-byte tape image, all actual loader commits, stopped-pixel CPU/control
progress, live eject and HOLD/RUN. Fault cases retain the unchanged client
bounds and refuse uncertain retries. The selected simulation build ID is not
a compiled SOF identity.

The standalone video simulation uses behavioral memory and digital clocks.
It does not model analogue metastability, physical M20K collision behavior,
PLL recovery, HPS bridge transactions or external HDMI electrical timing.

## Compiler and software evidence

[Software checks](software/checks.json) report 32 focused tests passing and
all DE25 regressions passing. The full affected run completed 967 parent tests
(one skipped) successfully, but its 1,456-test FPGA lane had one GPU-lease
fixture failure (35 skipped). That unchanged fixture observed a held lock with
no owner metadata. Its isolated retry, unchanged-parent control and full
17-test file retry pass. [Issue #1271](https://github.com/DeanoC/fes/issues/1271)
tracks it; the failed aggregate log is retained, rather than called a pass.
The aggregate overlapped a timing-only constraint commit; final-head CI is
reported separately in the PR.

The [first routed candidate](failed-first-constraints/README.md) passed owner
setup/hold timing but had an unwaived CDC-50003 metadata constraint finding.
The final constraints explicitly add datapath-only delay and maximum skew,
without changing RTL, clocks, seed, timeouts or adding waivers. Its completed
compiler reports and remaining coverage limits must be read together;
SOF generation alone is not timing or hardware acceptance.

The current-head Quartus Pro 26.3 build succeeds with zero errors. The renderer is
one 22,140×8 dual-clock simple-dual-port memory fitted into **11 M20Ks**
(177,120 logical bits). [Timing summary](timing-summary.json) and
[104 paired rows](owner-paths.tsv) cover every metadata bit and synchronizer
and reset-release tail at all four published corners. Metadata data flight
is at most **4.231 ns**, below its 10 ns bound. Setup/hold, datapath delay,
maximum skew and net delay pass; CDC-50003 has zero violations/waivers.

The constrained global summary minima are 1.763 ns setup, displayed 0.000 ns
hold, 7.427 ns recovery, 0.026 ns removal and 0.220 ns pulse width. The displayed
zero is rounded, not a positive physical margin. **Overall Design Closure
still fails**: two inputs and 30 outputs lack external delays (9 input and
2,494 output path pairs). The inherited ROM-depth warning remains. Design
Assistant also retains two LNT-30023, two RDC-50003 and three RES-50101 findings,
all unwaived. Relative to the earlier HPS base, the extra synchronous polarity
finding concerns source VBLANK setting/clearing capture state; the extra reset
findings include the new ownership release chains and separate board pixel
reset. Their four-corner tail timing and stopped-clock behavior are covered,
but this does not establish whole-board reset/clock recovery or MTBF.
The full compressed reports are retained alongside the receipts.

The local `out/de25-zx81-frame-ownership-artifacts.tar.gz` archive contains
the current-head JTAG SRAM diagnostic SOF and compiler/timing receipts. The
client proof inside the historical receipts remains bound to its recorded
older source.
[Archive identity](artifact-archive.json) records its hash and size; raw vendor
and FPGA payloads are not committed. SOF SHA-256:
`e89f5a8f9543d69794e581ac476433bb68dcd201cc083a893ef9366c5892f320`.

## First-board acceptance

Use the existing [headless Linux first-boot collector](../../../docs/de25-linux.md#first-boot-evidence-collection)
with the verified image, explicit board identity and independent SSH host-key
check. Record serial boot, memory discovery, Ethernet/DHCP, SSH and the manual
reboot comparison before admitting a physical control backend.

This SOF still needs its own SPL/handoff compatibility, bounded MMIO/device
binding and JTAG SRAM acceptance. On that exact artifact, check cold-start
blanking followed by a stable BASIC picture, keyboard/tape LOAD, HOLD/input
neutrality, resume and reconfiguration without a stale frame. Physical clock
loss/recovery and reset tests require a controlled board diagnostic; simulated
clock stoppage does not establish them. No image, QSPI, boot configuration,
kit or harness is changed by these build/proof commands.

The [combined ZX81/RAM computer evidence](../2026-10-29-de25-zx81-pr-computer/README.md)
now records a separate current-head base and both RAM personas. The standalone
frame and RAM profiles do not inherit one another's client or hardware
acceptance; the combined artifact still needs its own board checks.
