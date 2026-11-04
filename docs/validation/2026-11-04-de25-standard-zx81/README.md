# Standard ZX81 core preparation for DE25

The installed DE25 ZX81 package still uses a reduced bring-up wrapper. This
work extracts the standard machine/control core from the DE10 board wrapper;
it does not yet replace the DE25 package or establish hardware acceptance.

## Implemented

- Shared `fes_zx81_core` retains standard mailbox, keyboard, cassette/SAVE,
  machine video capture and PCM sample logic. DE10 board primitives and the
  original registered expansion placement remain in the board wrapper.
- Session-display identity can be advertised independently of the DE10
  reserved DDR window. This is identity plumbing, not DE25 allocation support.
- Shared display-plane parameters permit a sixteen-beat reader with read-ahead
  without changing existing default integrations.

## Validation

- At `2ebf10a145a4bbe0ade56c9ef85b68ead02b6f4f`, focused parent regressions,
  generated consistency, ZX81 producer/source-closure tests and the actual
  `sim-fes-zx81` target passed. The bounded job completed in 1287.976 seconds.
  Other producer closure rows were unchanged; an all-core campaign is not claimed.
- At `1b8b4891c`, five actual RTL identity configurations passed, including
  the shared standard core with reserved-window support disabled. Existing
  Apple II mailbox and standard ZX81 session regressions also passed.
- At `1a05aca27b0d34bf391ec3d452b6ab203c6cd27a`, the registered
  `sim-fes-zx81-session` target passed all three modes in 200.549 seconds:
  legacy 52.224 MHz, standard 100 MHz, and standard 100 MHz with the bounded
  sixteen-beat reader. Tests cover complete frames, keyboard input, cassette
  CRC insert/eject, RAM preservation, drain/reopen and modeled display faults.

The standard session harness uses internal 16K RAM and a modeled DE10 memory
window. It does not qualify the physical DE25 allocated-memory path or the
standard expansion socket. No new RBF was compiled or deployed in this phase.

## Next integration

Bind the shared core and display plane to the DE25 kernel-owned allocation,
with explicit memory capability negotiation and verified stop/drain/reopen.
Port board clocks and audio using Terasic settings, preserve standard ROM and
expansion behavior, then build with Quartus and run the resulting package
through the normal FES catalog/runtime on the board. Timing, hardware and
full normal-kit recovery acceptance remain open.

## Shared runtime transport

At `5bd5a61d77721f3a7d6ea5c04fe377139d6733a2`, the standard `FesGpCoreDriver`
can consume the DE25 atomic-register bridge through the existing MMIO adapter.
The DE10 default retains its existing bus-settling writes. DE25 validates bridge
identity/version, ready/fault/idle state, request/ACK agreement and retained
words, then submits exactly one full word. Uncertain completion remains poisoned
across session boundaries and realignment; known acknowledged rejections leave
the link usable.

The independent computer model exercises the actual shared feature driver:
selected identity, keyboard, running-state cassette insert/eject, SAVE capture,
dirty-save refusal and explicit acknowledgement. Twelve bridge fault cases
check refusal and no replay. The first development test supplied reserved HID
bits; that failed fixture is retained privately and the corrected usage passes.

The complete canonical affected plan passed in 475.202 seconds, including
1,158 parent tests (five skips), host Go/UI and runtime tests. The same committed
FesGp test binary was compiled using the authenticated managed DE25 SDK and
passed under Cortex-A76 AArch64 QEMU in a separate 26.334-second build/run.
Exact identities and command results are in `standard-driver-transport.json`.
This is software/model evidence. Production construction still uses the old
bring-up driver, and the board was not reprogrammed or restarted. Standard core
RTL, the allocated session memory adapter, clocks/audio, production profile and
exact-package hardware acceptance remain required for the original normal-kit goal.

## Combined standard component and receipt refresh

The standard board wrapper connects the actual shared ZX81 core, atomic control
bridge and allocated session display. Separate system/pixel reset releases remain
board inputs, and the reserved DE10 window is disabled. At `83e89341b`, the
committed full-wrapper regression passed in 207.599 seconds, including the older
real-ROM BASIC/endpoint/board regressions. The combined diagnostic preserves a
RAM marker and continuing CPU writes across three allocation/display cycles.
Its internal16K diagnostic ROM does not establish ROM/RAM-pack composition.

At `f77738c50`, the captured standard Qsys component passed actual Pro26.3
validation and Verilog generation in 18.922 seconds; its current model/producer
checks passed in 48.867 seconds. Component sources and M20K adaptations are
recorded, but the canonical complete-board producer still selects the bring-up
path. This is not a full-board fit, timing result or deployed package.

