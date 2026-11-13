# Native tenfoot system on emulated devices

This is a QEMU-only integration result from the exploratory DE25-Nano branch,
based on `d95277daa9d5a6befbe7487945cc4865c7684b69`. It supplies no physical
DE25 boot, USB, SD, Ethernet, HDMI or FPGA acceptance.

The existing tenfoot App and renderer execute in a native AArch64 TamaGo image.
The image mounts FAT32 through modern VirtIO MMIO, reads boot configuration,
uses the ordinary HTTP client over the Lneto/VirtIO network adapter, and renders
to QEMU ramfb. The private host runs the existing FogCast scanner/catalog/API
with targets disabled. Its three `.sfc` files are synthetic browsing samples;
these are not playable ROMs and the cards correctly report “Browse only”.

## Result

[The complete receipt](result.json) binds the native compiler, linked ELF,
compiled source/asset hashes, host executable, disk and commands. The native ELF
is `0db3429453697416029a839a4adb45c5654d1e20b9492dc0a13146808bca72e3`.
The run finishes in about 5.6 seconds on this host; this is a functional check,
not a performance result.

- First boot reads `FES/BOOT.JSON` and preallocated `FES/PREFS.JSON` from FAT32.
- Guest catalog IDs exactly match the real host's [catalog response](catalog.json).
- A live PL011 serial Right sequence moves focus from Aster to Borealis.
- A live `l` changes Grid to Shelf; the [saved JSON](preferences.json) is 98 bytes.
- The same disk is reopened by a fresh QEMU process. Tenfoot restores Shelf
  before fetching the catalog and renders it successfully.
- The runner captures actual scanout using QMP. It does not reconstruct frames
  from serial pixels. [Initial](initial.png), [Right](right.png),
  [Shelf](shelf.png), and [restored](restored.png) images are retained.

[First-boot](boot-1.log) and [second-boot](boot-2.log) logs contain the HTTP,
catalog, input, storage and frame markers. At first readiness the NIC reports
13 packets received, 13 transmitted and zero dropped. Full artifacts, ELF,
FAT32 image, packet captures and host state remain under
`out/native-runner-tenfoot-final-3/`.

## Additional checks and discovered limits

The separate network diagnostic transfers 192 KiB in both directions and
handles ten inbound requests with DMA memory prefilled `0xa5`. Resetting the
emulated NIC is observed through `Err`/`Done`; new sockets then fail promptly.
The [network log](network-diagnostic.log) is diagnostic evidence, not a board
Ethernet test. Fifty concurrent-client requests also pass in a
[separate control](network-concurrency.log).

A controlled stress test found that a goroutine running 180 ms CPU bursts can
starve TCP progress on this cooperative runtime. Yielding after each complete
burst is insufficient; dividing work into 2 ms chunks restores all ten tested
requests. [Failure](network-busy-failure.log) and
[cooperative control](network-busy-cooperative.log) are retained. This image
has an explicit yield between frames, but does not establish bounded latency
for large artwork decoding or other future CPU-heavy application work. Such
work needs smaller cooperative chunks before extending the workload.

The runner's interactive path was checked separately using a copy of the
persisted disk (`out/native-runner-interactive/`), with terminal input and
Ctrl-] exit. A competing process is refused by the per-disk lock before mtools
or QEMU opens the disk. The original successful disk remains unchanged.

During integration the real host's loopback Host-header policy rejected the
QEMU gateway authority. Boot configuration now supplies the existing
`WithAPIHost` override without altering server policy. Enabling PL011 FIFOs
also prevents terminal escape sequences being split across slow UI frames.
Earlier failed attempts remain in `out/native-runner-tenfoot*` for comparison.

The [repository checks](repository-checks.json) pass: 1065 parent regressions,
generated-consumer consistency, FogCast Go and browser suites, plus dead-code
analysis for Linux amd64 and ARM. Focused tenfoot preference-store tests also
pass with the race detector.

## Remaining boundary

FAT updates are limited to existing preallocated files. Writes flush data and
then directory metadata; this proves clean-restart persistence, not atomic
publication after abrupt power loss. The default Go OS filesystem is still
RAM-backed. No package installation, writable game media, artwork download,
USB enumeration or FPGA transition is qualified here.

Next integration work is USB/HID and an explicit runtime-owned FPGA lifecycle
model, followed by the board-revision-specific firmware/DDR/controller port.
Keep the successful QEMU image as the portable software reference when replacing
its virtual devices with Nano drivers. No shared kit or Powerboat process was
used or modified.
