# pc386 governor: ISA bus time for I/O and VGA memory, 3 November 2026

Issue [#1362](https://github.com/DeanoC/fes/issues/1362), a follow-up to the
speed governor ([#1357](https://github.com/DeanoC/fes/issues/1357),
[record](../2026-11-01-pc386-m5-governor/summary.md)).

**Result:** The governor now charges the period ISA bus time of I/O and
VGA memory accesses. On `pc386_system`, a port 61h toggle loop paces at
3.585 µs a pass at 286-12 and 2.881 µs at 386DX-25, within 0.04% of the
table. A 512-word text-mode fill runs at 2.40 MB/s and 2.39 MB/s, within
0.8% (one pass in a tick). The bench's charges equal the table on every
loop and preset, in the RTL and the netlist. Nothing architectural changes:
the lockstep at `max` equals main's, under 286-12 it equals `max`'s, and
FreeDOS boots on the RTL exactly as on main at `max` and still boots under
286-12. The seal (Quartus 17.0.2,
package `c86c5ebf…`) meets every analysis at 61.44 MHz. On Kit B at 286-12,
Ultima VI's opening-fight sounds are PIT tones at 213.1 Hz on this package
and on main's earlier seal alike. Its CPU-timed speaker routine did not
play, so the ~2.5% drop expected there is not measured. The 286-12
system-suite run finished its test and then hit the bench gap
[#1372](https://github.com/DeanoC/fes/issues/1372).

## What changed

The governor charged data-sheet counts, which assume a zero-wait-state local
bus, so an I/O instruction or a VGA memory access cost what it would on
system RAM. It now also charges the period AT's ISA bus time
([`governor.toml`](../../../sources/misteross/cores/fes-common/rtl/i386/governor.toml)'s
`[isa]`):

- **I/O.** An access to an 8-bit port takes one 8-bit ISA cycle per byte
  (a word two, a dword four), as the AT's bus controller splits it. The ATA
  data port 1F0h takes one 16-bit cycle per word. The charge is the cycles
  less the two clocks of the bus cycle the instruction's data-sheet count
  already holds. VGA ports are 8-bit ports here, as on ISA VGAs.
- **VGA memory.** An MMIO access (the machine maps only VGA memory,
  A0000h-BFFFFh, as MMIO) takes one VGA cycle per 16-bit half of the bus it
  touches: an aligned byte or word one, a dword two, a word at an odd
  address two. The charge is those cycles less what the table and the
  wait-state charge already give the access: two clocks, the preset's wait
  states, and on a 16-bit bus a dword's second cycle.
- **Cycles.** `[isa]` gives them in 120 ns ISA clocks (8.33 MHz, the
  machine's `pc386_io` clock): 8-bit I/O 6 (720 ns), 16-bit I/O 3 (360 ns),
  a VGA cycle 6. The governor turns them into reference clocks at the
  frequency it runs at, the preset's or the record's word-1 override, as it
  works out its step at reset. It rounds to the nearest clock, halves up, in
  a fixed point the generator checks against the exact figure at every
  frequency the record can hold. Neither charge goes below zero.

| Preset | 8-bit I/O cycle | 16-bit I/O cycle | OUT/IN of a byte (8-bit port): ISA charge | VGA word write: ISA charge |
| --- | ---: | ---: | ---: | ---: |
| 286-12 | 9 clocks | 4 | 7 | 6 (9 less 2 and 1 wait state) |
| 386SX-16 | 12 | 6 | 10 | 9 |
| 386DX-25 | 18 | 9 | 16 | 16 |
| 386DX-33 | 24 | 12 | 22 | 22 |
| 386DX-40 | 29 | 14 | 27 | 27 |
| 486DX-33 | 24 | 12 | 22 | 22 |
| 486DX2-66 | 48 | 24 | 46 | 46 |

At 286-12 the 8-bit cycle rounds from 8.64 to 9 clocks: 750 ns, the cycle
of a 12 MHz AT whose bus ran at 8 MHz.

**Where it is worked out.** WB knows the committed uop's I/O port and size,
and the attribute of its operand's physical address (a new register,
`wb_mp`, beside the store's `wb_p0`) and of a `st2` store's. The governor
registers them with the rest of the retirement, classifies the port
(`port16()`, generated from `[isa]`) and works out both charges in the stage
that forms the ROM address, so the hold still follows a retirement by five
clocks. `trace_gov_charge` widens to 13 bits.

**Not covered: an access across the window's edge.** The MMIO class is the
access's first byte's. An unaligned word or dword that crosses A0000h or
C0000h counts whole: one starting at 9FFFFh is not charged, and one at
BFFFFh is charged as VGA memory although its tail is in the VGA BIOS's ROM.
Codex raised this in review. Splitting it would need the operand's next-line
attribute, as the store path has (`wb_at1`), and a post-seal RTL change. No
period program is known to make such an access, so it is documented in the
design's "Speed control" and left.

**Device time.** The machine's own device time is unchanged: 720 ns for an
8-bit port, local-bus time for VGA memory and ports. While the governed CPU
is ahead of the reference, device time spends that lead, so a loop of port
accesses takes the larger of its charged time (now with the ISA cycles) and
its device time, not their sum.

The design's ["Speed control"](../../../sources/misteross/docs/i386-cpu-design.md#speed-control)
section now says where this time belongs, and the machine design's I/O
timing and rejected-alternatives text points at it.

## The period figures and their sources

| Figure | Source |
| --- | --- |
| An AT inserts one wait state on a 16-bit device's bus cycle (3 bus clocks) and four on an 8-bit device's (6), on memory and I/O | M. Abrash, ["Demystifying 16-Bit VGA"](https://jacobfilipp.com/DrDobbs/articles/DDJ/1990/9005/9005f/9005f.htm), *Dr. Dobb's Journal*, May 1990: "the bus inserts one-wait-state on each access to most 16-bit devices", and for 8-bit adapters "three extra wait states (in addition to the one-wait state that's routinely inserted), effectively doubling the access time per byte of such adapters to six cycles"; the ISA bus references in the [pc386 M1 record](../2026-10-26-pc386-m1-freedos-boot/summary.md#io-timing) (6 and 3 BCLK) |
| The ISA bus near 8 MHz whatever the CPU; 720 and 360 ns cycles | The machine's `isa8`/`isa16` classes ([pc386 machine design](../../../sources/misteross/docs/pc386-machine-design.md#io-timing)), checked against published figures in the M1 record |
| ISA VGA held the CPU on every display memory access | Abrash, as above: "three wait states per byte-sized access to display memory on an AT and often more in an 80386-based machine"; VGA I/O "subject to the same 8-bit device wait states as memory accesses" |
| A 720 ns VGA cycle per 16-bit half: 2.8 MB/s of word writes at most, 2.4 MB/s for REP STOSW at 286-12 and 386DX-25 | Vidspeed write rates of 16-bit ISA VGAs with the bus at 10 MHz: 1.2 MB/s (Trident TVGA8900B) to 3.7 MB/s (Tseng ET4000AX), 2.2 (Cirrus CL-GD5401) and 3.0 (ATI 28800) between ([os2museum.com, "Fast Unaccelerated VGA?"](http://www.os2museum.com/wp/fast-unaccelerated-vga/), October 2015). At 8.33 MHz those scale to about 1.0-3.1 MB/s; 2.4 is a mid-range card |
| Instruction counts | The 80286 and Intel386 columns of `governor.toml`, whose header names its sources |

## Inputs

- **Branch.** FES `deano/pc386-isa-time`, from main `9ef805e82`:
  `faad373dc` (the charges), `10f2e27d7` (the bench's interrupt test waits a
  period after the program arms it) and `667d1a5e0` (the lockstep's
  retirement bound under a preset).
- **Seal.** `fes.pc386` from a clean checkout of `10f2e27d7`, package
  `c86c5ebf…` ([Seal](#seal)).

## Checks

- **Charges and pacing.** The governor bench
  (`scripts/test_fes_i386_governor.py`) gains two loops: `vram` (REP STOSB,
  STOSW and STOSD into A000h, word and odd-address word stores, a load, an
  RMW, REP MOVSW from RAM and REP MOVSB within VGA memory) and `iobus` (IN
  and OUT of a word and a dword at the ATA data port and at an 8-bit port,
  REP INSW and REP OUTSB). Its existing `io` loop toggles port 61h. Every
  loop's charge equals the table's on every preset; every loop at presets
  1-5 is within 0.13% of its table time (budget 5%); the locked Yosys's
  netlist charges the same on every loop and preset
  ([`governor-bench-report.json`](governor-bench-report.json),
  [`governor-bench-netlist-report.json`](governor-bench-netlist-report.json)).

  | Loop | Preset | Cycles | Charged = table | Error |
  | --- | --- | ---: | ---: | ---: |
  | io (2,000 port 61h toggles) | 286-12 | 358,476 | 70,009 | +0.01% |
  | | 386DX-25 | 324,523 | 132,025 | +0.02% |
  | vram | 286-12 | 702,475 | 137,209 | −0.01% |
  | | 386DX-25 | 635,991 | 258,825 | −0.02% |
  | iobus | 286-12 | 977,951 | 191,009 | −0.00% |
  | | 386DX-25 | 929,055 | 378,025 | +0.00% |

  Main's `io` loop charged 42,002 at 286-12 and 68,009 at 386DX-25: a
  toggle pass now costs 35 clocks at 286-12 (2.92 µs) against 21, and 66 at
  386DX-25 (2.64 µs) against 34.
- **Interrupt latency.** As before, INTR is raised while a loop of REP
  MOVSB, DIV, PUSH and POP runs; the bound is the window, the loop's largest
  single charge and the delivery's charge. The testbench used to raise the
  first INTR as the program armed it, so the first delivery waited out the
  debt the bench's earlier loops had left, which the new `iobus` loop makes
  large: 184.6 reference clocks at 486DX2-66. It now waits a period after
  the mark, as it does after each handler's.

  | Preset | INTRs | Max latency | Mean | Bound | Debt max |
  | --- | ---: | ---: | ---: | ---: | ---: |
  | 286-12 | 113 | 84.8 | 67.4 | 115 | 163 |
  | 386SX-16 | 84 | 104.9 | 83.1 | 128 | 195 |
  | 386DX-25 | 38 | 81.4 | 68.3 | 123 | 243 |
  | 386DX-33 | 28 | 84.0 | 53.9 | 123 | 293 |
  | 386DX-40 | 23 | 81.4 | 66.7 | 123 | 335 |
  | 486DX-33 | 22 | 44.4 | 37.6 | 118 | 283 |
  | 486DX2-66 | 11 | 97.7 | 51.0 | 118 | 485 |

- **Architectural invisibility.**
  - At `max` the branch's full lockstep (`test_fes_i386_lockstep.py`)
    equals main's at `9ef805e82` in every reported field, cycle counts
    included: 0 of 7,093 leaves differ. Corpus 1,758,699 states, all pass,
    174,987,765 cycles; random 32,000 programs, all pass, 115,132,760
    cycles ([`lockstep.json`](lockstep.json)).
  - Under 286-12, `test_fes_i386_lockstep.py --preset 1 --same-as <max
    report>` passes, and every architectural result equals `max`'s: the
    1,758,699 corpus states, the 32,000 random programs, the protected-mode
    programs, faults, interrupts and every corpus file's and seed's trace
    hash. The corpus took 214,761,555 cycles against 174,987,765, the random
    programs 803,152,306 against 115,132,760; replays and restarts for
    self-modifying code moved with the timing (107,864 and 12,390 against
    109,525 and 12,391).
  - The first run under 286-12 timed out 84 corpus states, all REP INSD,
    OUTSD and MOVSD in the MMIO window (`66 6D`, `66 6F`, `66 A5`, with and
    without `67`). The testbench gives a retirement 20,000 cycles; the ISA
    time holds such a string for real time. Across those six corpus files
    the longest retirement took 31,668 cycles at 286-12 and 30,890 at
    486DX2-66, against 1,894 at `max`. Under a preset the bound is now
    80,000 (`667d1a5e0`); at `max` it stays 20,000. It is the testbench's
    hang bound, not a timing check, and the full-corpus run above under
    286-12 is what checks it.
- **System suite.** `cores/fes-pc386/sim/governor_test.asm` boots from a
  1 MiB disk on `pc386_system`, the preset set in the restored record
  (`sim_fes_pc386.py --suite governor`,
  [`system-governor-report.json`](system-governor-report.json)). After its
  Turbo Pascal–style CRT calibration it now counts two more loops' passes in
  a timer tick:
  - **speaker:** IN AL,61h; XOR AL,2; OUT 61h,AL; a pass counter and the
    tick compare, as a game's speaker loop toggles the speaker gate;
  - **vram:** REP STOSW of 512 words to B800h:1000h, the counter and the
    compare: a text-mode screen fill.

  The "table" columns are the reference machine's passes in one tick
  (65,536 PIT clocks): the preset's frequency over the pass's data-sheet and
  ISA cost.

  | Preset | CRT passes / table | Speaker passes | Table | VRAM passes | Table |
  | --- | --- | ---: | ---: | ---: | ---: |
  | `max` | 224,950 | 26,994 | — | 2,008 | — |
  | 286-12 | 27,453 / 27,463 (−0.04%) | 15,322 | 15,328 (−0.04%) | 129 | 128.1 (+0.72%) |
  | 386SX-16 | 39,932 / 39,946 (−0.03%) | 14,402 | 14,407 (−0.03%) | 115 | 114.0 (+0.84%) |
  | 386DX-25 | 65,374 / 65,387 (−0.02%) | 19,068 | 19,071 (−0.02%) | 128 | 127.4 (+0.46%) |
  | 386DX-40 | 104,608 / 104,620 (−0.01%) | 23,370 | 23,372 (−0.01%) | 134 | 133.9 (+0.08%) |

  A VRAM pass is about 1/128 of a tick, so one pass either way is 0.8%; the
  budget is 5%. The governed CRT counts equal the
  [M5 governor record's](../2026-11-01-pc386-m5-governor/summary.md) to
  within a pass: the calibration loop touches neither I/O nor VGA memory.

  So at 286-12 a speaker toggle pass takes 3.585 µs and at 386DX-25 2.881
  µs; a 512-word fill takes 426 µs (2.40 MB/s) and 429 µs (2.39 MB/s). Each
  speaker pass holds two 8-bit ISA cycles: 1.5 µs of 750 ns cycles at
  286-12, 1.44 µs of 720 ns ones at 386DX-25.

  **The 286-12 run hit [#1372](https://github.com/DeanoC/fes/issues/1372).**
  It finished the test, printed its figures above (`GOV TP=00006b3d
  DC=01f3 C18=00078a2c`, `GOV SPK=3bda VRAM=0081`, `GOV DONE`) and passed
  every check through Stop, the save and the record, whose CMOS 70h-7Bh hold
  TP 27,453 and DC 499. In the record's round trip `system_tb` then threw
  `port 2: a write burst cut short, then another begun at clock 205827804`
  and ended without `END`, so the suite counts the run unfinished and fails.
  That is #1372's bench gap (a hold inside a VGA capture burst, which the
  guard finishes on a kit); this change moves where the hold falls. The
  figures above for 286-12 are from its log; the other four runs pass.
- **FreeDOS on the RTL.** The RTL machine sim (`machine_tb`, `--suite boot
  --cpu rtl`) boots at `max` on the branch and on main at `9ef805e82` with
  identical reports (0 of 139 fields differ) and byte-identical VGA traces:
  `dir` at the prompt at 4.1 simulated seconds, 113,098,555 instructions
  retired, the RTL alone, the VGA replay matching
  ([`boot-rtl-max-report.json`](boot-rtl-max-report.json)). Under 286-12
  (`--preset 1`) it passes too: `dir` at the prompt at 14.1 simulated
  seconds, the RTL alone, the VGA replay matching, 30,799,875 instructions
  retired ([`boot-rtl-286-12-report.json`](boot-rtl-286-12-report.json)).
  The M5 governor record's 286-12 boot, before ISA time and on an older
  main, took 14.0 s and 30,826,678 instructions.
- **Unit tests.** `tests/test_fes_i386_governor.py` (12, four new: the
  ISA cycles' rounding at every frequency, the charges, the generated
  `port16()`, a bad `[isa]` refused), `test_fes_i386_lockstep.py` and
  `test_build_fes_pc386_quartus.py` (44 together, one skipped),
  `test_pc386_machine.py` (36) and `fes_i386_governor.py --check` pass.

## Seal

`make build-fes-pc386-quartus SEED=1` with Quartus Prime Lite 17.0.2, from a
clean checkout of `10f2e27d7`
([`seal-summary.json`](seal-summary.json)). Every analysis is met at
61.44 MHz.

- Package `c86c5ebff5149f8b45cfc2b4032fc860aae8e2517b98e980e994e26846e7a85d`,
  build `845ff7c7011d5123ae006f994fbc582b`, RBF `ff52e23f…` (3,620,196
  bytes).
- Worst slacks: setup +1.198 ns, hold +0.077 ns, recovery +7.214 ns,
  removal +0.273 ns, minimum pulse width +1.122 ns; no negative TNS. The
  slow 100 °C model's system-clock Fmax is 69.43 MHz.
- 28,998 of 41,910 ALMs, 26,032 registers, 261 of 553 RAM blocks,
  1,275,417 block memory bits, 36 of 112 DSP blocks.
- Machine elaborated by Yosys `0a7927bf1`; firmware BIOS `fd0df1f9…`,
  VGABios `69401dc8…`.

`667d1a5e0`, after the seal, changes only the lockstep testbench.

## Kit B: Ultima VI's speaker at 286-12

Run `u6a`, 3 November 14:52-15:09 EET, claimed and released in
`kitB-STATUS.md`. It was core-only on the kit's baseline software `b3444abf`
(main `21d57d7a`), image `f904961e`. Nothing was installed and there was no
reboot. The agent launched each package as a fresh library game under a kit
lease, with the record's word 0 set to 1 (286-12):

- this branch's seal, `c86c5ebf…`;
- main's earlier seal, `c7e06e04…`
  ([games record](../2026-11-02-pc386-games-u7/summary.md)), for comparison.

The disk was the FreeDOS games image (`e66e6348…`) plus the user-supplied
Ultima VI from eXoDOS (`5ec54a04…`), its `CONFIG.U6` set to music `n`
(none), so every sound goes through the PC speaker (disk `00246ed2…`). Each
session started a new game and recorded 67 s of HDMI audio (ALSA, 48 kHz)
from the game's start, the gargoyle fight at the shrine; its grabs show the
fight on both packages. The audio and frames stay on Powerboat (WAV
`9a539243…` this branch, `c22f51f1…` main); only these measurements are
here.

| Package | Tones (150-155 ms) | Pitch, median per period | Clicks |
| --- | ---: | --- | ---: |
| this branch, `c86c5ebf` | 6 | 213.0-213.1 Hz | 2 |
| main, `c7e06e04` | 4 | 213.1-213.2 Hz | 0 |

The pitch is an edge-timed median over each tone's 28 periods. The clicks
are single steps of the speaker gate. A PIT divisor of 5,600 gives
213.07 Hz, so these combat sounds are PIT-pitched: neither the CPU nor the
bus sets their pitch, and their 150-155 ms length (a tick-calibrated delay)
is the same on both packages.
How many sounds a session plays depends on how the fight goes.

GAME.EXE also has a CPU-timed speaker routine. It loads PIT channel 2 with
60 (a 19.9 kHz carrier) and gates port 61h from a phase accumulator, one
step per pass of a loop. Each pass holds an IN and an OUT of port 61h and a
delay of `[2FC6h]`/24 inner passes, where `[2FC6h]` is a tick-calibrated
count of INC/CMP/JE passes. The delay scales with the CPU and the I/O does
not. Worked out from the code, not measured: at 286-12 a pass is about 89
clocks plus about 475 per delay unit. The two 8-bit cycles' charge (7
clocks each) should lower those sounds' pitch by about 2.5% at one delay
unit. None of them played in the capture, so there is no measured figure.
Whether either package sounds right is Deano's call by ear.

The run left 21 paths in FogCast's data directories on Kit B (listed in the
status log): the two sessions' game records, their media deltas and the
staged disk image, which holds the game. They have not been removed.
