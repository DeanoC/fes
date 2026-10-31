# pc386 M5, slice 1: fes_i386_cpu in the machine, 29 October 2026

**FreeDOS boots to `C:\>` on `fes.pc386` on Kit B.** The package is the
pc386 machine with [`fes_i386_cpu`](../../../sources/misteross/cores/fes-common/rtl/i386/)
and its L1I, L1D and bus interface on DDR port 0, in place of M4's bus
master. Quartus 17.0.2 sealed it at the 61.44 MHz system clock with every
clock and analysis met. The same system, everything below the board shell,
boots FreeDOS in simulation with no oracle behind the CPU. Kit B ran the
29 October package. The 31 October seal, after the merge of #1230's head,
has had no kit run.

- **Simulation.** The system bench runs `pc386_system` with all three DDR
  ports on one window model. Its host does only what a kit's host can,
  through the window: it stages the disk and configuration, then reads the
  status block, the debug ring and VRAM.
  - The Bochs BIOS and VGA BIOS post with the same debug-port lines as the
    oracle boot of the [M1 record](../2026-10-26-pc386-m1-freedos-boot/summary.md).
  - The prompt appears 2.55 simulated seconds after the configuration, and
    45,534,556 instructions have retired by the end of the run. Nothing
    stopped or stalled the CPU, and no underflow, drop or reset counter
    moved.
