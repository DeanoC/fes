# Standard ZX81 core preparation for DE25

The standard ZX81 machine/control core now has a timing-admitted DE25 package
and native FES UI, cassette SAVE/reload, runtime recovery and active-core reboot
evidence. The post-reboot media picker still fails, so full normal-kit acceptance
remains open. The dated sections below retain each earlier artifact and its limits.

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

## Canonical standard-core candidate

The canonical DE25 ZX81 producer now selects the standard machine and all nine
interfaces of `de25-linux-computer-v1`. It captures the complete board, machine,
reader and audio closure and binds the functional BUILD_ID before generation.
The generated endpoint/topology and staged-source checks run before and after
compilation. Linux core-file conversion uses the same HPS-first vendor boot data.

The first committed attempt stopped before compilation because its broad source
record exceeded the format's64KiB limit. Source roots now retain the relevant
board/core modules and actual shared libraries; every captured hardware input
must belong to the closure. The final record contains474 inputs in52,000 bytes.
The limit and exporter validation remain unchanged.

Source `ea874ec412d44b9887da134667a166382840ba0a` completed a clean full canonical
compile and Linux phase conversion on fes-build-3090 in654.917 seconds. Its
1,740,800-byte core RBF has SHA256
`7d17a234408fbfe4984dc1caf88029c99b2b4dc0ffc11a5eba671055fd34c2aa`
and BUILD_ID `26921a8281abaf2f9b96ac388a5a63e5`. All92 reported numerical timing
checks pass. System/pixel setup slack is1.286ns/3.194ns; resources are8,794 ALMs
and301 RAM blocks. See [canonical-candidate.json](canonical-candidate.json).

Raw signoff still reports32 missing-output-delay findings and two missing-input
findings, with zero waivers and no other failed rule. Compile-only supplies no
reviewed-finding substitutions and exports no package. A separate clean build
completed in643.527 seconds with identical source/tool/execution records and RBF
bytes; its92 numerical timing checks also pass. See
[canonical-repeat.json](canonical-repeat.json). This candidate has not been
programmed or accepted on a board. The HDMI/audio-clock review, independent final
package rebuild, source proof receipt refresh and managed native acceptance
remain open.

## Audio counter phase correction

The frequency-only generated clocks incorrectly aligned SCLK and LRCLK rising
edges. The common serializer changes LRCLK on a falling SCLK edge. Explicit
source edges `{3 7 11}` and `{255 511 767}` now model that relationship without
changing the divider rates or RTL. The real Verilated serializer is compared
against the selected SDC edge lists over1,024 source cycles, including repeated
periods and both LRCLK transitions. Run the committed check with:

```sh
python3 -m unittest discover -s sources/misteross/boards/de25nano/zx81-hps -p test_audio_clocks.py
```

The actual RTL check and27 existing board-builder tests pass. A private copy of
the earlier routed candidate accepts the corrected constraints and reports
SCLK period325.516ns/waveform81.379–244.137ns and LRCLK period20,833.024ns/
waveform10,335.133–20,751.645ns. Both forwarded output clocks retain the same
waveforms. All four fitted operating conditions have an explicit min/max path
from each divider register to its output pin; their reports remain retained.
See [audio-clock-phase.json](audio-clock-phase.json).

