# DE25 ZX81 complete-frame ownership, 2026-10-29

The HPS-controlled ZX81 diagnostic now captures into two packed monochrome
M20K banks. A completed capture remains immutable until the pixel domain
switches banks at an output-frame boundary and acknowledges it. Startup and
transport-epoch reset hide unwritten RAM. A stopped reader causes whole source
frames to be skipped; a stopped source repeats the completed picture. HOLD
aborts partial capture while retaining the last completed picture.

Scope: misteross's `boards/de25nano/zx81-hps/` diagnostic and its tests.
Base: `b2070e3604f28d7515a74348a461c24404b3369c` (#1260).
RTL/client-tested source: `67068382b7a12047dad227077bd57c4b05e7fec3`.
Final compiler source: `ec14fda2964fd4fba1f5d3396bc70b754a0e1d29`.
[Source matching](client-source-match.json) confirms 83 captured files match
exactly; only the SDC file gained stronger datapath/skew constraints.
The shared DE10 renderer, generated protocols, runtime admission, boot image
and installed software are unchanged. This diagnostic retains its existing
internal 16 KiB RAM; it is not the separate reconfigurable RAM-only base.

## Digital evidence

[Video results](video.json) record 21,048,339 checked image pixels across three
independent clock ratios, with randomized unwritten RAM. Every displayed pixel
must match one completed patterned source frame. Checks cover the first/last
scaled pixels, aligned DE/sync, missing rows rendered as paper, malformed line
rejection, excess-row cropping, a partial final line, stopped source/reader
clocks, recovery, HOLD, and stopped-clock epoch reset with presentation held
through the next raster boundary. Writer/displayed-bank exclusion and
frame-boundary-only ownership transfer are checked throughout. After the
reader resumes, the stopped-reader case also verifies that the queued seed-5
frame became the displayed bank, rather than merely checking that it was not
overwritten while pending.
The video-only rerun binds source revision `7443fc4ee0bdba050f0278f4bdf171034da310bb`;
the compiler and client receipts remain bound to their earlier source revisions.

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

The final Quartus Pro 26.3 build succeeds with zero errors. The renderer is
one 22,140×8 dual-clock simple-dual-port memory fitted into **11 M20Ks**
(177,120 logical bits). [Timing summary](timing-summary.json) and
[104 paired rows](owner-paths.tsv) cover every metadata bit and synchronizer
and reset-release tail at all four published corners. Metadata data flight
is at most **3.907 ns**, below its 10 ns bound. Setup/hold, datapath delay,
maximum skew and net delay pass; CDC-50003 has zero violations/waivers.

The constrained global summary minima are 1.662 ns setup, displayed 0.000 ns
hold, 7.421 ns recovery, 0.066 ns removal and 0.220 ns pulse width. The displayed
zero is rounded, not a positive physical margin. **Overall Design Closure
still fails**: two inputs and 30 outputs lack external delays (9 input and
2,581 output path pairs). The inherited ROM-depth warning remains. Design
Assistant also retains two LNT-30023, two RDC-50003 and three RES-50101 findings,
all unwaived. Relative to the earlier HPS base, the extra synchronous polarity
finding concerns source VBLANK setting/clearing capture state; the extra reset
findings include the new ownership release chains and separate board pixel
reset. Their four-corner tail timing and stopped-clock behavior are covered,
but this does not establish whole-board reset/clock recovery or MTBF.
The full compressed reports are retained alongside the receipts.

The local `out/de25-zx81-frame-ownership-artifacts.tar.gz` archive contains
this JTAG SRAM diagnostic SOF and its compiler/client/timing receipts.
[Archive identity](artifact-archive.json) records its hash and size; raw vendor
and FPGA payloads are not committed. SOF SHA-256:
`fe91af9119df01b3173391f5c6017f86a0379ffefc078afb8b6f6024e9aa23fd`.

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

The next separate integration is to combine this mailbox/display machine with
the [empty/16 KiB PR RAM proof](https://github.com/DeanoC/fes/pull/1269).
That profile currently has no keyboard/tape mailbox or display, so its existing
RBFs are not a combined ZX81 image. A combined static base needs fresh compiler,
composition and client evidence; neither proof inherits the other's acceptance.
