# DE25 pc386 shell (slice 3): design

Slice 3 of #2029. It follows the lane plan (#2030), whose decisions
were accepted on 20 November 2026:

- P1, option A: all of pc386's memory in HPS DDR.
- P3: 61.437908 MHz.

The coordinator's brief adds three things:

- Generalise the managed DE25 machine builder for three writable F2SDRAM
  masters, the pc386 clock, and the plane derived out of the machine.
- Delete the single-master assumptions, without a pc386 special case
  beside them.
- Any MENU, ZX81, Spectrum or ST ID that moves takes the D12 netlist
  comparison in this PR.

This PR stays a draft until slice 4 gives it a Kit D25 run path.

**This note is the design. Nothing here is built yet.** File and line
references are to `origin/main` `8a162c536`, under
`sources/misteross/boards/de25nano/` unless a path says otherwise.

## Measured so far

- **The clock.** Quartus Pro 26.3's `quartus_ipgenerate` makes both P3
  outputs exactly from a copy of `machine/machine_pll.ip` with output 0 at
  61.437908 MHz. The IP chose N = 3 (hi 2, lo 1) and M = 188 (94/94), which
  gives a 3,133.33 MHz VCO. C0 = 51 gives 61.437908 MHz, and C1 = 255 gives
  12.287582 MHz. The existing machine PLL uses the same VCO, with C0 = 60
  (52.222222 MHz) and the same C1. So the 5:1 system-to-audio ratio holds
  exactly. (#2030 had assumed a 1,044 MHz VCO. That was never verified,
  and it is not what the IP chose.)
- **The audio rate.** I2S runs at the audio clock / 256, which is the system
  clock / 1280. The mixer's frame enable must stay `AUDIO_N/AUDIO_D = 1/1280`
  of the system clock, as the DE10 top sets it. It must not be derived as
  48,000 over the true clock: that would drift 34 ppm against the I2S. Every
  other guest time base is an exact rational of 9,400,000,000/153 Hz.

## What exists, and why it has to change

The DE25 fabric reaches HPS DDR through one path. The scanout's read-only
Avalon master (`fes_scanout.avm`) goes through `fes_mem_cdc`, an `mm_ccb`
19.4.1 bridge (128-bit data, 32-bit address, MAX_BURST_SIZE 64), to
`f2sdram_adapter.axi4_sub` (`scanout/integrate.tcl:30-67`,
`scanout/build.py:305-307`). The Linux profile is "scanout-only": read
credits 16, write credits 1, an 8 MiB two-slot grant, and SMMU stream 13
(`scanout/linux_dma.py`). The kernel owner allocates that grant
(`image/kernel/de25-dma`).

These are checked as exact facts:

- `hps/build.py:437-485` requires exactly one ingress, base 0,
  `master_count: 1`.
- `linux_dma.py:19-23, 244` checks the set of eight managed initiators.
- `linux_dma.py:325-340` requires write credits of `'1'`.
- `scanout/build.py:596-609` and `zx81-hps/build.py:437-441` require a
  read-only `avm`.
- `scanout/fes_de25_scanout.v:80` refuses any span but 8 MiB.

The machine builder (`machine/board.py`) has these fixed:

- `MACHINE_HZ = 52222222` and a PLL contract of 52.222222/12.287582 MHz;
- the board SDRAM conduit and pins;
- `RECORD = "standard_zx81"`;
- a component whose only memory master is the scanout's.

The producers (`producers/build_machine_package.py:107-110` and the MENU
and ZX81 ones) record `f2sdram_master_profile: scanout-only` and
`guest_memory: board-sdram`.

## The design

### 1. A window contract, `fes.fabric.de25-hps-window`

It is a new `mister-packages` fabric definition, generated to Verilog,
C and Go headers like `fes_fabric_de25_scanout.yaml`. It is the one place
both the fabric and slice 4's kernel owner read.