The [ADV7513 hardware guide](https://www.analog.com/media/en/technical-documentation/user-guides/adv7513_hardware_user_guide.pdf),
pages12–13, specifies2ns setup/hold for LRCLK relative to the selected SCLK edge.
The stock transmitter writes select rising-edge capture. This confirms the
required relationship; the pin-path probe alone does not include the complete
clock-network or PCB skew and therefore does not prove receiver setup/hold.
Raw signoff remains32 outputs/two inputs with zero waivers. The existing exact
30-output reference policy is unchanged; the two audio-output findings remain
open. The corrected source requires a fresh complete build before any artifact
claim, and no board programming or package acceptance occurred in this phase.

## Corrected canonical build and current client proof

Source `446bd3eca9c8dacdeb7c071a331aab3842bb4a13` completed the full canonical
compile-only build in652.811seconds. All92 numerical timing checks pass with the
corrected I2S counter phases. The1,740,800-byte RBF SHA256 is
`0bd85c0b67bfcaedc9d4e7965b0e4d8e970b685c2f2d65ffe24b104c4ba90bff`,
BUILD_ID `a43e6fcf2904ca4890b22c32ab5fb4cb`. Raw signoff retains32 missing-output
and two missing-input findings with zero waivers. No sealed package or physical
acceptance is claimed. See [canonical-phase-candidate.json](canonical-phase-candidate.json).
The first attempt stopped before synthesis because a local clone recorded a
filesystem origin rather than the required repository URL. A fresh private
clone with the actual GitHub URL resolved that setup error; validation was unchanged.

The new diagnostic `scripts/review_de25_audio_clocks.py` observes an immutable
candidate through a private copy, checks the staged source hashes, and binds the
original compiled database, core RBF, inputs, tool and reports before/after STA.
It compares all five audio-related fitted pins and voltage settings against both
locked Terasic examples, and the complete audio PLL bytes against the stock PLL
with only the two device metadata substitutions. The existing complete HDMI
reference check binds transmitter settings and derived controller files.

The collector passed on that exact446 build in9.581seconds, covering all five
clock definitions and32 min/max routes across all four fitted operating
conditions. The clock-network plus output-route observations leave at least
159.193ns setup and159.497ns hold budget after the receiver's2ns requirements,
**before unmeasured PCB differential delay, clock jitter/uncertainty and signal
integrity**. These are conditional engineering budgets, not independent board
setup/hold signoff. See [audio-clock-fitted-review.json](audio-clock-fitted-review.json).
No output delay, false path, waiver or package admission was added. The existing
30-output reference policy still blocks the two additional audio-clock findings.

To repeat the observation on a complete compile-only candidate:

```sh
cd sources/misteross
python3 scripts/review_de25_audio_clocks.py \
  --candidate /absolute/path/to/candidate \
  --output /absolute/path/to/new-review-directory \
  --quartus-sta /data/altera_pro/26.3/quartus/bin/quartus_sta \
  --vendor-archive /absolute/path/to/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip
```

Six malformed-report tests reject missing corners/paths, duplicate observations,
wrong phase/frequency/forwarded clocks, invalid numbers and inverted min/max bounds.
This tool deliberately cannot clear a signoff finding or export/program a core.

The actual combined native/Cortex-A76 client/RTL proof also passed all14 cases
in588.594seconds at446. Both architectures completed real-ROM BASIC
SAVE/load/run/edit/reload plus modeled RAM-pack selection and six failure cases.
The source-bound fixture was copied byte-for-byte only after checking every
current compiler/runtime hash, the committed-source flags and all14 scenario
results. Its previously stale gate now passes. See
[combined-client-proof-current.json](combined-client-proof-current.json).
This is behavioral memory and selection-mux persona evidence, not physical PR,
standard package deployment or managed board acceptance. The initial empty SDK
volume setup failed under the image's default user; a fresh volume was populated
as root, then the compiler ran as the host UID. Both traces remain retained.

## Standard core with actual RAM personas

At source `0673c3d598141d427060e3986b83082707b4d9f4`, the standard-core expansion
diagnostic, existing50 board tests and current source-fixture gate pass in
9.394seconds. Generated consistency also passes41 consumers/42 fixtures/0copied
source pins. The new diagnostic uses the actual `fes_de25_zx81_standard`, atomic
transport, standard display component, existing PR controller and actual
empty/16K RAM personas. It does not use the reduced computer endpoint.

Both randomized initial-memory seeds17 and53 show vacant1K mirroring at4000/
4400, distinct5A/A5 bytes with the16K persona, and correct empty/RAM/empty
reselection. Six live-write freeze phases retain zero post-Frozen pack writes
and a stable static response. A poisoned atomic bridge also holds the machine
and fences RAM while the independent PR controller remains Running. The cold,
unallocated display issues no DMA. See
[standard-expansion-diagnostic.json](standard-expansion-diagnostic.json).

Run `make -C sources/misteross/boards/de25nano/zx81-hps standard-expansion-sim
OUTPUT=/new/private/path`. The standard `sim.py` dispatcher also includes this
case. Complete captured/derived sources, binary, tool and log evidence remains
in its retained proof directory. The first bench attempt used word indices
against byte-addressed MMIO and was rejected; that failed trace is preserved.
The corrected bench uses the generated bridge/controller offsets.

The17-byte diagnostic ROM does not establish BASIC boot or the native client
path for standard personas. Memory primitives and selection are behavioral;
the harness reads the real private machine-reset signal for its modeled static
fence. Production Qsys must export and qualify that reset boundary explicitly
before this becomes a physical PR implementation. No canonical package,
production defaults, timing-policy scope or board configuration changed. The
full normal-kit goal, pending audio reference decision and native acceptance
remain open.

## Managed software candidate prepared off board

The normal immutable software producer built source
`e26c65606430d253cc01fdc6aa494afa914a0546` against the retained, reviewed6de Linux
base in54.446seconds. It authenticates the original managed-factory SDK
`ad6479d06d506ed7406a289da1daa2712582ccc351e94e4db43498b01f0968e1`, freezes the
source, builds the complete AArch64 agent/runtime/launchers, checks the base ELF
and versioned-library closure and required MENU interfaces, and independently
inspects the finished bundle. No OS/kernel or FPGA build ran in this step.

Candidate bundleID is
`7b82f9033e63ba9dd000010cef9e77033286182b00f0ac3fa5267c4be8ab4645`, archiveSHA256
`aa0012724f7570d508140c88ba6019aef06a0d7ddd65d78752473e2853248d02`.
The matching Linux amd64 host API binary was built from the same source and
retained with SHA256
`b4f40409d0d11c2a23ffafbbf16cdfee51018489959b44b0d5a2be6cc4ba15c3`.
See [managed-software-candidate.json](managed-software-candidate.json).

A separate4.478second probe verifies every packaged file hash and all four
AArch64 executable headers. Using the exact factory SDK libraries, the actual
packaged runtime reaches its early argument-validation path under Cortex-A76
QEMU and returns the expected invalid-request exit2, before constructing any
hardware or opening diagnostics. The host help parser likewise exits before
opening a service or configuration. See
[packaged-software-smoke.json](packaged-software-smoke.json). The first probe
incorrectly expected a VCS field in the host's Go metadata; the normal build
uses `buildvcs=false`. The corrected probe binds the clean source-pinned build
record and logged linker arguments; the host recipe and binary were unchanged.

These are build, compatibility and early-entry checks. They do not establish
native daemon startup, pairing/session continuity, standard-core admission or
hardware behavior. The software bundle and host remain retained off board;
no running service, pairing database, software generation, kernel/card/FPGA or
board baseline changed. The pending audio-reference decision, final sealed
standard core and exact-artifact managed acceptance remain required.


## Native lost-process recovery

Runtime source `7688e73016b74f457fa3ca14ed2cbd46f80c6425`, immutable AArch64
bundle `33d40e58e3548c73c3a8b265189a845ac01c7b0c08a8a366001d22a1d7a03dbc`,
and the original sealed standard core `5a42fa706` run on the matched ABI3 Linux
image `6de6a392`. All 482 sealed FPGA source inputs remain unchanged. The normal
host code is unchanged from source `314ed9d9e`; no kernel, image, card, flash, JTAG or
FPGA producer change forms part of these tests.

The complete unmodified affected plan passes all 8 commands in 641.854 seconds;
the actual driver/manager/allocation/factory focused suites pass in 58.576 seconds.
Nine publication-name cases and eight actual DE25 command-transport retained-SAVE
cases cover the two repairs. Before-repair failures remain retained: enumeration
rejected the agent's normal package-ID-plus-token directory, while fresh-start
Identify incorrectly required an empty cassette in a retained image. The new
check verifies live identity and uses the existing SAVE snapshot guard without
adopting a driver session, resetting/ejecting media or acknowledging dirty SAVE.

| Injected exit | Native evidence |
| --- | --- |
| Idle runtime | Entire recorder passes in 65.780s; replacement group and 10s stable readiness observed within 30.236s, including that stability interval. |
| Runtime with ZX81/tape active | Entire recorder passes in 85.767s; replacement group and 10s stable readiness observed within 32.039s, including that interval. |
| Agent with ZX81/tape active | Original single-crash recorder fails in 19.262s on the expected HTTP connection reset. Read-only observation confirms the new group at MENU; a separate 47.352s proof checks the same replacement,10s stability and fresh UI. No second crash occurs; the failed recorder remains failed. |

Every recovery stays in boot `bac7afb2-34d3-4997-9213-46f3dc0e6c7d` and selected
software generation 25. New executable/PID/start identities, staged MENU inode,
exact MENU payload/build, healthy allocation and host reconciliation are checked.
Fresh catalog sessions load/run7, open the real media picker, return machine
focus, edit/run9 and Stop back to FES Home. All 14 captures are visually checked.
Keyboard input uses the explicit bus 0 evdev fixture; physical USB is untested.
An old session's Stop is never sent to the replacement runtime.

The bottom board returns to its named baseline bundle `a14a` at generation 26,
same boot/card/kernel/image, ready/idle/lease-free, with its original host/unit
and fresh catalog restored. Owned fixtures are removed and both status locks
protect independent verification/release. The top board is untouched. No branch
baseline is promoted. See [runtime-restart-native.json](runtime-restart-native.json)
for exact hashes, separate recorder outcomes and retained evidence paths.

This record accepts the stated lost-process cases on those exact artifacts.
It does not accept uncertain hardware, dirty SAVE loss, guest-session resumption,
physical USB/audio, native ROM/persona composition or physical PR. Current-head
CI and independent review remain required; full normal-kit acceptance is open.

## Combined client receipt after runtime recovery changes

The actual combined client/RTL proof at committed source
`04ba8455010e57bb6b8abfe26122bec65c7f3e38` passes all 14 native and emulated
Cortex-A76 AArch64 cases in 1047.993 seconds. The real ROM emits a 923-byte SAVE;
both clients load/run/reload it and print 7, exercise empty/RAM/empty selection,
and contain wrong identities, callback failure, ambiguous writes, missing
responses and late reset. This uses the diagnostic selection mux and behavioral
memory; it does not establish physical partial reconfiguration or kit-CPU execution.

The branch-local combined proof fixture is copied byte-for-byte from that run
after checking every compiler/runtime hash against the current committed bytes.
The intervening generated-parent pin fix changes none of those captured inputs.
This refresh repairs the stale receipt exposed by CI after runtime header changes;
it grants no timing or hardware exception. The new standard-core package still
requires complete timing checks, an independent matching rebuild and exact-artifact
board acceptance.

## Audio source review for the public reset export

The complete fit at `b6559f38ee86f6fdd2a13dac2f979b9b4e2c97cf` reaches RBF
generation, then correctly refuses the previous audio-source pins. The three
changed reviewed files add only the full reset output, its system-reset conduit
metadata and an unused top-level connection. Reversing those exact changes
reproduces all three previously reviewed files byte-for-byte; the other five
reviewed audio sources remain identical.

A separate read-only fitted review passes in 8.930 seconds, checking 32 paths
across four corners, the unchanged stock PLL and all five fitted pin/voltage
settings. Its conditional receiver budgets still exclude unmodeled PCB skew
and jitter. [audio-reset-export-review.json](audio-reset-export-review.json)
records the source comparison and fitted observations. The three literal source
pins are updated after this review; the eligible internal16K profile, two approved
clock ports, warning inventory and all timing/admission gates remain unchanged.
The failed candidate stays unadmitted. A fresh complete producer run and matching
independent rebuild are required for the changed producer inputs; physical board
acceptance remains open.


## Reset-export package and native trial

Committed producer `a35f3207a668e87ac16a4edb6028ef554a2f8140` completes the normal
Quartus Pro 26.3 package exporter and an independent fit in 1193.332 seconds.
Both fits pass all 92 reported numerical checks and have zero unreviewed failed
rules. Their actual RBF files are identical: SHA256
`02c2d5e4a87912e2b4b2316212a17e85a688782cf333ab5980fd171725c7bd8e`.
Package `138ab5cbda9b923b19a8d8f61c0dbc22670a2b6cd7d4272015f5973b009374bc`
contains BUILD_ID `e1644cc46c58daaab1e5bdb151d78e1a`; all 482 selected source inputs
match the committed producer. Fresh per-fit reviews apply only the approved
two asynchronous inputs and internal16K `HDMI_SCLK` / `HDMI_LRCLK` stock-reference
exceptions. Raw warnings remain retained. The complete unmodified seven-command
affected plan passes in 1611.057 seconds.

The normal immutable AArch64 bundle
`bcecd49fdcc9d0ce8b39755543a13c9f60f9a5da5aae484a58665708ac17979d`
is built from the same source and installed through managed activation on bottom
board TRWU8J8G, matched ABI3 image `6de6a392`, software generation 27. The host
remains the normal source `314ed9d9e` binary.

| Native case | Result |
| --- | --- |
| Actual FES picker, insert/eject, BASIC load/run/edit/reload and ROM SAVE | PASS, 88.603s; all 13 pictures checked, capture and acknowledge HTTP 200. |
| Newly emitted SAVE `9838526c` selected through the real picker | PASS, 48.808s; LOAD/RUN9, edit/RUN7, reload/RUN9 and Stop/Home; all seven pictures checked. |
| One exact selected active-runtime exit | PASS, 83.376s; replacement group stable within 31.844s including 10s stability, fresh ZX81/UI/input and Stop/Home; all five pictures checked. The lost guest is discarded. |
| One ordinary OS reboot while ZX81 prints7 | PASS, 71.296s overall; new boot in 47.946s, same image/card/kernel/bundle, authenticated new boot and 10s Ready/MENU/idle/free. No power cycle or old-session Stop. |
| Fresh post-reboot launch and retained SAVE picker | FAIL, 23.972s; fresh K prompt renders, then picker opening refuses the session allocation. No picker appears. |

The failed case reports `DE25 session reopen requires a successfully retired
allocation`, followed by `DE25 retired session counter was not cleared or
publication was unconfirmed`; the raw observation reports zero underflows.
This follow-on status does not prove which invariant failed first. No second
reboot or repeated failed trial occurs. The owned cleanup Stop returns MENU.
The full trace, both failure images, raw observation and cleanup receipt remain
retained; the failed case remains failed. The next investigation must capture
the first display-close/present failure before another controlled trial.

Normal activation restores the named baseline bundle `a14a` at generation 28.
Independent authenticated checks under both status locks verify new boot
`5c6295d4-704a-432f-9a98-a07a4c479729`, unchanged image/card/kernel, ready/idle/free
and no maintenance or software fault. The original host/unit/fresh catalog are
restored with SQLite integrity checked; the candidate catalog and all trial
evidence are retained. Owned fixtures are removed and the board is released.
The first restore wrapper failed on JSON nesting before any activation; its
corrected, separate invocation passes in 29.784s. The top board is untouched.

[reset-export-native.json](reset-export-native.json) binds exact artifact hashes,
source receipts, visual observations, the failed case and restoration. Keyboard
input uses the explicit bus 0 fixture. Physical USB/audio, native ROM/persona
composition, physical standard PR and current-head CI/independent review remain
open. This partial trial does not promote a main baseline or establish full
normal-kit acceptance.


## First-failure reporting for the post-reboot investigation

The retained source-a35 trace shows only a reopen refusal and its subsequent
quiesce error; it does not establish the earliest failed invariant. The existing
runtime log now records failed show/close requests before later status updates,
and NativeHardware records a failed session present before cleanup adds a
second error. Reopen refusals include the grant/bootstrap/quiescence/fault flags;
retirement refusals include observed, completed, submitted and logical-base
counters. No command, gate, timeout, allocation or recovery behavior changes.

A regression fails before the change in 14.449s because the first close error
is absent from the event stream after a subsequent failed show. After the
change, the complete shared-runtime suite (56 reported cases), native-hardware
suite and DE25 allocation suite pass in 127.833s, including the original
failure-before-cleanup and exact-counter checks. All 482 admitted FPGA
producer inputs remain byte-identical to source a35. The new reporting still
requires a controlled native trial; neither this test nor a passing trial
without reproduction would establish that the post-reboot defect is fixed.


## Controlled trial of first-failure reporting

Runtime source `5109333149cbb74d5ae4534e02a70866da94a058` builds the normal immutable
AArch64 bundle `4ecf08d8e0ff607b880c38da29271e8465f97ae3a1e3716336c3744c89962c3b`
in 54.402s. All six archive entries, manifest identity, source trees, executable
hashes and exact provenance verify independently. All 482 admitted source-a35
FPGA inputs remain identical; this trial uses the same standard package138ab5cb
and changes no FPGA producer, image, kernel or card. The complete unmodified
eight-command affected plan passes in 1079.611s with the checkout frozen at510
throughout. The earlier source-a35 delta selected seven commands; these are
separate complete plans, not a filtered subset.

Normal managed activation passes in 37.131s, selecting bundle4ec at generation29.
The first runtime session uses the real picker for SAVE9838526c, executes9, edits
and executes7, reloads and executes9, and returns Home: PASS55.171s, all seven
images checked. A separate real-UI Settings/F12/Esc trial retires and reopens
the allocation three times, with distinct display generations4/5/6, logical
completions1/2/3, machine focus after each close and zero underflows. All ten
pictures are checked. Its recorder exits0 in 59.028s, but its new keyboard
sequence omits the line-entry settle wait: the BASIC images show edited
listings `1 PRINT 9` and `1 PRINT 7`, not executed output. Its visual receipt
therefore remains partial (`passed: false`) and supplies no RUN proof.
TV surround returns On; only the newly created hash-checked preferences file
is removed to restore its authenticated original absence.

With the established settled typing sequence, one ordinary OS reboot from ZX81
showing7 passes in 71.241s overall (47.692s for reboot observation). The new boot
`e8d10b71-cb1e-4061-bc93-81e4dc51754b` is authenticated with unchanged kernel,
card and image, selected bundle4ec generation29, and ten seconds of stable
ready/MENU/idle/free. No repeated reboot, old-session Stop or power pull occurs.
Its first fresh session repeats the original early-picker timing and passes
SAVE selection, executed9/edit7/reload9 and Stop/Home in 48.503s; all seven
pictures are checked. The original display fault does not recur, so no new
first-failure logging path is physically exercised. Reporting-only changes
establish neither the root cause nor a repair; the earlier failed trace remains
failed and normal-kit acceptance stays open.

Baseline restoration passes in 29.474s, selecting the named bundlea14a at
generation30. The restore executes with the separate authenticated e8d10 SSH
helper. Its copied descriptive job scope still says5c62; a separate metadata
erratum retains that discrepancy, while the executed script/result bind e8d10.
The original host/unit/fresh catalog return with SQLite integrity checked,
preferences are absent as before, and owned keyboard/status fixtures are
removed. Independent authenticated observation under both status locks verifies
the exact baseline, same image/kernel/card, ready/idle/free and no maintenance
or software fault before release. The top board is untouched and no main
baseline is promoted.

[first-display-fault-native.json](first-display-fault-native.json) binds the
source receipts, retained-file hashes, partial visual result, restore erratum
and release identity. Physical USB/audio, native ROM/persona composition,
physical standard PR, current-head CI/independent review and the intermittent
display failure remain open.


## Standard expansion through actual runtime clients

At `7be4e896379f3ff995ac264dfbc253f0baf55a48`, the existing standard expansion
bench uses `FesGpCoreDriver` and `De25PrControl` in place of hand-coded GP/PR
commands. The Avalon adapter only drives the model pins. Both native x86 and
an ELF64 AArch64 executable under Cortex-A76 QEMU pass seeds 17 and 53. The ARM
build uses the authenticated SDK from the recorded managed FES image, and runs
the same retained standard RTL and actual runtime clients. This executes the
compiled RTL model on an emulated CPU; neither board was accessed.

The tests retain empty/16K/empty memory distinction and six live-write freeze
phases, and check that GP HOLD and PR freeze independently hold the machine.
A wrong-persona probe enters sticky Fault, while a callback failure after actual
RAM selection leaves Frozen. Both keep the standard machine held; a second
request through the poisoned PR client performs no writes or callback. An
initial bench assertion wrongly required Frozen for both cases. The first ARM
attempt also aborted before the scenarios because of Verilator's default
host-sized thread pool. Both traces remain; an explicit one-worker limit passes
the complete cases on native and emulated ARM.

[standard-expansion-runtime-clients.json](standard-expansion-runtime-clients.json)
binds sources, tools, binaries, logs and the retained jobs. This uses the existing
17-byte diagnostic ROM, behavioral memory and a selection mux. It does not prove
BASIC with the standard personas, target-side composition, native-kit CPU
execution or physical PR. No RBF or package was rebuilt or admitted, and the
stock-clock timing exception retains its internal16K scope. The previous
intermittent display retirement fault and all remaining normal-kit gates stay
open.

The complete unmodified seven-command affected plan passes in 1112.772 seconds
at `87e8eb79c09fff01827020d1190a71af76d429e6`, held unchanged throughout that run.
The subsequent `7be4e896` delta only sets the bench to one worker and documents
that limit; its full native and emulated ARM integration cases pass separately.
Documentation links and support-matrix checks pass. These are software/model
checks, with no new board or package acceptance.


## Standard PR project generation

The explicit `zx81-standard` PR profile now prepares the normal standard machine
and managed Linux DMA/display board with its socket enabled. Actual Pro26.3
generation passes all shared PR-control and standard board verifiers at
`1812fcbd652b05d7e00e60731c070117bc9cf978` in 111.902 seconds. A separate normal
internal16K generation passes in 111.727 seconds; all three HPS/EMIF IP files
are byte-identical. The five tool-added HPS defaults are admitted only with
those exact values in this PR profile. Original-parameter changes retain the
existing closed managed-Linux policy.

The generated PR parent has a separate reviewed source pin. Its changes add
control ports, wires, the PR-control instance and its interconnect route; all
ten bridge/FIFO sources retain their exact original pins. The production static
bus module also runs in the standard real-client simulation, which passes both
seeds. The existing bring-up combined proof was rerun after the Linux split conversion
and selected-log fixes at `2d94bbfb2`: all14 native and emulated AArch64 cases pass in 654.339
seconds, including both real-ROM BASIC round trips. Its exact report refreshes
the source-bound branch fixture after checking all65 compiler and59 runtime/test
inputs; it
does not prove standard-core BASIC with PR personas.

[standard-pr-generation.json](standard-pr-generation.json) binds the control,
source review and preserved failures. The first base synthesis stopped on a
missing root bus-definition header. `bcc21b08e` stages the original header
explicitly; fresh synthesis retains all 20 persona-response and 46 static-request
FFs with zero boundary inversions. The first routed base (`3503229c7`) passes
49 constrained internal summary rows and 968 paired boundary/PCM path checks
across four corners, but the unchanged qualification gate rejects the
combinational execution/fault mute before its audio synchronizer (CDC-50011).
`fb82b4155` registers that mute in its existing system domain, removing the
CDC-50011 finding. Its original audit rejects Quartus's retained physical fault
replica. The corrected audit checks every connected replica and requires both
logical causes in every corner and check; a read-only test against the completed
database passes, preserving all 26 reports/root-QDB files. A fresh ordinary
`c4cc43fd7` base then passes in 632.928 seconds, including 49 constrained internal
summary rows and 984 paired path checks across all four corners. The two failed
builds remain failed. All six raw finding groups remain visible: overall Design
Closure still fails, including 33 missing output delays for the live PCM profile.
The full base/empty/16K build compiles all three revisions, preserving 100% of
the root logic for both personas. Each revision passes its 49 constrained
internal summary rows, memory inventory and fitted-region checks. Packaging
then rejects the HPS-only output of a direct SOF-to-RBF conversion. A separate
probe through the shared canonical Linux split converter produces a fabric RBF
with matching POF0 identity and passing integrity, leaving the failed project
unchanged. The profile now selects that shared converter. The fresh full build
below closes packaging; independent rebuild, whole-board timing and hardware
acceptance remain required. The default internal16K
wrapper and clock exceptions are unchanged. The production bus model also
passes both diagnostic-ROM seeds under AArch64 QEMU in 23.175 seconds.
A private copy of the completed `c4cc43fd7` build passes the full strict
collector at `34e8bb3b7` in 52.956 seconds. Compiler and verifier revisions are
recorded separately; the original failed candidate remains unchanged. The
resulting real diagnostic base/personas pass deterministic composition on the
host and under AArch64 QEMU in 1.817 seconds, with eight positive runs and
12 rejection controls.

The same static AArch64 composer then runs on the bottom DE25 CPU. Both empty
and16K personas compose twice, with byte-identical output to host/QEMU. Four
invalid selections are rejected without output. Both downloaded bundles pass
independent host inspection. The native trial finishes in 4.426 seconds after
two retained wrapper preflight failures before upload: the stock rootfs has no
`timeout` utility. The corrected wrapper retains host/supervisor deadlines.
Temporary files are removed, the same baseline image/bundle/boot is verified,
and the kit returns idle and lease-free. No FPGA load or service change occurs.

This proves native CPU composition of diagnostic base/persona bundles. It does
not prove ROM composition, physical PR, a Linux opener, standard-persona BASIC
or package admission. The internal16K clock exception does not apply to this
profile.


The fresh ordinary `34e8bb3b7` full build compiles all three revisions and
completes the shared Linux split conversion, then fails after 1339.822 seconds
while hashing an obsolete `base-convert.log`. The earlier private c4 clone
retained that old log, so its changed-verifier pass did not expose this clean-build
failure. The fix returns the real log names from the selected converter and
records the shared `linux-enrich.log` and `linux-split.log`; the direct converter
retains its own log. No placeholder log or timing/metadata relaxation is added.
The full strict collector passes in 51.802 seconds at `2d94bbfb2` on a private
copy of that fresh34 compiled project, with the legacy log absent and both real
shared-converter log hashes recorded. All141 original protected files remain
unchanged. An initial wrapper supplied the work root instead of the generated
project; it exits before collector writes, and its failure remains retained.
The corrected run uses the explicit project path. Compiler34 and verifier2d
remain separate evidence. The subsequent ordinary `2d94bbfb2` full build passes
in 1358.299 seconds with compiler and verifier at the same source. All three
revisions pass their49 constrained internal rows, memory/region checks, vendor
metadata/integrity and the paired boundary/PCM audits. Both personas preserve
100% of the static root. The receipt binds the actual shared conversion logs.
Overall Design Closure remains failed with all six raw finding groups retained;
this does not admit a package, establish whole-board timing, replace an
independent rebuild or prove hardware acceptance.

The complete unmodified seven-command affected plan passes at `d88ac9af4` in
941.180 seconds, including1,171 parent and1,821 FPGA software tests. That source
remains distinct from the later2d log-selection fix. Its actual14-case
compatibility proof passes in654.339 seconds; after the exact report refresh,
the complete unmodified d88..94 affected plan passes in954.643 seconds with
1,171 parent and1,821 FPGA software tests. No hardware is loaded by these checks.


## Real-ROM standard RAM-persona model

The existing standard expansion model adds explicit `--basic` selection. It
uses the shared real-ROM BASIC script through actual `FesGpCoreDriver` keyboard,
SAVE capture/acknowledgement and media-unit insertion, plus `De25PrControl`.
Only observation code reads guest state. The actual public SAVE capture is
written and synced before acknowledgement, then staged from its absolute path
through `ComputerMediaSnapshot` and unit0's normal CRC/512-byte protocol.

A working-copy diagnostic passes seeds17 and53 in640.100 seconds. Each boots
1K/16K/1K (RAMTOP17408/32768/17408), types and saves1 PRINT7, loads/runs twice,
observes7 twice and confirms that reload removes a later edit. Each retains the
same923-byte SAVE (SHA256 `4137c58140dcd2895961f86d29fda6e7dbce0ea863da1d73f94cea2ba2f1fecc`).
Loader tags must reach actual persona RAM writes with matching address/data,
and the real raster must complete. The original default17-byte diagnostic and
freeze/failure cases remain unchanged in purpose.

Two earlier adapter failures remain retained: the first called the legacy
upload API; the second supplied a relative media path and was rejected before
media mutation. Neither establishes a production fault. All98 selected FPGA
compiler inputs remain byte-identical to the completed2d full build. That working-copy result remains a development record; committed results follow
below. The common BASIC helper gained client callbacks, so its compatibility
matrix also requires an actual refresh. No physical PR, kernel opener, package
admission or hardware acceptance is supplied by this model.


At committed source `8e926fe92`, the full native standard BASIC model passes
both seeds in643.101 seconds. Its original default diagnostic mode also passes
both seeds in6.029 seconds. The common-helper compatibility matrix passes all14
native/emulated-AArch64 cases in764.811 seconds; the source-bound fixture copies
that actual report after checking all65 compiler and59 runtime/test inputs.
The independent AArch64 whole-standard-model batch reaches its compute deadline
after4203.356 seconds. Seed17 completes the full test; seed53 reaches SAVE and
its firstRUN. The original failed job is preserved. A separate bounded run
executes only the missing seed53 using the same immutable static ARM binary,
source/model/SDK and QEMU, with native SAVE/result comparison. It completes in
2242.548 seconds: both seeds now have actual SAVE, two loads, two RUNs printing7,
24 editor returns, reload removal of the edit and 668 complete picture frames.
Their 923-byte SAVE files match native execution exactly. The original timed-out
batch remains failed; the aggregate records the two completed bounded attempts.
See [standard-real-basic-aarch64.json](standard-real-basic-aarch64.json).
No physical PR or hardware acceptance is inferred.


The complete unmodified seven-command affected plan for that committed BASIC
change and its actual refreshed compatibility fixture passes in1024.795 seconds
at `0b990554f`, frozen through the run. The native and compatibility results stay
separate from the subsequently completed whole-standard-model AArch64 proof. See
[standard-real-basic-canonical.json](standard-real-basic-canonical.json).

## Early Bluetooth firmware preparation

Passive authenticated checks on the bottom board find USB radio`0bda:a728`
and an early built-in-driver firmware failure before the managed root mounts.
BlueZ is running but exposes zero adapters. The final managed root contains the
correct firmware; the small bootstrap does not. No pairing, driver reset,
service restart or FPGA/image change was performed during these observations.

Source `c2920cdf2` adds the existing firmware, licence and checksum manifest to
the DE25 bootstrap only, binding every input to its content identity and reading
it back after filesystem construction. Real pinned-tool/card regressions and
DE10 controls pass in17.556 seconds; the full six-command canonical affected
plan passes in154.677 seconds. The existing bootstrap component builder produces
two identical32MiB files, SHA256
`fa236fa35525a5781dc592f17973d47b98f3ca9e9a5d7d12ec3e69ea8cd7ae8b`.
The rebuilt boot-selector executable, kernel and factory manifest are unchanged.
The full-factory command correctly refuses the retained old factory against the
current image recipe; that failure remains retained. The component build
instead authenticates the retained factory against its actual producer and
constructs only the bootstrap. It grants no new factory or native acceptance.

[bluetooth-bootstrap-preparation.json](bluetooth-bootstrap-preparation.json)
records the exact artifact and checks. It is not installed: a separate explicit
boot-change approval is pending, followed by native adapter and physical-input
checks. The working board baseline is unchanged.

## Fitted live-audio output observations

A read-only private copy of the completed2d project yields144 fitted min/max
clock-network and output paths: all three revisions, four operating conditions,
and SCLK/LRCLK/I2S. The original compiled project remains byte-identical, and all
98 selected compiler inputs still match `c2920cdf2`. No SDC, clock, delay,
exception or admission policy changes. The existing internal16K clock approval,
reconfirmed by Deano on2026-11-05, retains that exact internal16K scope.

The conditional normal-operation budgets, including the receiver's2ns setup
and hold requirements, are at least158.715ns setup and159.249ns hold. These raw
fitted phase/path calculations exclude PCB differential delay, additional clock
jitter/uncertainty, signal integrity and PLL/reset transients. They do not
establish board timing or extend an exception to the live-PCM PR profile. All raw
Design Closure failures, independent rebuild and exact-package native gates
remain open. See
[standard-pr-audio-output-observations.json](standard-pr-audio-output-observations.json).


## Generated EMIF clock ownership

At source `413eba238`, the explicit standard PR profile retires the one exact
manual `EMIF_REF_CLOCK` declaration already ignored by Quartus. The generated
memory IP owns the existing6.000ns reference; both PR SDC and fitted boundary
audits still require it. The shared managed-constraint verifier binds the full
derivation and its closed receipt to the locked original. The default
internal16K derivation stays unchanged. No clock or timing exception is added.
The focused source/negative tests pass13 cases. The initial source-fixture gate
correctly rejects the stale compatibility receipt; the actual14-case native and
emulated-AArch64 rerun passes in668.244 seconds. Its report is copied verbatim
only after all65 compiler and59 runtime/test hashes match. See
[standard-pr-emif-compatibility.json](standard-pr-emif-compatibility.json).
The subsequent complete three-revision build and remaining acceptance boundaries
are recorded below. The compatibility matrix alone grants no timing or hardware
acceptance.

The unchanged shared PCM/I2S RTL also passes an external serial-decoder stress
matrix in5.777 seconds:36 clock-ratio/phase combinations,3,024 decoded frames,
1,980 active frames,900 held frames,34,318 data transitions on falling SCLK and
72 lock losses. Invalid-pair and silent-source controls are rejected. The initial
monitor expected active audio before the request/ACK transfer and first complete
serialized frame; its corrected bounded warmup is a monitor change, not an RTL
repair. See
[standard-pr-audio-cdc-phase-matrix.json](standard-pr-audio-cdc-phase-matrix.json).
This digital model evidence does not qualify metastability, board timing, the
CDC warning or package/hardware admission.


## Complete generated-clock build and target CPU composition

The fresh ordinary `413eba238` build passes compiler, packaging and constrained
internal checks in1356.097 seconds. Base, empty and16K revisions each pass49
numerical timing rows; both personas preserve100% of root logic. Both duplicate
EMIF clock findings disappear in all three revisions. All four remaining raw
finding groups stay visible:33 output-delay findings,2 input-delay findings,
one PCM topology finding and one shared reset/enable finding. Overall Design
Closure remains failed, and the artifacts remain diagnostic. The complete
unmodified seven-command affected plan passes in976.685 seconds at`3ccb0a5ed`.
[standard-pr-generated-clock-full-build.json](standard-pr-generated-clock-full-build.json)
binds these actual results; no independent rebuild or hardware admission is
claimed.

The new artifacts also pass host/emulated-AArch64 bundle composition in1.720
seconds, then actual bottom-board CPU composition in3.122 seconds. Four native
positive compositions and four rejection controls pass; downloaded bundles are
independently inspected and equal the host/QEMU bytes. Scratch is removed and
the authenticated named image/bundle generation30 and same boot are verified;
the board is released. No FPGA, software, image or host activation occurs.
[standard-pr-generated-clock-native-composition.json](standard-pr-generated-clock-native-composition.json)
records that CPU-only proof. ROM composition, the Linux PR opener, physical PR
and native standard-persona BASIC remain open.

The fresh fitted review checks144 audio clock/data paths and48 paired paths
from the100MHz reset-release register to the two sticky epoch-enable receivers.
Minimum conditional audio budgets are158.935ns setup and159.219ns hold, before
PCB skew, additional jitter/uncertainty, signal integrity and PLL/reset transients.
Those two enable paths have at least8.798ns setup and0.331ns hold slack across
all three revisions and four conditions. This is not coverage of every
asynchronous reset fanout and does not waive the raw reset/enable finding.

The physical three two-stage PCM control chains have the expected isolated
first-stage fanout in every revision. Their36 corner/chain reports are identified
only automatically and excluded from calculated MTBF. A private post-fit probe
shows explicit identification enables estimates at tool defaults; the requested
100MHz toggle bound is not applied, including after preloading the private QSF.
The failed strict probe and all reports remain retained. No conservative-rate
reliability claim or new exception follows. The actual recipe needs explicit
identification and verified toggle assumptions in a fresh fit before qualification;
see [Altera's reporting reference](https://resources.altera.com/quartushelp/current/tafs/tafs/tcl_pkg_sta_ver_1.0_cmd_report_metastability.htm).
[standard-pr-generated-clock-fitted-review.json](standard-pr-generated-clock-fitted-review.json)
binds the observations and their limits. The internal16K clock approval is not
extended to this live-PCM PR profile.

The actual recipe at `c053cdf03` explicitly identifies all six control stages
before fitting, requests 100 MHz at all three heads and rejects missing chains,
tool-default rates, excluded MTBF entries or unexpected intermediate fanout.
The fresh complete three-revision build passes in 1373.275 seconds. All 36
control/corner observations use the requested rate, two explicit stages and
MTBF inclusion; the raw worst-case estimates exceed one billion years. All 49
constrained internal summary rows pass per revision, and both personas preserve
100% of the static root. The unmodified seven-command affected plan passes at
`28f4ae754` in 972.998 seconds, including the actual Tcl report rejection tests.

Fresh fitted observations cover 144 audio paths and 48 paired reset-enable paths.
Minimum conditional audio budgets are 159.097 ns setup and 159.342 ns hold; the
two epoch-enable receivers have at least 8.489 ns setup and 0.359 ns hold slack.
The same PCB/jitter/uncertainty/signal-integrity/transient exclusions apply.
Host/emulated-AArch64 composition passes in 1.718 seconds; the bottom DE25 CPU
then passes four compositions and four rejection controls in 3.925 seconds.
Downloaded bundles match the host results; the board is authenticated at its
unchanged named baseline and released after scratch cleanup. This CPU test
does not program the FPGA.

[standard-pr-pcm-synchronizer-fit.json](standard-pr-pcm-synchronizer-fit.json)
binds these results. The four raw Design Closure finding groups remain visible
and overall closure still fails. A clean independent three-revision rebuild is
running; no independent-build, PCB, physical PR or new package acceptance is
claimed yet. The two internal16K clock exceptions remain scoped to that profile.
