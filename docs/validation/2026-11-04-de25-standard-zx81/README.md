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
The complete-board recipe still needs to select and connect the wrapper.

The corrected standalone diagnostic fault observer also passes eight selected
native/AArch64 identity/signature/reset cases in9.542seconds. The full20-case
standalone matrix is not claimed. After this and the14-case combined proof,
the owned temporary SDK volume was removed; authenticated SDK archives/cache,
source snapshots, binaries, logs and proof receipts remain available.