- **A grant:**
  - base (lo/hi);
  - span, fixed at 256 MiB (`0x10000000`), the pc386 window, which must
    be contiguous (#2030, Codex);
  - a generation;
  - one control/status block on the lightweight bridge, at its own
    peripheral offset beside the scanout's.

  The runtime writes the grant before Execution starts. The fabric refuses
  traffic until a grant with a nonzero generation and the exact span is
  set. Clearing the grant needs all three masters drained.
- **The masters:** 3. Port 0 is 128 bits, and ports 1 and 2 are 64 bits,
  with word addresses as `fes.memory.hps-ddr` 1.0 defines them. All three
  read and write.
- **SMMU:** one stream ID for all three masters. The agent's reading of the
  owner is that one SID covers reads and writes, so a separate stream per
  master is not needed. Whether the scanout's stream 13 is shared, or the
  window takes its own, is for slice 4's kernel owner. The contract
  carries the value.
- **Bounds:**
  - every burst stays inside the span;
  - no burst crosses a 4 KiB boundary (the AXI rule; `fes_hps_ddr`
    bursts may run to 128 beats);
  - a burst outside the span is a sticky fault that holds the machine
    and is reported in the status block.

### 2. `fes_de25_hps_window`, the DDR adapter

It has the port list of `fes_hps_ddr` (`hold`, `pN_reset`, `pN_drained`,
and the three Avalon ports), so `pc386_system` connects unchanged. Inside,
for each port:

- `fes_hps_ddr_guard`, reused unedited, keeps the hold/drain semantics
  the DE10 seal and `system_tb` rely on;
- the address is rebased: subtract `FES_APPLICATION_HPS_DDR_WINDOW_BASE`,
  which pc386's masters add as a constant (`pc386_disk.sv:43`,
  `pc386_fdc.sv:149`, `pc386_status.sv:44`), check it against the span,
  and add the grant base;
- bursts are split at 4 KiB boundaries and at the bridge's MAX_BURST_SIZE.
  As built, the split is in `fes_de25_window_arbiter`, which joins the three
  ports into one master: a burst that would cross a 4 KiB boundary goes as
  two. Its bursts are at most 128 beats, the window bridge's MAX_BURST_SIZE;
- an `mm_ccb` crosses each port from the machine clock to the 100 MHz
  F2SDRAM side.

Ports 1 and 2 are 64-bit, and the adapter's AXI side is 256-bit
(`axi4_sub` `rdata` 256, `hps/build.py:473-475`). Whether Platform
Designer's width adapter or a 64→128 upsizer in front of `mm_ccb` does
the conversion is for the spike to measure.

### 3. The machine builder, generalised

Following the brief, the single-master assumptions are deleted, not
special-cased:

- **The machine spec** names its F2SDRAM masters, as a list of
  `(name, data bits, writable)`, and the builder creates one `mm_ccb` and
  one F2SDRAM route per master. The scanout is one entry in that list.
- **The machine spec** names its clock, PLL outputs and board conduits:
  SDRAM for Spectrum and ST, none for pc386. `MACHINE_HZ` and the PLL
  contract move into each spec.
- **The verifiers** (`verify_scanout_memory_handoff`,
  `verify_managed_fabric`, `verify_clock_crossings`, `verify_handoff`)
  check the spec's master list exactly, in place of a hard-coded one.
- **`RECORD`** keeps its key (`standard_zx81`), because the MENU, ZX81
  and reset verifiers recognise it (`board.py:26-29`), and renaming it
  moves their identities. Renaming it is a separate decision.
- **The Linux profile** becomes "scanout-only" or "scanout+window" by the
  spec. Each profile is exact: its credits, its initiator set, its SID.

**Closure.** `hps/build.py`, `scanout/build.py`, `scanout/linux_dma.py`,
`scanout/integrate.tcl` and `producers/` are in all four DE25 packages'
closures. So their `BUILD_ID`s move. Each of MENU, ZX81, Spectrum and ST
then takes the D12 comparison in this PR:

- its Quartus Pro QSF and staged sources byte-identical apart from
  `BUILD_ID` and paths (it is a native-Quartus front end);
