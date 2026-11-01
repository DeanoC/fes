# MENU frame-mapping agreement

Allocation-backed MENU recovery needs a peer's mappings and descriptors gone
before a frame-commit reply: the failed commit can retire and reload idle before
replying. Releasing only when launch pauses does not cover that failure path.

The runtime now reports `frame_mapping: true` for an adapter that requires it.
The client explicitly agrees when requesting a grant, and the reply confirms
the same lifetime. A peer without that agreement is refused before any fd is
shared. This mode does not support a blitter. An absent flag preserves the
persistent-window request and mapping contract, including older servers.

The client drops its view before committing and before abandoning preparation.
Grant inspection also leaves no mapping. The next frame remaps the same grant
with slot/damage history preserved. Kernel reference exclusion is unchanged:
the agreement cannot release a retained FD/VMA or authorize unknown adoption.
All current production adapters retain the existing DE10 contract. The DE25
presenter, profile, factory and hardware acceptance remain unfinished.

Validation is source-bound software evidence:

- C++ producer `736815a0f38c983ed7dc7027a6ae81231ac5f532`: native routine,
  archive and active-tree tests pass in 381.0 seconds. Grant tests refuse an
  unaware peer before fd duplication and refuse frame-mapped blitter grants.
- Producer `bd856e1cdde8a113f35c0b9bad0c7bdcc5d9f58c`: the standard dynamic
  AArch64 runtime/archive/agent recipe passes in 309.3 seconds, including ten
  actual Cortex-A53 C++ test binaries and the required agent tests. The entire
  runtime module is byte-identical between that producer and the final UI
  producer below; later UI changes do not claim to rebuild the C++ binary.
- Final UI producer `ba43ac5aa5acc3ba14225849515eef9538b3e154`: menu-client,
  gfx and launcher packages pass. Four new frame-mapping cases pass twenty
  race-detected repetitions (13.4 seconds). Actual shared-mapping observations
  check that the client's view has ended when commit and abandonment reach the
  local socket server, including a failed commit reply. Damage updates survive
  remapping, and a grant with a different lifetime is rejected before mapping.
- The same final producer builds the static AArch64 launcher and test binaries.
  All nine named Cortex-A53 QEMU mapping/pause cases pass (7.8 seconds); the
  receipt checks each case ran without failure or skip. No launcher startup,
  board display, cache-coherency or physical DMA acceptance is claimed.
- The actual combined ZX81/PR proof at `bd856e1cd` passes all fourteen native
  and Cortex-A76 client/RTL cases in 1655.3 seconds, including BASIC LOAD/RUN,
  ROM SAVE and reload. Compiler and runtime input hashes match this merged
  source closure exactly before refreshing the canonical receipt; the receipt
  retains its actual producer. This model does not establish physical SAVE or
  normal catalog launch/stop/relaunch.

The earlier incorrect pixel-layout test expectation and unsynchronized test
setup failures remain retained with their source. The final result includes
both corrections and the close-before-unmap preparation cleanup correction.
No physical board, kernel, FPGA, QSPI or installed bundle changes are made by
these checks. The full normal development-kit acceptance remains open.
