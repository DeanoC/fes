# DE25 shared computer driver software proof, 2026-10-31

Producer `c147cdc8c7b666c4d315d3ec69cd410613736336` adds the internal
De25ComputerCoreDriver around the existing atomic protocol client and optional
HID/basic-media dispatch in the one CoreDriver interface. NativeHardware uses
its selected driver for these operations. FesGp keeps its existing methods;
no second lifecycle, daemon, factory or package profile table is introduced.

The exact three-interface BRAM computer ABI1.0 descriptor and selected build
must identify before control. Launch media stays held until Start; live tape
replacement sends no execution/reset command. A session boundary does not
clear transport uncertainty. This driver is not registered or constructed by
the production factory; package and AArch64 production admission remain closed.

## Results

- Focused native tests pass: all15 DE25 scripted-register groups and the
  NativeHardware suite including a non-FesGp selected-driver dispatch fixture.
  Cases cover invalid descriptors without writes, changed build contexts,
  launch/Start/live-tape/HID/eject, and no replay after an uncertain Start.
- All six affected commands pass in463.4 seconds, including parent Python,
  consistency, full runtime functional/incremental/version/exact archive and
  hardware-support-truth checks. Detailed output and job identity are retained.
- `make check` passes at the same producer in7.0 seconds.
- Locked SDK builds the full static AArch64 archive and daemon. The programmer
  filesystem tests pass under QEMU. All15 computer groups pass with the SDK's
  dynamic libraries and existing cortex-a53/QEMU recipe (22.9 seconds total).
  Output hashes and SDK identity are in sdk-build.json. This is software-only
  evidence: registers are modeled, and no new binary ran on the board.

The initial static computer-test binary aborted under QEMU with std::system_error
and unknown error16786624. Its complete failed build/test receipt is retained.
The retry changes only the test linkage/QEMU invocation to the established SDK
recipe; source bytes are unchanged. It does not establish that static threaded
execution works or diagnose the root cause of that failure. Both trials still
build the static production archive/daemon successfully.

## Hardware and remaining work

No board, serial, HDMI capture, card, QSPI, reboot or FPGA operation occurred
for this increment. The prior [programmer diagnostic](../2026-10-31-de25-runtime-programmer/README.md)
remains source-bound to its earlier adapter/library artifacts, not this driver.

Next integrate unit-based media discovery/insert/eject/save and status through
the selected driver, then platform-aware package/registry/startup and safe
restart adoption in the existing factory/lifecycle. Managed MENU, agent/catalog,
normal ZX81 launch/stop/relaunch/recovery and DE10-equivalent acceptance remain
open. Physical failed transfer, secure-call/shared-mutex bounds, global DMA
reset/drain, DDR/SMMU/firewall and timing #1284 are separate unfinished gates.
The current known BRAM image cannot qualify the external-DMA path.

Independent complete SDK artifacts and logs are retained at
`/home/deano/fes/out/artifacts/de25-core-driver/c147cdc8c/`.
