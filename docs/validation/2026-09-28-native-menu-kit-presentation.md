# Native menu HDMI presentation on the designated kit — 2026-09-28

Classification: **exact-artifact hardware diagnostic pass** for the menu
package and matching runtime/agent binaries below. This was a disposable
appliance image test, not a two-pass FES image release or factory menu
selection. The original kit image was restored after the test.

## Exact artifacts and boot admission

- Sealed `fes.menu` package:
  `0d1ecd3328237fb4ba93e69c69dee45f48b4251a063e54cc86e6f7a8c96b1cc2`;
  RBF SHA-256 `839b4084851c7be180fbcc6612c22dd2dab546bb5fcb583e87fe732b4f7dde28`
  (2,012,379 bytes), built from source `8907788dde541788728778fdc5d2f2d2591b280b`.
- Runtime source `9455a708cf95938ffd8ef57083a2aa760c29036a`, binary SHA-256
  `6f96bde5da38074acf03c94f1459a0c9ab1f870faf2557bbf115fd139d04f7b6`,
  advertised version `git-9455a708cf95`.
- FogCast agent source `e6582868d77058854ec685bab9f86e91715ae407`, binary SHA-256
  `9f45735e0c59785b8e8887b08ed22b25ee1211ec00657e9800184748e93a8022`.
- Derived diagnostic root image SHA-256
  `052a39fd9dbdf65909618bee03ba2d01577431b044591a7b612d346c526b702f`
  (67,108,864 bytes). The supported appliance updater staged, booted and
  confirmed it with boot ID `27076859-e381-4f12-b879-6f19d3c7568c`.

The user physically power-cycled the kit before the test. The runtime's boot
record read `latched` after that cold boot and again after the matched-image
boot. The kit reported kernel `5.15.1-MiSTer`, `mem=511M
memmap=513M$511M`, and actual Linux System RAM only at
`0x00000000–0x1fefffff`; the FPGA DDR window at
`0x30000000–0x3fffffff` was excluded. The runtime mapped that window through
the qualified `/dev/mem` `O_SYNC` path and used the ARM `dsb sy` visibility
barrier before submission. The test did not independently inspect page-table
cache attributes; observed scanout and counters validate this kernel/image
combination. The kit lease remained held through programming and presentation.

## HDMI presentation and resource measurements

The ARM diagnostic client painted complete 1280×720 RGBA frames into sealed
staging descriptors. Runtime acknowledgements advanced from generation 1,
displayed sequence 3 to sequence 4,463. A continuous 600.044-second run
submitted 4,460 new frames with **zero underflows** and no runtime error.
Presentation calls averaged 76.225 ms and reached 95.623 ms maximum. Client
CPU time was 258.259 seconds and peak RSS 6,100 KiB. Runtime samples over
394.43 seconds recorded 21,882 additional CPU ticks (about 218.82 CPU
seconds at 100 Hz), RSS from 3,772 to 6,928 KiB, and a 7,388 KiB high-water
mark. No sustained stall or growing memory trend appeared in those samples.

The operator saw stable RGB bars, grid, changing sequence strip and full-width
output on the monitor. HDMI capture confirmed alternating sequence strips,
all six colour bars, row/column grid and the deliberate one-pixel far-right
row/sequence marker. That marker can look purple; it is painted by the test
client and vanished with the diagnostic image. The capture's YUYV mode first
returned black; MJPEG and later BGR captures showed the actual pattern. PNGs,
sampling records and raw client responses are retained under
`out/validation/native-menu-presentation/kit/` in the worker checkout.

## Recovery and menu/game handoff

- A prepared frame followed by client disconnect left the menu available.
- An old generation and a commit without its required descriptor were
  rejected with `invalid_request`; the next valid frame displayed with zero
  underflows.
- A sealed frame committed before client disconnect advanced the displayed
  sequence from 4,464 to 4,465. A fresh client frame then advanced it to
  4,466 without an underflow.
- The installed Pong package
  `55497314c644d193638c861800eb6facd592f2413a6a7d27fc3f40744ce57b84`
  loaded and appeared on both monitor and capture. Stop returned to an
  available menu at fresh generation 2, sequence 0; two complete frames
  displayed. The second Pong/Stop cycle returned at generation 3, sequence 0;
  two more complete frames displayed. Both cycles had zero underflows.

The first physical attempt revealed two integration defects. On this kernel,
`mmap(PROT_READ, MAP_SHARED)` of a `F_SEAL_WRITE` memfd returned `EPERM`;
`MAP_PRIVATE` succeeded. The target test failed before and passed after
runtime commit `9455a708`. The old FogCast agent also rejected the new
optional `menu_display` status field under its strict decoder, preventing
appliance idle confirmation. Agent commit `e6582868` accepts and validates
that field; the matched-image boot reported `raw_idle_ready=true` even while
the menu was configured. Initial staging outside the trusted package root
was rejected before FPGA programming and corrected by staging under
`/tmp/fogcast-development/core-packages`.

## Cleanup and next gate

The diagnostic client stopped, the lease was released, and the supported
appliance updater selected and confirmed the original baseline root image
`0103b5f04cbe256ed84e97d589ec5cf108b1031e415820bd28c8bd3cb16b1a4d`
with boot ID `ee0c4e69-d89f-457a-95ce-ca8abd337bf7`. Original runtime
SHA-256 `d5776191048b112f852443db260962760cd14cce106acd2c068894cac6c07235`
and agent SHA-256
`d9d2cf5a582a99b156ad71feb3103a1b50e3d7f5c8abb4224c9e58505efa52b0`
matched again. The kit lease was free and HDMI showed the normal FES idle
splash, without the edge marker. The diagnostic image remains retained as an
immutable previous candidate.

The next separate integration gate is a tenfoot renderer and mesh-aware menu
shell using this runtime presentation path, followed by stabilized FES image
selection and two-pass release evidence. This test alone does not install a
product on-kit menu.