The broad affected run at `cfbe09131` passed its parent and four preceding
native/Coleco RTL stages, then failed one source-bound composition receipt check
among 1,797 misteross tests (76 skips). No all-plan pass is claimed. The transport
extraction also required updating the proof observers to the same sticky fault
word at its new hierarchy. A fresh 14-case native/Cortex-A76 combined client/RTL
proof passed at `2c8395296` in 730.866 seconds. The branch-local fixture is copied
from that actual proof only after checking every current compiler/runtime source
hash and committed flag. Earlier dated receipts and failures remain unchanged.
The proof covers its real-ROM BASIC SAVE/load/run/reload and modeled persona
selection/failure cases; physical partial reconfiguration remains unqualified.
Six remaining shared-consumer RTL targets are still running separately.

No FPGA or kit software was installed in this phase. The complete board recipe,
allocated session runtime/profile, vendor clock/audio and standard ROM/expansion
integration, exact Quartus artifact and normal-kit hardware acceptance remain open.

## Stock audio clock and standard PCM wrapper

At `451f853c7`, `fes_de25_audio` uses Terasic HDMI_ASx4's dedicated `av_pll`
on CLOCK1_50 with the standard coherent PCM/I2S modules. The supplied archive
hash is verified. All259 PLL parameters remain unchanged except the target-device
field after the two device-only metadata substitutions. Pro26.3 generation retains
the stock integer M=58/C=236 ratio from50MHz: nominal12.288MHz, with the supplied
actual divider rate about12.2881356MHz (about48000.53 samples/second).

The isolated elaboration initially flagged the missing board Reset Release IP.
Adding the actual Terasic reset IP and using NINIT for PLL reset/hold resolved
that finding. Generation plus analysis/elaboration passed in32.824seconds with
zero errors and warnings. No reset/timing exception was added. The standard PCM
model passed in3.074seconds, covering atomic stereo, independent clocks, I2S
padding, hold and clock loss. `audio-integration.json` binds source and vendor
identities. This is not a full-board fit/timing result or a native audio capture.
The later whole-board integration below selects and connects the wrapper.

The corrected standalone diagnostic fault observer also passes eight selected
native/AArch64 identity/signature/reset cases in9.542seconds. The full20-case
standalone matrix is not claimed. After this and the14-case combined proof,
the owned temporary SDK volume was removed; authenticated SDK archives/cache,
source snapshots, binaries, logs and proof receipts remain available.

## Direct shared-display consumers

At `8025906de`, the direct C64 session-display target, pc386 presenter/capture/
plane benches, MENU simulation and Spectrum board simulation all passed in
122.775seconds. Their exact commands/results are in `shared-display-consumers.json`.
These complement the earlier ZX81/Coleco and allocated DE25 results. The larger
follow-up completed C64, Coleco, MENU, pc386 and Spectrum. Its one-hour cap
interrupted ZX81's final bounded-reader session case; that case passed separately
in 74.113 seconds. The aggregate remains terminated. Exact source hashes and
results are in `shared-target-completion.json`; no all-plan pass is claimed.

## Whole-board standard-core integration

At `a762078e1`, the existing ZX81-HPS builder's `--standard-core` selection
generates the complete managed GHRD with the shared standard machine, allocated
session display and stock audio PLL. The internal-16K base matches the standard
DE10 configuration without an expansion socket. Quartus Pro 26.3 generation
passed in 106.077 seconds and whole-board analysis/elaboration in 55.488 seconds.
There were no errors or critical warnings. Ordinary vendor/parser/unused-port
warnings remain recorded; this is no timing exception or hardware signoff.

The actual generated handoff checks both computer/scanout slaves, 100/74.25 MHz
clock/reset routes, sixteen-read credits, response FIFO depth, the same eight
managed initiators and sole read-only path into HPS memory. The eleven shared
FIFO/reset files retain their exact derivation; the new parent has a separate
reviewed hash. The three shared headers are emitted beside the RTL, display
control uses SystemVerilog, and the existing guarded Agilex/M20K RAM branch is
selected. Explicit ROM padding preserves the original contents and resolves
the initialization-depth warning. `whole-board-integration.json` binds this
result to the source, inputs, generated handoff and log hashes.

The 96 focused builder/reset tests passed; the firmware guard adds an independent
MIF/HEX BASIC byte comparison. The canonical changed-source software run at
`84b0d297b` finished in 997.608 seconds. The 1,158 parent cases passed with five
skips; the 1,800 FPGA software cases had one failure and 76 skips. That failure
is the branch-local combined native/AArch64 receipt checking older compiler
helper hashes. It needs an actual proof refresh after the producer inputs
stabilize; an earlier receipt is not current-source proof. The aggregate software
run remains failed and does not establish a merge gate pass.

The generation/elaboration milestone above did not establish fitting, sealing,
a new runtime profile or native kit acceptance. Production loading and the
physical kit remain on the existing working baseline.

## Routed standard-core candidate