- an RBF reproduction with main's `BUILD_ID`;
- its timing and signoff gates.

That is about eight Quartus Pro compiles on chip, and their cost is the
main reason to keep the builder edit to what the brief asks.

### 4. The pc386 machine, derived

A `boards/de25nano/pc386/derive.py` makes anchored edits, each of which
fails if its anchor moves. The DE10 sources stay byte-identical.

- In `pc386_machine.sv`, delete `fes_display_plane` and
  `pc386_plane_bridge`. Tie `pc386_port2`'s plane inputs idle. Export the
  presenter's 720p word, and the display request/response, in place of
  the plane's output.
- In `pc386_system.sv`, carry those ports out, and drop the plane
  underflow counter from the status words. Status word 4's plane half
  then reads 0, and the record says so.

The board top instantiates `pc386_system` as main has it: a 386 with no
387, `x87_present` tied to 0. Deano decided (21 November 2026) that the
first DE25 shell ships this way, 387 off as board build j was; slice 3's
package and slice 4's platform, image and kit run target it. The 387 on the
DE25 waits for the area study and slice 6's reductions to find its roughly
3,500 ALMs. (#2033's `X87_PRESENT` parameter isn't on
main, and #2037 deleted `X87_BACKEND = 0`.) Its parts are:

- `fes_de25_hps_window`;
- `fes_de25_session_display`, with the plane window 160/0/960/720, as the
  DE10 sets it;
- `fes_gp_crossing` and the computer transport, as the Spectrum has them;
- the machine audio.

The ROM stays linked in the netlist, as on the DE10 (`pc386_rom`,
`ROM_HEX`). How a firmware-less DE25 format-3 package is expressed is to be
read from `build_menu_package.py`. If it needs a schema change, that change
belongs in slice 4, because the schema, its fixtures and three readers
change together. In that case slice 3 stops at a signed-off candidate and
its record.

### 5. Front end and fits

- **Front end.** The machine is the Yosys SEAL netlist of the derived
  `pc386_system` (`fit_i386_device.py`'s path). Quartus Pro's own
  elaboration of this RTL has no lockstep behind it. The board's own RTL
  (the shell, the adapter, the session display) goes to Quartus Pro
  directly, as the other DE25 machines' does.
- **The shell's cost, by the method fixed in #2030.** Fit the
  virtual-pin envelope around the same derived netlist bytes, reused by
  hash, against the board top. Use four seeds a side. Measure the
  capacity row. A cost counts only if it exceeds the larger spread.
- **Seed stability.** All four board seeds fit, and every clock meets
  setup and hold at every corner.

## Order of work

1. **Spike.**
   - Generate the Platform Designer system with the scanout plus three
     read/write masters, behind a stub adapter.
   - Fit one seed with the real derived machine.
   - Relay the ALM figure to the coordinator, labelled one-seed and
     provisional.

   This shows early whether the fabric shape survives generation, before
   anything is verified.
2. **Freeze the window contract** (step 1 of the design), with its
   generated headers.
3. **The adapter and its bench**: `fes_hps_ddr_guard` semantics, rebase,
   bounds, burst splitting, and `system_tb`'s hold checks.
4. **`derive.py` and a Verilator boot** of the derived machine on the
   adapter.
5. **The builder generalisation**, with the four other cores' D12
   comparisons.
6. **The producer**, with rational rates and `AUDIO` fixed at 1/1280. Then
   four-seed signoff, the controlled pair, and the record.

## Open, for slice 4 or for Deano

- **The kernel owner's window allocation.** That is 256 MiB contiguous,
  below 4 GiB, under one SID, with a profile beside
  `fes,de25-linux-dma-scanout`. It also needs the ABI: the info and
  publish structures are frozen at 104/24 bytes (`de25_dma.c:30-33`) and
  mirrored in libmister-runtime. This is the image change Deano approved
  for slice 4.
- **The native (Linux-free) runtime's memory contract** (#1867/#1952)
  would need the window too, if that runtime becomes the product.
