# DE25 pc386 shell: the plan, and the memory question it needs answered

On 20 November 2026 Deano accepted the x87 slice 6 plan (#2027). Its decision
D4 makes the DE25-Nano pc386 board shell, producer and package a separate lane,
started at once. Slice 7 (the 387 switch flip on the DE25) and the 486DX
(DE25 only) both need this lane. The shell's measured ALM cost also sets slice
6's area target (D2). Until then the working line is 92%, 43,056 ALMs.

This record plans that lane. **It builds no RTL and runs no fit.** Every fact
below cites the file or record it came from, at main `8a162c536`. One design
question has to be answered before the shell can be built: where pc386's
memory lives on the DE25. The decisions for Deano are collected at the end.

## What pc386 asks of its board

`pc386_system` (`sources/misteross/cores/fes-pc386/rtl/pc386_system.sv`) is the
whole machine apart from its pins. It holds the mailbox, the CPU, the AT
devices, the VGA, its capture writer, the 720p presenter and the
session-display plane. The DE10 shell (`rtl/top.sv`, 147 lines) adds only
these:

- two PLLs: the system clock at 61.44 MHz with a 12.288 MHz audio clock, and
  the 74.25 MHz pixel clock;
- the HPS GP mailbox words;
- the HPS I2C for the ADV7513, and the HDMI and I2S pins;
- `fes_hps_ddr`, which provides **three Avalon ports into HPS DDR, all
  read/write**, in the system clock. Port 0 is 128 bits; ports 1 and 2 are
  64 bits.

Every pc386 memory region sits in one 256 MiB HPS DDR window
(`FES_APPLICATION_HPS_DDR_WINDOW_BASE/BYTES`, `0x30000000`/`0x10000000`).
The table below gives the regions
(`sources/misteross/docs/pc386-machine-design.md`, "HPS DDR window and ports"),
plus one added column: what the HPS must do with each region.

| Window offset | Size | Contents | Port | Fabric | **HPS** |
| --- | --- | --- | --- | --- | --- |
| `0x00000000` | 8 MiB | Session-display plane slots | 2 | read | **writes** (launcher frames) |
| `0x00800000` | 256 KiB | VRAM | 1 | read/write | none |
| `0x00A00000` | 3 × 2 MiB | Capture frame buffers (VGA → 720p presenter) | 2 | write, then read | none needed |
| `0x01000000` | 20 KiB | Status ring | 2 | write | **reads** (`pc386_cpu_status.py`) |
| `0x01400000` | 4 MiB | EMS board memory | 2 | read/write | none |
| `0x01800000` | 2 × 4 MiB | Floppy A: and B: images | 2 | read/write | **writes** the image, **reads** it back to save |
| `0x04000000` | 64 MiB (16 used) | Guest RAM | 0 | read/write | none |
| `0x08000000` | 128 MiB | ATA disk image | 2 | read/write, CRC pass | **writes** the image, **reads** dirty sectors back |

Two facts follow from this table:

- **The picture itself travels through memory.** The VGA writes captured
  frames, and the presenter reads them back for 720p. So the shell needs a
  writable memory, even to show POST.
- **The disk and the floppies must be both HPS-writable and fabric-writable.**
  This matches Deano's standing direction of 22 October 2026: media for every
  core lives in HPS DDR. Guest RAM, VRAM, EMS and the capture buffers are the
  fabric's alone.

## What the DE25 has today

All of the following is on main.

- **HPS memory from the fabric is one read-only path.** The managed DE25
  scanout reaches HPS LPDDR4 A through the GHRD's F2SDRAM adapter, through a
  128-bit `mm_ccb` at 100 MHz. Its Linux profile is named "scanout-only"
  (`boards/de25nano/scanout/linux_dma.py`, `retarget_adapter`). It raises
  read credits to 16 and leaves write credits at one. The allocation is
  8 MiB, two slots (`scanout/build.py`, `allocation_bytes`). The kernel
  owner allocates that from CMA (`image/kernel/de25-dma/`). It is the only
  fabric master on the F2SDRAM adapter (`MANAGED_MASTERS`). No DE25 fabric
  master writes HPS memory.
- **The DE25 machines put their media in board SDRAM, not HPS DDR.** The
  Spectrum tape and the whole Atari ST (RAM, floppy, the uploaded ROM) go
  through `fes_de25_sdram`. That controller drives the board's 128 MiB, 16-bit
  SDR SDRAM (#1937; `boards/de25nano/spectrum/fes_de25_spectrum_standard.v`).
  The HPS reaches that memory only through the GP mailbox (firmware upload,
  tape delivery).
- **FPGA LPDDR4 B (1 GiB) exists only in the RAM tester.** The vendor B EMIF
  is integrated in `boards/de25nano/ramtest/` as a destructive standalone
  diagnostic (`ramtest/README.md`). No core uses it.
- **The display contract differs.** A DE25 machine shows its picture through
  `fes_de25_session_display`, which sits outside the machine and owns the
  scanout's read path and the `fes.fabric.de25-scanout` two-slot grant. On the
  DE10, `pc386_machine` holds its own `fes_display_plane` and reads the plane
  slots on port 2 (`rtl/pc386_machine.sv:322`). A DE25 pc386 needs a
  derivation that takes the plane out of the machine and feeds the
  presenter's 720p word to `fes_de25_session_display`. This is the same kind
  of anchored derivation the Spectrum's and ST's `derive.py` records already
  use.
- **The mailbox is ready.** The DE25 copy of `fes_computer_mailbox_core`
  (`boards/de25nano/shared-rtl/fes_computer_mailbox.v`) already has the 1.1
  ATA-disk, persistence, pc386-machine and wall-clock interfaces.
- **The machine-board builder is fixed to one clock.** `machine/board.py` has
  `MACHINE_HZ = 52222222`, and its PLL contract accepts only 52.222222 and
  12.287582 MHz (`machine_pll`). It also keys the standard-core integration
  record under `standard_zx81`, because the scanout, HPS and reset verifiers
  that MENU and ZX81 share recognise that key. Generalising it touches files
  in those cores' closures, so their `BUILD_ID`s may move (see the slices).

## The memory question

pc386 needs **a fabric path that writes HPS DDR**. The shared regions hold
8 + 8 + 128 MiB (plane, floppies, disk), plus the status ring. At today's
offsets, though, they span the whole 256 MiB window. That
path does not exist on the DE25. Whatever happens to the fabric-only regions,
it is platform work across three layers:

- the F2SDRAM profile and SMMU stream (write credits, several masters);
- the kernel owner (a contiguous 256 MiB allocation at today's offsets, where CMA today
  serves 8 MiB);
- the runtime (writing the disk and floppy images into the allocation, and
  reading back dirty sectors).

The kernel and CMA parts are an image change.

So the question is not whether a writable HPS DDR path is needed. The
question is how much of pc386 rides on it.

| | Option | Fabric-only regions (RAM, VRAM, EMS, capture) | Shared regions (plane, disk, floppies, status) | What it costs | `pc386_system` change |
| --- | --- | --- | --- | --- | --- |
| **A** | **Everything in HPS DDR**: the DE10's design | HPS DDR, through F2SDRAM | HPS DDR, through F2SDRAM | One contiguous 256 MiB window. The disk region starts at the fixed offset `0x08000000` and may run 128 MiB, to `0x0fffffff`, and J8G's owner allocates one contiguous span (`DMA_ATTR_FORCE_CONTIGUOUS`). So anything smaller fails a full-size disk unless slice 4 adds sparse mappings or address translation. Three fabric masters with writes on the F2SDRAM adapter. The CPU's L1 refills and the presenter share HPS DDR with Linux, as they do on the DE10. No EMIF. | None beyond the display derivation |
| **B** | **Split**: fabric-only regions in FPGA LPDDR4 B | LPDDR4 B, through the vendor B EMIF | HPS DDR, through F2SDRAM | A smaller HPS window only if the shared regions are packed into new offsets; with today's offsets the disk still ends at `0x0fffffff` and two memory systems. The EMIF B's fabric cost is unmeasured outside the RAM tester, and it takes an I/O bank. Port 0 alone (the CPU) could move first. | Ports split by region: port 0 and VRAM move, port 2 splits |
| **C** | **No HPS DDR for the machine**: media through the mailbox into board memory, as the Spectrum and ST do | LPDDR4 B or board SDRAM | LPDDR4 B or board SDRAM; the HPS delivers and saves through GP | A new storage protocol: 128 MiB through the GP mailbox at load, and dirty sectors back through it. Board SDRAM is 16-bit SDR, slow for the CPU's 128-bit refills. | Disk, floppy and status paths rewritten |

**Recommendation: A.** It is the DE10's design, so `pc386_system` stays
unchanged apart from the display derivation, and the lockstep, seal and
census evidence carries over. It follows Deano's media-in-HPS-DDR direction.
Its platform cost (a writable F2SDRAM profile, a large kernel allocation,
runtime delivery) is needed by options A and B alike. A is the shortest path
to a running package. B is a later optimisation, for if HPS DDR latency or
bandwidth proves to hold the CPU back on the kit, and it can be measured
then. C contradicts the 22 October direction, and its board SDRAM variant
would starve the CPU.

What A leaves open, and this record does not settle:

- **Can the kernel owner reserve a window that large on the J8G image?** The
  KUnit configuration's CMA is 32 MiB (`image/kernel/de25-dma/.kunitconfig`).
  The board's figure is in its kernel command line or device tree, which this
  record has not read.
- **The SMMU's shape.** Whether one stream ID serves three writing masters,
  or each needs its own.
- **The native (Linux-free) DE25 runtime of #1867/#1952** has its own physical
  memory contract. A pc386 window would have to appear there as well, if that
  runtime becomes the product.

## The clock

The DE10 rung is 61.44 MHz, with audio at 12.288 MHz (a ratio of 5). The
existing DE25 machine PLL makes 50 × 188/15, then /12 and /51
(52.222222 and 12.287582 MHz, both −34 ppm). The same multiplier at a
different pre-divider gives pc386's pair at the same −34 ppm:

| | Output | Exact |
| --- | --- | --- |
| VCO | 50 × 188 / 9 MHz | 1,044.444 MHz |
| System, /17 | **61.437908 MHz** | −34 ppm |
| Audio, /85 | **12.287582 MHz** | the existing machine PLL's audio clock, unchanged |

The VCO range and counter limits are only assumed here, and need
`quartus_ipgenerate` to confirm them. The exact 61.44 MHz needs M = 768, and
no integer setting with a reasonable N reaches it (searched: N ≤ 32,
M ≤ 320).

**This is harmless to the guest, and it changes one producer assumption.**
Every guest time base is an exact N/D of the sealed frequency
(`scripts/build_fes_pc386_quartus.py`, `guest_rates`). But that function takes
an integer in Hz, and `SYSTEM_CLOCK_HZ` is the integer 61,440,000. The DE25
clock is the rational 9,400,000,000/153 Hz. `guest_rates` would have to take
a `Fraction`, and its N/D pairs grow: they must still fit the RTL's 32-bit
integer parameters, which the producer slice has to check. Rounding the
clock to an integer would put a fixed error into every time base, and is not
proposed.

Slice 6's D3 (a clock above 61.44 MHz, with a ladder) stays open and is
separate. This lane builds at the DE10 rung unless D3 says otherwise.

## `x87_present` and `X87_BACKEND` as per-board settings

Today `pc386_system` ties `x87_present` to `1'b0`. Every DE25 fit has turned
on `x87_present` and `X87_BACKEND` by two scratch edits (#2027, section 4,
item 3). Making them parameters of `pc386_system` is a small RTL change, but
it is in the DE10 package's source closure. It moves the DE10 `BUILD_ID`,
and it is not a closure-only change under AGENTS.md's D12 rule, because
it edits RTL. So it owes #1768's DE10 gates:

- lockstep on the RTL and on the locked-Yosys netlist;
- test386;
- a Quartus 17.0.2 identity-off seal;
- a DE10 kit run.

A parameter with a default of 0 cannot flip the DE10 identity. The DE25
shell would pass 0/0 until slice 7. This change belongs in its own PR, so
that the DE10 reseal lands alone.

## Slices

Each slice is one PR. Slices 2 to 4 wait on decision P1.

1. **This plan** (doc only).
2. **Per-board x87 settings**: the parameters, with the DE10 gates listed
   above. This does not wait on P1, and can run alongside slice 3.
3. **The shell and producer, x87 off, fitted and sealed.** The work:
   - a `boards/de25nano/pc386/` component with a `derive.py` (the plane out,
     the presenter's word to `fes_de25_session_display`), a board spec, and
     the PLL above;
   - the writable F2SDRAM masters for the option P1 picks;
   - a Verilator boot of FreeDOS on the derived machine;
   - a producer that writes a design-v2 record with `device_family`
     Agilex 5;
   - a Quartus Pro 26.3 signoff on chip-30x0-ai-dev.

   The fit is the controlled pair below. This slice also generalises
   `machine/board.py`'s clock. If that moves the MENU, ZX81, Spectrum or ST
   `BUILD_ID`, those cores take the D12 netlist comparison in the same PR.
4. **Platform**: the writable F2SDRAM profile, the kernel owner's pc386
   window and the image that carries it (an image change, which needs
   Deano's OK). Then runtime and FogCast admission, extending #1937's
   per-core table with pc386 (ATA disk, floppy, wall clock, the machine
   record, mouse). Then the Kit D25 exact-head run through
   `docs/de25-kit-hil.md`.

Kit D25 (`de25-j8g`, `TRWU8J8G`) exists and runs the HILQ MENU and ZX81
baselines. A pc386 kit run cannot happen before slice 4. That does not make
the earlier slices HIL N/A:

- **Slice 2** changes the DE10 package, so it merges only with a DE10 kit
  run (Kit A or B) on its exact head.
- **Slice 3** makes a DE25 bitstream, and the merge gate wants an exact-head
  Kit D25 PASS for that (AGENTS.md, "Merge gate"). Its fit and seal are
  host-only evidence, which can be reviewed but not merged on its own. Slice 3
  stays a draft until slice 4 gives it a run path, and the two land together,
  as a stack at most two deep. The only other way is a waiver from Deano
  himself.

## The shell's cost: method, committed before any fit

This is the measurement D2 is waiting for.

- **One tree, two tops, same front end.** The two tops are #1899's
  virtual-pin envelope around `pc386_system` and the slice-3 board top. Both
  use the same Quartus Pro 26.3 build, the same device, the same netlist or
  SystemVerilog front end (whichever #1899's envelope used), the same
  settings, and the same `x87_present = 0`.
- **Four seeds a side**, 1 to 4.
- **The quantity** is Quartus's `ALMs needed [=A-B+C]`, the capacity row.
  The LAB row is not the measure (#1899).
- **The shell's cost** is the median of the board seeds minus the median of
  the envelope seeds. Each side's spread is reported next to it, and the
  difference counts as measured only if it exceeds the larger spread. The
  virtual-pin cells (651 ALMs in #1899's machine envelope) are reported
  separately, because they are what the cost replaces.
- **Seed stability at the system clock**: all four board seeds fit, and
  every clock of the shell meets setup and hold at every published corner.
  Three of four is a fail, not a candidate.
- **The D2 figure** for slice 6 is the identity-on fit (from #2027's 6.0a,
  once it runs) plus the shell's cost. It is labelled uncontrolled, because
  the two come from different trees.

## Decisions for Deano

| | Decision | Options | Recommendation |
| --- | --- | --- | --- |
| **P1** | Where pc386's memory lives on the DE25 | A: all in HPS DDR / B: fabric-only regions in LPDDR4 B / C: no HPS DDR, media through the mailbox | **A.** It keeps `pc386_system` and its evidence. Every option except C needs the writable HPS DDR path. |
| **P2** | The image change that a large writable HPS DMA window needs (kernel owner, CMA, SMMU) | Approve it for slice 4 / route it through #1867's secure-lifecycle work | Approve it for slice 4, built on the Linux owner already on J8G. |
| **P3** | The DE25 pc386 clock | 61.437908 MHz (−34 ppm, the same audio clock as the DE25 machines) / an exact 61.44 MHz through a cascaded PLL or another reference | **61.437908 MHz**, with the producer's rates made rational. D3's higher clock stays a separate decision. |
| **P4** | Tier and support matrix | A `de25-nano` pc386 recipe row and support-matrix entry with slice 3 / none until slice 4's kit pass | **None until slice 4's kit pass.** DE25 is tier 2. A row naming a package that cannot launch would overstate it. |

## Decisions (20 Nov 2026)

Deano accepted all four recommendations as written: "Accept all four",
20 November 2026, relayed by the coordinating session. The table above is
the proposal and stays as it was. What was decided:

- **P1, memory:** option A. All of pc386's memory lives in HPS DDR, as it
  does on the DE10.
- **P2, image change:** the image change for a large writable DMA window
  (kernel owner, CMA, SMMU) is approved for slice 4, built on J8G's Linux
  owner. The approval covers building and testing the change. Writing an
  image onto a kit is still a destructive kit step: it needs Deano's own
  explicit OK for the exact image and kit when slice 4 reaches it. A relayed
  approval does not count for that step.
- **P3, clock:** 61.437908 MHz, with the producer's guest rates made
  rational.
- **P4, tier:** no `de25-nano` pc386 recipe row or support-matrix entry
  until slice 4's kit pass.

## Sources

- [x87 slice 6 plan, #2027](https://github.com/DeanoC/fes/blob/deano/i386-x87-s6-plan/docs/validation/2026-11-20-i386-x87-s6-plan/summary.md),
  sections 2 and 4 and decisions D2–D4 (on PR #2027's branch until merged)
- [Agilex 5 fit, #1899](../2026-11-16-i386-agilex5-fit/summary.md): the
  virtual-pin envelope, the 651 ALMs of virtual pins, and the capacity row
- [Transcendentals fits, #1959](../2026-11-17-de25-x87-transcendentals/fits.md)
  and its [method](../2026-11-17-de25-x87-transcendentals/method.md)
- [i486 plan](../2026-11-17-i486-plan/summary.md): the 486DX on the DE25 only
- #1937: the DE25 Spectrum and Atari ST, from producers to runtime and FogCast
- `sources/misteross/boards/de25nano/` at `8a162c536`: `machine/board.py`,
  `scanout/linux_dma.py`, `scanout/build.py`, `spectrum/`, `ramtest/README.md`,
  `shared-rtl/fes_computer_mailbox.v`
- `sources/misteross/cores/fes-pc386/rtl/top.sv`, `pc386_system.sv` and
  `pc386_machine.sv`; `sources/misteross/scripts/build_fes_pc386_quartus.py`
- `docs/de25-linux.md` and `image/kernel/de25-dma/`