The complete generated board at source `2ce959057` completed synthesis, fitting
and Timing Analyzer with Quartus Pro26.3 on fes-build-3090. Fitting and analysis
took 306.128 seconds. All90 reported numerical timing checks passed, including
setup/hold, reset recovery/removal and the bounded payload/skew paths. Worst
system setup slack is1.602ns and pixel setup slack3.349ns. The fitted design uses
8,758/46,800 ALMs and301/358 RAM blocks. Registered readiness/fault/drain launches
remove combinational logic before synchronizers. The two generated tiny FIFO
leaves use MLAB only for depth<=4; large response queues retain M20K.

The raw85-rule timing signoff report has two failing categories:32 outputs
without output-delay constraints and the two previously reviewed HDMI asynchronous
inputs. All CDC, reset-crossing and resource rules report zero violations. No
compiler finding was waived. The output list includes HDMI_SCLK/LRCLK: internal
and explicit forwarded divider clocks do not remove the tool's demand for
external output-delay declarations. A separate normal Timing Analyzer run on
an isolated copy of the same routed candidate, with those forwarded clocks,
completed in10.882 seconds and retained the same32/2 raw findings. An earlier
custom report-panel export crashed inside Quartus; its trace is retained and
was not used as signoff evidence.

This is routed diagnostic evidence, not a sealed package or physical acceptance.
The standard build action remains refused until the exact board-reference/output
checks and canonical package recipe are integrated. No synthetic external delay
has been assigned, and no new image has been loaded on the kit.

## Allocated session runtime adapter

At `ec3549d5b`, the existing LinuxDe25Menu adapter and shared lifecycle pass the
focused display checks on native and actual managed-SDK AArch64 binaries under
Cortex-A76 QEMU. The bounded job completed in76.764 seconds, including compilation
and generated consistency. The structured record is
[session-allocation-runtime.json](session-allocation-runtime.json).

GP controls Configure/Enable/Quiesce; only the kernel grant publishes frames.
Close must prove kernel drain, the end of peer references and hidden-plane
quiescence before reopening. Each reopen gets a new cleared allocation, starts
with back slot1 and a physical frame counter of0, and preserves the logical
completed-frame count. Tests exercise three odd-length display cycles, stale
sequence refusal, late publication without replay, pinned peers and uncertain
close. The shared lifecycle negotiates per-frame mappings for session displays
and handles fresh-slot ownership separately from the retained DE10 memory path.

The first broader AArch64 run failed because QEMU's filesystem prefix redirected
ancestor directories into the SDK while newly created files remained on the
host. A library-only prefix fixed the `/tmp` open but still redirected root-FD
walks. Using the target dynamic loader directly removes that prefix ambiguity;
the broader diagnostic executable then passed. Those initial failures remain
in the private logs. The committed focused run above uses the corrected loader
invocation and does not claim a full current-source runtime suite or hardware
acceptance. Standard production profile/package selection remains unfinished.

## Standard production profile and agent integration

The existing production factory now constructs the shared FesGpCoreDriver for
`de25-linux-computer-v1`, with the actual GP display driver and ABI3 allocation
adapter. It admits the exact format2 standard ZX81 contract: fixed video, HID,
controllers, fixed PCM, cassette/SAVE, session display, shared blend plane and
allocated scanout1.1. No Cyclone V DDR window or Linux transmitter recipe is
used. The prior BRAM profile remains explicit, with no automatic fallback.

At source `fd79133d0`, six native test executables passed in57.949 seconds with
generated consistency. The six actual managed-SDK AArch64 executables passed
under Cortex-A76 QEMU in61.119 seconds. They cover exact profile refusal,
read-only bridge readiness and sticky faults, selected allocated overlay,
build-bound session allocation, production construction, MENU→standard ZX81,
three complete open/present/close cycles and Stop back to MENU. The running
machine is not held during visibility changes. Session control goes through
the real GP display driver and atomic bridge; kernel operations and machine/
pixel-control behavior remain independent injected models.

The first lifecycle candidate failed first-open because launch already owned a
cold allocation. The adapter now reuses that unexposed allocation for first-open;
subsequent reopen still requires successful retirement and gets fresh storage.
The next candidate reached Stop and exposed missing GP display commands in the
test model; the test now covers those actual driver commands too. Failed runs
and the deliberately stopped superseded target build are retained privately.

Shared schema/emitter and FogCast package-store, media protocol and runtime
client tests passed in30.316 seconds at source `775481d01`; those Go inputs are
unchanged at `fd79133d0`. The agent consumes a real production C++ serializer
fixture. Native and AArch64 outputs match its bytes exactly, including mapping
agreement, observed identity, allocated-session status and the full16K tape
limit. See [production-runtime.json](production-runtime.json).

This completes software profile/factory wiring, not canonical artifact or board
acceptance. The sealed standard package producer, output/reference timing checks,
independent artifact rebuild and managed hardware tests remain open. The physical
kit still runs its previous verified bring-up package and software baseline.
