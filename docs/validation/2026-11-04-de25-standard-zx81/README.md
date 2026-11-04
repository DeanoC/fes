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