- **Seal, 31 October.** Build `951c8048…`, package `15c5c86d…`, Yosys
  `0a7927bf`, at `2c13827a2`, after the branch merged #1230's head
  ([below](#merge-conflicts)).
  - Worst setup slack +0.709 ns, on the pixel clock. The system clock has
    +2.250 ns, and its slow-corner Fmax is 71.30-73.80 MHz.
  - 24,004 ALMs (57%), 251 of 553 RAM blocks and 27 of 112 DSP blocks. The
    design's estimate for the first playable machine was about 77% of the
    ALMs.
  - The system bench boots the merge cycle for cycle as it booted the
    29 October RTL.
- **Seal, 29 October.** Build `0efa2213…`, package `538773ad…`: setup
  +1.969 ns, 23,147 ALMs (55%). Kit B ran this package.
- **Kit B, run m5k1.** A core-only run of the 29 October package, with
  [`pc386-host`](https://github.com/DeanoC/fes/blob/c97eee0b6f/sources/misteross/cores/fes-pc386/host/main.go) on
  the HPS staging the FreeDOS fixture through `/dev/mem`.
  - The kit printed the same BIOS lines as the simulation, then reached the
    prompt 2.53 s after the configuration, 45,710,361 instructions retired.
  - Every underflow and drop counter read zero, and nothing stopped the CPU.
  - HDMI shows the FreeDOS banner, FreeCOM and `C:\>`
    ([grab](kit-m5k1-prompt.png), [report](kit-report-m5k1.json)).

The first simulated boot found one VGA scanout underflow before the VGA BIOS
ran. It came from a DDR latency tail on the CRTC's reset raster, five
characters by two lines, which no software programmed. The scanout now counts
underflows only once the CRTC has been written
([below](#the-scanout-underflow-before-the-vga-bios)).

This record has host-only checks, a sealed package and a hardware diagnostic
boot. The four games, the keyboard and mouse, and the M0 contracts are later
M5 work.

## Inputs

- FES branch `deano/pc386-m5-cpu`, sealed at `c97eee0b6f` on 29 October and
  at `2c13827a2` on 31 October. It merged main `6a7e47d0af` and these
  branches, merges only, no rebase:

  | Branch | Head | Content |
  | --- | --- | --- |
  | `deano/pc386-quartus` ([#1262](https://github.com/DeanoC/fes/pull/1262)) | `5dc3bb161f`, `01423f9e68`, then `e10811e575` | The Quartus sealer on #1245 and the M4 shell of #1226. The second head fails the gate on `-0.000` slack and on unanalysed clocks; the third brings #1226's review fixes, `pc386_snapshot` and the lock's move to Yosys `0a7927bf` |
  | `deano/i386-smc` ([#1234](https://github.com/DeanoC/fes/pull/1234)) | `ee0c7c8ac0` | Memory slice 2 on #1230's memory path |
  | `deano/pc386-game-census` ([#1243](https://github.com/DeanoC/fes/pull/1243)) | `853ac57f0e` | The game census on #1195's RTL boot |
  | `deano/pc386-at-m10k` ([#1153](https://github.com/DeanoC/fes/pull/1153)) | `962f7cd0c0` | Test and record fixes; no change to the netlist |
  | `deano/i386-mempath` ([#1230](https://github.com/DeanoC/fes/pull/1230)), 31 October | `45af99a23` | Main `bb0680c66`, which squashes every branch above but #1234, with #1234's squash, the CPU work since and the memory-path review fixes. Sealed again at the merge, `2c13827a2` |

- Quartus Prime Lite 17.0.2. The netlist comes from the pc386 lock's Yosys
  `0a7927bf`, DeanoC/yosys#68's squash merge on Yosys main
  ([`toolchain.lock`](../../../sources/misteross/cores/fes-pc386/toolchain.lock)).
  The two earlier fits below used its predecessor, `1f7d0273`, #68's head.
  Verilator 5.032 for the benches.
- The locked Bochs BIOS and LGPL VGABios (`firmware.lock`: BIOS `8caa3f2f…`,
  VGA BIOS `69401dc8…`), and the FreeDOS 1.4 fixture of `freedos.lock`
  (`freedos.img`, SHA-256 `b9d36549…`).

### Merge conflicts

- **`deano/i386-smc`:** 13 files. Main carries the i386 Phase 1 PRs
  (#1161-#1181) squashed, and the branch carries the same commits unsquashed.
  In 12 of the files, main's version is a blob in the branch's own history,
  so the branch's version was taken: the i386 RTL, the lockstep bench and its
  tests, the i386 and docs READMEs, `benchmark_fes_i386.py` and
  `test_fes_i386_lockstep.py`. `sim_fes_pc386.py` combined both sides: M4's
  `diag` suite and `verilate(flags=)`, and the branch's `pc386_vga.vlt` and
  `rtl-stops` suite.
- **`deano/pc386-game-census`:** three files. The SMC branch had moved
  `machine_tb.cpp`'s RTL CPU onto port 0 (`Port0`, the CPU's MMIO, DMA and
  FLUSH ports) and deleted `pc386_rtl_cpu.sv`. The census had extended that
  wrapper and the old data port with its VGA-load accounting.
  - Kept the port 0 bench, and carried over the census's `--stop-history`
    and `--stop-dump`.
  - Dropped `UNRETIRED`, `OVERLAP`, `--trace-port`, the `shadow_faults`,
    `vga_cancelled`, `vga_faulted` and `vga_overlaps` counters, and their
    acceptance checks. This is a design claim: the bus interface issues an
    uncached (VGA) load only for the oldest uop, so no VGA load is on a
    wrong path or abandoned. Restarts show in `replays` instead.
  - Kept the census's rule that `--suite boot --cpu rtl` fails on any stop,
    stall or oracle step.
  - The README took the port 0 description with that rule.
- **`deano/pc386-quartus`, second head:** the README (the M5 text, with the
  head's sentence that TimeQuest must analyse every clock) and the sealer's
  test fixtures (both kept). The sealer itself merged cleanly.
- **`deano/pc386-at-m10k`:** `verilate()` takes both `flags` and the
  branch's `defines`.
- **`deano/pc386-quartus`, third head:**
  - The files M5 deletes stay deleted, and the diag tests the head added go
    with them: the bus master, the port fence, the diagnostic machine,
    `diag_tb`, `pc386_diag.py`, the kit tool and the nextpnr producer.
  - The README, `pc386.vlt`, `sim_fes_pc386.py` and the machine tests keep
    M5's side.
  - `pc386_ddr_model.h` takes both: its 64-bit port now holds masters to
    Avalon-MM, beside M5's port 0 model.
  - `pc386_system` takes the new `pc386_snapshot`. Its status block had read
    the presenter's and the plane's pixel-clock counters in the system clock
    without a synchroniser, as M4's observe words had.
- **`deano/i386-mempath` at `45af99a23`, 31 October.** Main now holds
  squashes of branches this one merged as their own commits. The merge base,
  `6a7e47d0af`, is older than all of them, so a plain merge could quietly
  revert one side. The merge tree starts from `45af99a23` and adds only
  slice 1's changes, so `git diff 45af99a23` lists only slice 1's 44 files.
  - Slice 1 changed 30 files that #1230 left as an input this branch had
    already merged. Those take this branch's version.
  - Both sides changed the other 14. Each was merged with an explicit base:
    this branch's merge of its inputs before slice 1 (`5690dd639`), or
    `e10811e575` for the README and the M4 record.
  - The Makefile and `tests/test_affected.py` keep main's PC/XT entries.
  - The README keeps #1230's stop-test row.
  - `test_pc386_machine.py` and `sim_fes_pc386.py` drop the M4 diagnostic
    tests, the suite and the imports that #1226's review had added.
  - `top.sv` drops the bus master's `TIMEOUT_CLOCKS`.
  - Two of #1230's changes reach slice 1's files. The CPU gained
    `init_cr0`, which `pc386_system` ties to 0 (MP and TS clear, as at a 386
    reset), and new trace outputs, left open. The bench's port 0 model now
    asks its memory for `at(offset, write)`, which `system_tb` answers from
    the window.

The merged base passes on its own. `--suite boot --cpu rtl` on `machine_tb`
reaches the prompt and lists `dir`, with 113,547,658 instructions retired.
There is no oracle step, stop or stall, every VGA counter reads zero, and
the VGA trace replay matches Bochs.

## What slice 1 builds

- **[`pc386_system`](../../../sources/misteross/cores/fes-pc386/rtl/pc386_system.sv):**
  the machine and its audio from M4, with `fes_i386_cpu` on port 0.
  - The CPU's I/O and INTA cross a small handshake: one cycle at a time, the
    acknowledge a clock after the machine's done, IN data cut to size.
  - The machine's MMIO, DMA, FLUSH, A20M and INTR ports are wired to the
    CPU's.
  - The attribute map and reset state are `machine_tb`'s and Bochs's.
  - An 8042 or port 92h reset holds the CPU at least 4 clocks and until its
    memory is idle.
- **[`pc386_rom`](../../../sources/misteross/cores/fes-pc386/rtl/pc386_rom.sv):**
  96 KiB of M10K in front of port 0. It answers the L1s' reads of
  C0000h-C7FFFh and F0000h-FFFFFh in command order, until the CPU has an L2
  and its memory router.
- **[`pc386_host_link`](https://github.com/DeanoC/fes/blob/c97eee0b6f/sources/misteross/cores/fes-pc386/rtl/pc386_host_link.sv):**
  the M0 stand-in on port 2.
  - The HPS stages the ATA image and a configuration block (straps, disk
    geometry, wall clock) in the window, and the machine waits in reset for
    it.
  - The link writes a status block every 65,536 clocks, and the debug-port
    bytes to a 4 KiB ring.
  - The ATA image moves to port 2, as the design's table has it, as the
    arbiter's lowest-priority master.
- **[`system_tb`](../../../sources/misteross/cores/fes-pc386/sim/system_tb.cpp)
  and [`pc386-host`](https://github.com/DeanoC/fes/blob/c97eee0b6f/sources/misteross/cores/fes-pc386/host/main.go):**
  the same host in simulation and on a kit.
- **Sealer.** [`build_fes_pc386_quartus.py`](../../../sources/misteross/scripts/build_fes_pc386_quartus.py)
  seals `pc386_system`, its ROM read from the locked images. The summary
  records the fit's resources.

**The M4 diagnostic package is gone.** M5 replaced the bus master, and its
programs all passed on Kit B. Its last open exit, the disk round trip with
the runtime, waits on the M0 contracts with or without it. Removed:
`pc386_diag`, `pc386_bus_master`, `diag_tb`, `pc386_diag.py`, the kit tool,
and the nextpnr producer, whose lane #1235 parks.

The port fence goes too. It existed because the bus master reset the machine
without `fes_hps_ddr`'s execution hold. In `pc386_system` nothing resets the
machine outside that hold. The link's hold comes before any port traffic,
and a CPU reset leaves the machine and its ports running.

## System simulation

`python3 scripts/sim_fes_pc386.py --suite system`, at `c6003e5304` (the
29 October seal's RTL; `c97eee0b6f` changes only documents):
[`system-report.json`](system-report.json). Its benches took about 40
minutes of wall time.

On 31 October the suite ran again at `2c13827a2`, the merge of #1230's head,
in 16 minutes. Its bench was built from that RTL, the CPU with CR0's MP and
TS among it. Its report is byte for byte `system-report.json`: the same POST
and debug lines, prompt cycle, instructions retired, screen and status
block. Nothing the CPU gained since changed a cycle of this boot: not the new
instructions, and not the DMA and forwarding fixes.

| | Simulation, 61.44 MHz | Kit B, m5k1 |
| --- | --- | --- |
| Image and configuration written | cycle 48, as the link announced itself | 0.68 s after the host attached (copying 31 MiB) |
| BIOS lines | `BIOS BUILD DATE: 10/26/26`, `VGABios ID`, `ata0-0: PCHS=65/16/63`, `IDE time out`, `Booting from 0000:7c00` | the same five |
| Last POST code | 77h | 77h |
| Prompt on VRAM's last row | cycle 156,672,000, 2.55 s after configuration | 2.53 s after configuration |
| Instructions retired, at the end | 45,534,556 (2.6 s) | 45,710,361 (2.59 s) |
| CPU stops, CPU resets | 0, 0 | 0, 0 |
| Scanout, capture, presenter, plane, debug counters | all 0, none ever changed | all 0 |
| 720p frames shown | 156 | 155 |

The final screen's last 13 rows, from the FreeDOS 2043 banner to FreeCOM's
`C:\>`, are the M1 oracle boot's first 13, before it typed `dir`; the BIOS
banners and the disk line are still on screen above them. The
simulation's latency model is calibrated at 100 MHz, so it overstates DDR
latency at 61.44 MHz, but the two boots still finish within 1% of each
other.

### The scanout underflow before the VGA BIOS

The first run, before the fix, reached the prompt at the same cycle, but
counted one VGA scanout underflow, 6.5 ms in, long before the VGA BIOS ran.
A probe printed the raster at each underflow: every one, across seeds, had
htotal 5, hde 1, vtotal 2, vde 1. That is the CRTC's reset raster, which the
VGA follows by design (`vga_divergences.toml`, `reference-unprogrammed`).

- A text line's prefetch is two dependent reads: the character, then the
  font byte. A five-character line is about 100 clocks at 61.44 MHz.
- The DDR model's tail, 54-79 clocks at the 100 MHz calibration, can make
  two dependent reads exceed that.
- So the count moved with the model's seed (`system_tb --seed`). Before the
  change, seeds 0 to 5 counted one to six underflows in the first 0.1 s,
  every one before the VGA BIOS set mode 3.
  `machine_tb`'s merged-base boot at 100 MHz, with more clocks per dot,
  counted none.

No software asks for that raster, and a kit's status block would report the
same phantom underflows. So the scanout counts an underflow only once a CRTC
register has been written. The line still shows black as before. With the
change, seeds 1, 4 and 5 count none through the mode set, and the benches,
devices and vga suites pass, the vga replay with no mismatch. The bench
keeps its latency model, which is pessimistic at 61.44 MHz.

## Quartus seal

`make build-fes-pc386-quartus` at seed 1, both times from a clean checkout.
Each summary is the build's own record: timing, resources, firmware digests,
and every pinned input's hash.
- **31 October**, at `2c13827a2`, the merge of #1230's head:
  [`seal-summary-2026-10-31.json`](seal-summary-2026-10-31.json).
- **29 October**, at `c97eee0b6f`: [`seal-summary.json`](seal-summary.json).
  Kit B ran this package. It no longer describes the branch's RTL.

| | Seal, 31 October | Seal, 29 October | Earlier fit, Yosys `1f7d0273` | Earlier fit, with the underflow change |
| --- | --- | --- | --- | --- |
| Revision | `2c13827a2` | `c97eee0b6f` | `183dfbbc1b` | `9576db71ab` |
| Build ID, package | `951c8048…`, `15c5c86d…` | `0efa2213…`, `538773ad…` | `66121381…`, `3fa1455a…` | not published: the checkout was not clean at the end |
| System Fmax, slow corners | 71.30-73.80 MHz | 72.53-72.70 MHz | 72.58-73.34 MHz | 73.80 MHz |
| Pixel Fmax, slow corners | 78.40-79.37 MHz | 86.99 MHz | 82.06 MHz | 80.57 MHz |
| Setup slack: pixel, system | +0.709, +2.250 ns | +1.969, +2.489 ns | +1.278 ns worst | +0.933 ns worst |
| Hold slack | +0.086 ns | +0.093 ns | +0.069 ns | +0.113 ns |
| Recovery, removal, pulse width | +7.776, +0.370, +1.122 ns | +6.914, +0.266, +1.122 ns | +7.607, +0.551, +1.122 ns | +7.948, +0.266, +1.122 ns |
| ALMs | 24,004 (57%) | 23,147 (55%) | 23,156 (55%) | 23,145 (55%) |
| Registers | 22,426 | 22,315 | 22,150 | |
| RAM blocks | 251 of 553 (1,244,615 bits) | 251 of 553 (1,243,843 bits) | 251 | |
| DSP blocks | 27 of 112 | 27 of 112 | 27 | |
| RBF | 3,461,832 bytes, SHA-256 `82859bbf…` | 3,465,012 bytes, SHA-256 `97272a86…` | | |

The two earlier fits are timing data points only. Every fit has no illegal
or unconstrained clocks.

The 31 October seal is the first with the CPU work #1230 carries since the
last merge:
- BT, BTS, BTR and BTC; BSF and BSR; DAA, DAS, AAA and AAS.
- BOUND, with CLTS, WAIT and CR0's MP and TS.
- #1234's squash, and the DMA-ordering, store-forwarding and review fixes.

That costs 857 ALMs and 111 registers. The pixel clock's slack fell from
+1.969 to +0.709 ns, though no pixel-clock RTL changed. TimeQuest's worst
paths at the slow -40 °C corner, the worst for both clocks, lie outside the
CPU:
- **Pixel clock.** All eight worst paths run in the presenter's scaler, from
  `presenter_width` to `p_wx` and its duplicates. No presenter RTL changed,
  so the difference comes from the fit.
- **System clock.** The worst paths run in the VGA scanout, from `vcount` to
  `line_base`, at +2.250 ns. Against M4's shell (9,754 ALMs, 53 RAM, 21 DSP),
the CPU, its ROM and the host link add about 13,400 ALMs, 198 RAM blocks and
6 DSP blocks. The ROM takes 128 of those blocks: Quartus maps its 6,144 x
128 bits as 128 blocks of 8K x 1, where the design budgeted 96. RAM is not
the tight resource (251 of 553), so this stays as fitted for now.

### How the sealer builds the system

The M4 lane's steps carry over: the pc386 lock's Yosys elaborates everything
below the shell, memories with more than one write port and no `ramstyle`
become registers, and flattened names become plain identifiers.

- **No CPU memory falls under the register rule.** Only the machine's small
  register files do, as in M4. A Yosys dry run listed every `$mem_v2` the
  rule selects.
- **Quartus infers the ROM** as one 6,144 x 128 M10K ROM from Yosys's
  `initial` block.
- **The L1D's eight data arrays** carry byte-lane writes, which Quartus does
  not recognise in Yosys's form (warning 10999). It builds each as four
  byte-wide RAMs instead of registers. The L1I, the tags and the microcode
  ROM are inferred as written.
- **The build record** names the Yosys program with its source list as
  `$MACHINE_SOURCES` and records the list's SHA-256. The CPU's sources made
  the full program longer than a record parameter may be.

## On Kit B

Run m5k1, 29 October 16:12-16:13 EET. A core-only run: the package was
loaded through the agent's `/v1/development/core` under a kit lease, with no
bundle install and no image or boot change.

- The package loaded in 5.88 s.
- `pc386-host boot` found the link's status block, wrote the 31 MiB image and
  the configuration with the kit's wall clock, and followed the boot.
- POST codes and debug lines matched the simulation's, and the prompt was on
  VRAM's last row 2.53 s after the configuration.
- The status block at the end: 45,710,361 instructions retired, CS:EIP
  `F000:E857` (the BIOS's INT 16h handler returning "no key": FreeCOM polls
  for a keystroke), the last vector delivered 08h (the timer). Every underflow and drop counter read zero, no CPU reset, no stop.
- After the session the agent was idle with its lease free. The kit's image,
  boot state, launcher and software matched the probe, and the kit was logged
  FREE.

## Not done

- **No keyboard or mouse.** The HID row model is not built, so the kit run
  could not type at the prompt.
- **The `fes.computer` 1.1 contracts (M0)** will replace the host link,
  together with the disk round trip with the runtime.
- **The four games**, with both presets.
- **The i386 lock** still pins Yosys `238eac9a`, four commits behind the
  pc386 lock's `0a7927bf`, which now elaborates the CPU for the seal. The
  lock's rationale says the i386 lock should follow in its own change.
