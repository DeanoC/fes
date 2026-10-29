# pc386 games on the RTL CPU

Dated record, 2026-10-28. Host simulation only. No package, RBF or kit.

The next step of pc386's M5 after the [FreeDOS boot](2026-10-28-pc386-rtl-boot.md):
what the first Phase 3 games need from [`fes_i386_cpu`](../../cores/fes-common/rtl/i386/),
measured the way the boot was. Each game's instruction mix comes from a
`--census` run under the Bochs oracle, then the game runs on the RTL. Nothing
in the CPU changed.

- The CPU is `1814f0c590`, byte for byte, as in the boot record.
- The games are the M3 acceptance setups
  ([record](../../../../docs/validation/2026-10-26-pc386-m3-audio/summary.md)),
  user-supplied from the eXoDOS 6.04 collection and copied onto a copy of the
  FreeDOS fixture outside the repository. Nothing from them is committed.
  - **Ultima VI**: `Ultima VI - The False Prophet (1990).zip`, SHA-256
    `5ec54a045d18ccea63fe35e857e9c0294427e06d397370433d4a7c5add881cb9`, 324
    files. `cd ultima6`, `ultima6`, no keys, 16 simulated seconds.
  - **Wolfenstein 3D**: `Wolfenstein 3D (1992).zip`, SHA-256
    `f451d8538266fb9f12a4fa8909a671b37d354de388a9b76d1c1033a6a541ed37`, 11
    files. `WOLF3D.EXE` is FreeDOS's shell; Enter every 1.5 s from 4 s into
    E1M1, then Ctrl and space every 0.8 s from 17.5 s. 24 simulated seconds.
- The censuses and the first Wolfenstein 3D RTL run are of `d8b0e63907`:
  the censuses are its `machine_tb` on the game suite's arguments, and the
  RTL run is through `scripts/sim_fes_pc386.py`. That commit predates the
  base branch's FIFO wait rule (`42dea8a0d3`), which changes only how the
  bench reports a VGA port write's time, not what the machine does. The
  oracle Wolfenstein 3D runs on the merged tip `1187fefd6d` check that rule
  against the game and the game's timing (below).
- The Wolfenstein 3D RTL run with a retire history is of `9899d39f53`, which
  adds `--stop-history` to the bench.
- The Ultima VI and boot RTL runs are of `d6fb3af33d`, which fixes the
  bench's data port (below), through the runner. So is the first Wolfenstein
  3D run on the fixed port, run directly.
- Wolfenstein 3D's runs to 14.4899 s, just past where it first stopped at
  that time, are of `69de303890` (RTL and oracle), which prints each overlap
  as it happens; `e41dd6a36d`, which adds registers to the history and
  `--stop-dump`; `678f9aa62c`, which adds `--trace-port`; and `037248ffae`,
  which fixes the port a second time (below). The RTL figures to 14.4899 s
  are of `037248ffae`.
- Run data: [`2026-10-28-pc386-game-census.jsonl`](2026-10-28-pc386-game-census.jsonl):
  a summary line per run, and the censuses, a line per stem.

## Result

**Wolfenstein 3D runs 14.49 s on the RTL, into E1M1, then goes astray.**

- **First crash, at 12.127 s, was the bench's.** The first run stopped as
  E1M1 started, running ASCII text as code. The oracle runs of the game at 3,
  4 and 5 clocks an instruction all pass that point, so the game's timing
  did not cause it. A retire history traced it to the bench, not the CPU. The
  bench's data port gave the bytes of a VGA load the RTL had abandoned to the
  read of IRQ0's vector. A rerun that prints each overlap shows that read
  going out while the abandoned load's read was still out.
- **Second crash, at 14.490 s, was the bench's too.** With the port fixed
  (`d6fb3af33d`), the game runs past 12.127 s with no stop until 14.490 s.
  There a RETF at the end of one of its compiled scalers returns to
  0101:0108, in DOS's data, and the RTL stops on x87 escapes there. The CPU
  had left its path 125 instructions earlier, when the sprite scaler's loop
  read 1D1Dh where its span list holds the 0000h that ends it. A trace of
  the data port shows where those bytes came from: a VGA read M1 had sent
  for a wrong-path instruction that D had already marked faulted, which M2
  never waits for. The port held the span-list read behind that read and
  gave it the VGA's bytes. `037248ffae` fixes the port, and the game then
  reaches 14.4899 s with no stop. That M1 sends a faulted uop's load at all
  is the CPU's: it puts a read nothing waits for on the bus, which on this
  bench loads the VGA's latches (below).
- **The garbled status bar is the CPU's missing uncached path.** The game
  copies its numbers, face and weapon picture to the screen through the
  VGA's latches, with a REP MOVSB in write mode 1. On the RTL each element's
  store reaches the VGA after the read two elements on, so each byte takes
  the latches of the source byte two ahead. That fits every byte the bar's
  copies drew but the face's, whose animation frame may differ from the
  oracle's.

**Ultima VI runs its 16 s on the RTL with no stop**, and its final frame is
the oracle's but for the intro's animated TV picture.

**Neither game's oracle run needs an instruction or a mode the CPU has not
built.** Every stem either game retired under the oracle is one the
lockstep counts as built (`test_fes_i386_lockstep.py`'s `SCOPE`), all of it
in real mode. Neither RTL run entered protected mode or V86 before
Wolfenstein 3D went astray.

| | Ultima VI | Wolfenstein 3D |
| --- | ---: | ---: |
| Instructions (interrupt deliveries) | 307,159,964 (761) | 532,700,506 (15,046) |
| Stems | 295 | 327 |
| Stems the boot did not use | 14, 1,954,299 instructions | 47, 7,837,352 instructions |
| Stems not built | 0 | 0 |
| Protected-mode or V86 instructions | 0 | 0 |
| 66h-prefixed | 517,462 (0.17%) | 53,493,182 (10.04%) |
| 67h-prefixed | 0 | 0 |

What the runs reach limits this. Ultima VI's 16 seconds end in its intro,
before character creation or play. Wolfenstein 3D's end in E1M1, with the
pistol fired empty and the knife drawn. Ultima VII is not here: its
memory manager needs HIMEMX and 2 MiB of XMS, which the fixture lacks, and it
enters unreal mode.

On the RTL, with Wolfenstein 3D's runs to 14.4899 s (the RTL's of `037248ffae`):

| | Ultima VI, oracle | Ultima VI, RTL | Wolfenstein 3D, oracle | Wolfenstein 3D, RTL |
| --- | ---: | ---: | ---: | ---: |
| Instructions | 307,159,964 | 292,535,229, 0 on the oracle | 306,091,197 | 327,946,430, 0 on the oracle |
| `C:\>` first shown | 2.400 s | 2.350 s | none (the game is the shell) | none |
| ISA I/O, every access at its class time | isa8 233,881 at 74; isa16 270,336 at 38 | isa8 238,030; isa16 270,336 | isa8 667,209; isa16 354,304 | isa8 751,412; isa16 354,304 |
| VGA port reads | 41,245,631 | 29,808,910 | 25,242,160 | 18,227,405 |
| VGA port writes over their class time, net of FIFO waits | none | none; 1 byte and 1 word waited 6 clocks (`IOWAIT`) | none; 36 words waited 4 to 110 clocks | none; 31 bytes waited 6 to 8 clocks, 189 words 1 to 136 |
| INTA, all at 146 clocks | 761 | 770 | 8,336 | 8,393 |
| OPL2 writes; samples equal to Nuked-OPL3's | 3,813; all 795,454 | 3,895; all 795,454 | 1,270; all 720,378 | the oracle's 1,270, one pair in another order; all 720,378 |
| Sound Blaster | not used | not used | 1 reset, 3 commands | the same |
| VGA loads that never retired | — | 26 of 139,556 | — | 179 of 200,031 |
| Final frame | the intro's den, the TV on | the same; differs only inside the TV picture (below) | E1M1's first view, fading in | the same view, further into the fade; the status bar garbled (below) |

The games run to the time limit, so the counts are of different amounts of
work on each CPU and say nothing about correctness. The final frames do:
in Ultima VI, 2,172 of the 128,000 pixels differ, all inside the TV's
picture (x 233-282, y 104-173 of 320 × 400), which the intro animates; the
rest of the frame is the oracle's, pixel for pixel.

In Wolfenstein 3D both frames show E1M1's first view, the door ahead and a
dead guard before it. 120,780 pixels differ, nearly all by a few levels of
brightness: the RTL is further into the level's fade-in (34h against 30h,
69h against 61h in each channel). Mapping each of the oracle's colours to
the RTL's commonest at the same pixels leaves 2,298 pixels, all in the
status bar (x 24-297, y 328-391): its numbers, face and weapon picture are
garbled. FLOOR's 1 shows as two bars 4 pixels apart, and the 0, 3, 100% and
8 are doubled the same way; the face's right half repeats in 4-pixel
strips. On the port before `037248ffae`, 24 more were one column of the dead
guard's sprite (x 152-153, y 232-243), light grey where the oracle's are
dark, drawn from a span the stray bytes below had corrupted. The frame is
otherwise the same on both ports, byte for byte.

The game draws these with latch copies: a REP MOVSB from a copy of each
picture in the VGA's memory to the screen, in write mode 1. Each element's
read loads the VGA's four latches, one byte of each plane, 4 pixels, and its
store writes them out. A MOVSB element is one uop (st2) that reads at M1 and
writes at WB, and a VGA read freezes D to WB until it is answered. So
element n's store waits in WB while element n+2's read is out, and writes on
the edge M1 sends element n+3's. The VGA sees R0, R1, R2, W0, R3, W1 and so
on: each byte takes the latches of the source byte two ahead, and a row's
last three bytes all take its last. Taking the oracle's screen as the
source, that fits every byte the bar's copies drew but the face's: the
digits' and the key slot's 960 and the weapon picture's 576, which one
element ahead fits in only 444. The face fits in 272 of 384, but its
animation frame may differ from the oracle's. Its last three bytes are
equal in every row, as the model has it, and in none of the oracle's. No
other byte of the bar differs.

This is the design's known gap, no uncached path, not the bench's: the port
keeps the CPU's order, a store before a load sent on the same edge. The
design's uncached loads issue only when they are the oldest instruction,
after every older store has written.

## The crash at 12.127 s, and the bench's data port

The first Wolfenstein 3D run on the RTL (`d8b0e63907`) ran with no stop
until cycle 1,212,734,685 (12.127 s), as E1M1 started. Then the RTL ran
ASCII text as code:

| Cycle | CS:EIP | Bytes | Stem |
| ---: | --- | --- | --- |
| 1,212,734,685 | 0206:2C0B | `64 62 61 61` ("dbaa") | `62` BOUND, FS: |
| 1,212,734,734 | 0206:2C29 | `3F` ("?") | `3F` AAS |
| 1,212,735,604 | 0206:2C67 | `63 00` ("c") | `63` ARPL |
| 1,212,771,331 | C000:003A | `63 74 20` ("ct ") | `63` ARPL |
| 1,212,774,444 | C000:3333 | `FF FF` | `FF /7`, invalid |
| 1,212,775,558 | C000:3526 | `D8 6C 6C` | `D8 /5`, x87 |
| 1,212,780,066 | C000:0EFE | `0F 0B` | `0F0B` UD2 |
| 1,212,785,273 | C000:7FFF | `D9 00` | `D9 /0`, x87 |

Those are 8 of its 21 stops; the jsonl has them all. C000:0022 to 0040
holds the VGA BIOS's name and date ("Bochs VGABios", "0.9d 26 Oct 2026"),
and C000:7FFF is the ROM's last byte. The oracle step for the last stop
never returned (below), and the run was stopped.

The oracle runs the game at 4 clocks an instruction. Run at 3 and at 5 to
14 s (`1187fefd6d`, 380,873,221 and 244,005,152 steps), its OPL2 writes per
half second match the 4-clock run's through 13.5 s, the level music from
12 s included. So do the RTL's, to the crash; the level music's burst of
writes starts just after it. So the game reaches the level on the oracle
whether its CPU is faster or slower, and the crash is not the game's
timing.

A rerun to 12.1278 s with `--stop-history 256` (`9899d39f53`) stopped first
at the same cycle, so the run is deterministic. It shows the path into the
first stop:

| Cycle | Retired | Bytes | Then CS:EIP | SS:SP | Delivered |
| ---: | --- | --- | --- | --- | --- |
| 1,212,734,243 | 2C0B:08F5 | `F3 A4` REP MOVSB | 2C0B:08F7 | 4993:FF80 | |
| 1,212,734,249 | 2C0B:08F7 | `01 DF` ADD DI,BX | 2C0B:08F9 | 4993:FF80 | |
| 1,212,734,250 | 2C0B:08F9 | `4A` DEC DX | 2C0B:08FA | 4993:FF80 | |
| 1,212,734,251 | 2C0B:08FA | `75 F7` JNZ | 2C0B:08F3 | 4993:FF80 | |
| 1,212,734,262 | 2C0B:08F3 | `89 C1` MOV CX,AX | 2C0B:08F5 | 4993:FF80 | |
| 1,212,734,557 | (2C0B:08F5 next) | | 2B0F:0097 | 4993:FF7A | IRQ0, vector 08h |
| 1,212,734,657 | 2B0F:0097 | `E6 20` OUT 20h,AL | 2B0F:0099 | 4993:FF7A | |
| 1,212,734,663 | 2B0F:0099 | `58` POP AX | 2B0F:009A | 4993:FF7C | |
| 1,212,734,674 | 2B0F:009A | `CF` IRET | 0206:2C0B | 4993:FF82 | |

The game copies VGA memory to VGA memory a row at a time, with DS and ES at
A000h: a mode-X copy through the latches. IRQ0 arrived with the next REP
MOVSB still to run. The RTL entered the timer handler at 2B0F:0097, the tail
of a handler (`OUT 20h,AL`, `POP AX`, `IRET`), not its start. POP AX took the
pushed IP (EAX is 08F5h at the stop). IRET then took the pushed CS (2C0Bh)
as IP and the pushed FLAGS (0206h) as CS.

The vector the RTL read was wrong, and the bench put the wrong byte in it.
M1 can send a load on the edge WB flushes for an interrupt, and the
MOVSB's load from A000:C931 went out on that edge: the bench's count shows
4 VGA loads at 2C0B:08F5 lost to a restart, the first at cycle
1,212,734,297. The bench queued the load's read for the VGA and held
`dmem_valid` low until it came back. The CPU's next load was the delivery's
read of vector 8 at 0000:0020, and the bench gave it its bytes only once the
VGA read returned, merged in: the VGA byte replaced lane 1, which in the
vector is the high byte of IP. The RTL took 2B0F:0097 where the vector
held some other 2B0F:xx97.

A rerun on `69de303890`, which prints each overlap as it happens, shows it.
At cycle 1,212,734,504 the CPU issued a load from physical 20h, vector 8,
while the abandoned read of AC934h, the MOVSB's, was still out. It was the
first of 8 such reads of vector 8 in the run, each of which the old port
would have corrupted. Of the run's other 28 overlaps, 12 are of 2B0F:000E,
in the timer handler's segment. The other 16 are of the game's data at
57713h-598B8h, within 145 µs at 13.47 s.

`d6fb3af33d` fixes the port. A VGA load whose uop dies on the edge M1 sends
it still goes to the VGA and loads its latches, as it would with no
uncached path, but its bytes go to no load and the port is free at once.
The `RTL` line now counts such loads (`vga_cancelled`) and the loads issued
while one's read was still out (`vga_overlaps`), which the old port gave the
wrong bytes; `OVERLAP` lines give the first of them. Nothing in the CPU is
implicated: an L1D drops a request on a flushing edge, and the design's
uncached loads wait until they are the oldest. The Ultima VI and boot RTL
results are of `d6fb3af33d`.

## The stop at 14.490 s

On the fixed port (`d6fb3af33d`) Wolfenstein 3D ran past 12.127 s with no
stop. The first stop came at cycle 1,448,989,946, on `D9 00` at 0101:010C.
The second came 856 cycles later, on `D9 00` at 0101:0110, and the oracle
step for it never returned (below). A rerun to 14.4899 s (`69de303890`)
stopped first at the same cycle, with the same registers. Its
4,096-retirement history shows where the CPU left its path, 125
retirements before the stop. That was in the game's sprite scaler,
`ScaleLine` (`WL_SCALE.C` in its published source):

| Cycle | CS:EIP | Bytes | Instruction |
| ---: | --- | --- | --- |
| 1,448,989,416 | 2000:036D | `3E 8B 5E 00` | MOV BX,DS:[BP]: the span list's next entry |
| 1,448,989,421 | 2000:0375 | `26 8B 1F` | MOV BX,ES:[BX]: the code offset that ends the span |
| 1,448,989,425 | 2000:0378 | `26 8A 17` | MOV DL,ES:[BX]: save the byte there |
| 1,448,989,426 | 2000:037B | `26 C6 07 CB` | MOV BYTE ES:[BX],CBh: a RETF over it |
| 1,448,989,428 | 2000:037F | `3E 8B 76 04` | MOV SI,DS:[BP+4] |
| 1,448,989,432 | 2000:0383 | `26 8B 04` | MOV AX,ES:[SI]: the code offset that starts the span |
| 1,448,989,433 | 2000:0386 | `36 A3 E7 DD` | MOV SS:[DDE7],AX |
| 1,448,989,435 | 2000:038A | `3E 8B 76 02` | MOV SI,DS:[BP+2] |
| 1,448,989,436 | 2000:038E | `83 C5 06` | ADD BP,6 |
| 1,448,989,438 | 2000:0391 | `B8 00 A0`, `8E C0` | MOV AX,A000h; MOV ES,AX |
| 1,448,989,449 | 2000:0396 | `36 FF 1E E7 DD` | CALL FAR SS:[DDE7], to 614F:A103 |
| 1,448,989,532 | 614F:A103 | `03 88 03 9A` | ADD CX,[BX+SI+9A03h] |

ES before the call is the scaler's segment, 614Fh. The five calls before
this one in the history went to 614F:0323, 0316, 0316, 0355 and 0316. The
spans they ran ended at RETFs the loop had patched in, the last at
614F:03A1. 614F:A103 is 40 KB further on, in another scaler's tables:
offsets into its code (0388h, 039Ah, 03ACh, ...) and then pixel widths (3,
3, 4, ...). The CPU ran those tables as code, a byte off, and slid into that
scaler's code. On the way, `03 E7` (ADD SP,DI) made SP odd. The scaler's
RETF then took 0101:0108 from the stack.

So the offset AX carried to the call, A103h, was wrong. As bytes it is
03h A1h: a 03xxh offset's high byte, then 03A1h's low byte.

A rerun with registers in the history and memory dumps at the stop
(`e41dd6a36d`) stopped at the same cycle. Those loads read what memory
holds. SI was 007Dh, from DS:[BP+4] = 97D0:0244, which holds 007Dh, and AX
was A103h, from ES:[SI] = 614F:007D, an odd offset into the scaler's table
of code offsets, whose bytes there are 03h A1h. The fault is earlier. Each
entry of a column's span list is three words: the table offset of the row
that ends the span, the source offset, and the table offset of the row that
starts it. A zero word ends the list. BP was 0240h, just past the previous
entry at 97D0:023A, where that column's list ends with its zero. MOV
BX,DS:[BP] at 036D should have read 0000h and left the loop. It read 1D1Dh.
The loop then took the next column's words a word out of step, and its odd
start offset.

That was the second time. The history holds nine passes of that load:

| Cycle | BP | Address | Read | Memory at the stop | Clocks since the last retirement |
| ---: | ---: | ---: | ---: | ---: | ---: |
| 1,448,987,269 | 0260h | 97F60h | 007Ch | 007Ch | 6 |
| 1,448,987,388 | 0266h | 97F66h | 0000h | 0000h | 12 |
| 1,448,988,051 | 0250h | 97F50h | 007Eh | 007Eh | 6 |
| 1,448,988,298 | 0256h | 97F56h | 0000h | 0000h | 132 |
| 1,448,988,539 | 0242h | 97F42h | 006Eh | 006Eh | 6 |
| 1,448,988,819 | 0248h | 97F48h | 1D1Dh | 007Eh | 193 |
| 1,448,988,984 | 024Eh | 97F4Eh | 0000h | 0000h | 71 |
| 1,448,989,174 | 023Ah | 97F3Ah | 007Eh | 007Eh | 6 |
| 1,448,989,416 | 0240h | 97F40h | 1D1Dh | 0000h | 127 |

At 1,448,988,819 the 1D1Dh sent the loop to ES:[1D1Dh], 448Ah, for the
span's end. It patched its RETF into 614F:448A, far past the span, so the
span ran on to the scaler's own RETF, and then put the byte back. The game
survived that one. The other loads of the list, through BP+2 and BP+4, all
read what memory holds. Both wrong reads came after a long wait, 193 and
127 clocks since the JMP back to 036D retired, and read a dword's lanes 0
and 1. The two reads after waits of 132 and 71 clocks read lanes 2 and 3,
and were right.

A last rerun (`678f9aa62c`) printed the data port each clock around both
wrong reads, and they have one cause. At cycle 1,448,988,607 M1 sent a
2-byte load of A0288h for 614F:034F, the byte after the RETF the loop had
patched in at 034D. That byte is `37`, AAA, which the CPU has not built:
the front end decoded on past the RETF, D marked the uop faulted, and M1
sent its load anyway. M2 waits only for a fault-free load's bytes
(`dmem_read` in `fes_i386_cpu.sv` does not check the fault that `m2_load`
does), so on the next edge the RETF retired and flushed with the read still
out. The port did not know that nothing waited for it. It held the next
loads behind it, the span-list read at 1,448,988,635 among them, until the
VGA answered at 1,448,988,816, and merged the read's two bytes into that
load's lanes 0 and 1. At 1,448,989,270 the same happened at 614F:03A3,
after the RETF at 03A1: `3F`, AAS, the same load of A0288h. The span-list
read at 1,448,989,298 waited until 1,448,989,413 and took 1D1Dh where memory
held 0000h. Both times the VGA returned 1D1Dh for A0288h, pixels already
drawn there. The read after 71 clocks, at 97F4Eh, waited behind a third
such read from 03A3 and was right only because it used lanes 2 and 3.

`037248ffae` fixes the port. The wrapper exports whether the uop of the
load M1 sends is fault-free (`dmem_awaited`), and the port abandons the VGA
reads of a load M2 will not wait for as it sends them. They still go to the
VGA and load its latches, but their bytes go to no load (`vga_faulted`).
The CPU's part is that it sends the load of a uop it has already marked
faulted: a VGA read, with its effect on the latches, for an instruction that
never runs. The bench's count of VGA loads that never retired already held
these: on the old port, 15 at 614F:03A3 and 2 at 614F:034F, on wrong paths.

On `037248ffae` the game runs to 14.4899 s with no stop, with
`vga_faulted` 25 and `shadow_faults` 0. 18 of the faulted loads are at
614F:03A3 and 2 at 614F:034F. The frame is the old port's but for the dead
guard's sprite column, which now matches the oracle's. The OPL2 writes and
the audio are the old port's, sample for sample. With a faulted load's read
abandoned, the loads issued while it is out count as overlaps. The
`OVERLAP` lines, one for each abandoned read a load was issued during, rise from 36 to 56, the 20
more all for reads of A0288h, and `vga_overlaps`, which counts every load
issued while an abandoned read is out, from 290 to 704.

## The oracle hangs on an x87 escape

In both Wolfenstein 3D runs that went astray, an oracle step for an x87
escape never returned:

- `D9 00` at C000:7FFF in the first run, after `D8 6C 6C` at C000:3526
  returned;
- `D9 00` at 0101:0110 on the fixed port, after `D9 00` at 0101:010C
  returned.

Sampling the bench's instruction pointer found it in Bochs's
`handleWaitForEvent` both times, the first time on its check for IGNNE#:
the step had put Bochs in its wait-for-x87 state.
`BochsOracle::step()` (`cores/fes-common/sim/i386/bochs_oracle.cpp`) runs
Bochs's `cpu_loop`; nothing in that wait sets `kill_bochs_request`, and no
interrupt can arrive during a step, so the loop never ends.

The first escape returning and the second hanging fits how Bochs handles an
x87 error with CR0.NE clear, though the runs do not show it directly. The
FPU's reset control word unmasks every exception, so a garbage operand
leaves an error pending, and the next escape waits for IGNNE# or an
interrupt.

Any RTL run that hands the oracle two x87 escapes can hang this way; the
stop path cannot be trusted with them until the adapter bounds such a wait.
Not fixed here: it is the oracle adapter's.

## The design's known gaps, in these games

| Gap | What the games did |
| --- | --- |
| No uncached path | VGA loads on a wrong path reached the VGA (below). Wolfenstein 3D's latch copies read the VGA two elements ahead of their stores, garbling the status bar (above) |
| No caches | Not exercised: the bench's memory has no latency |
| No self-modifying-code detection | The bench counts each store into code the front end has fetched ahead of the next instruction (`smc_stores`), the bytes they land on (`smc_addresses`), and each instruction that retires over such a byte before a refetch (`smc_executed`). Ultima VI: 425,444 stores on six bytes, the boot's two and 1BCBCh-1BCBFh, from the shell's code at 1006:BC40 and BC45 at 3.93 s, as FreeCOM runs the typed command. No instruction retired over a stored byte. Nothing from Ultima VI's own code. Wolfenstein 3D to 14.49 s: 425,533 stores, all on one byte, 99EFBh, from FreeDOS's 8FE9:A04A from 0.30 s, as in the boot, where it is data inside a code segment. None executed. The game writes a RETF into a scaler before each sprite span and the old byte back after it (2000:037B and 2000:039D), but never into code already fetched |
| No protected mode | Never entered, in either game; neither census has a protected-mode or V86 instruction |
| No debug traps | TF was never set |
| No double fault | No fault during a delivery |

## VGA loads that never retired

The boot record counted the RTL's MMIO reads that the oracle's VGA trace
lacks: 22, all wrong-path loads, all reading A0000h-AFFFFh as FFh in text
mode. A game's timing diverges between the CPUs, so its traces cannot be
compared read for read. The bench now counts the same thing directly: each
VGA load the RTL sends that never retires. The oracle never speculates, so
these are exactly the reads its trace would lack. On the boot the count is
22 of 28,837 VGA loads, the boot record's figure, with 121,487,313 retired,
the same as there, on the old port and on the fixed one (where 11 loads were
cancelled and none overlapped).

| Game | VGA loads | Never retired | Where |
| --- | ---: | ---: | --- |
| Ultima VI | 139,556 | 26, all on a wrong path | The boot's 22, and 4 at 3.95 s as the game starts: 3 from FreeDOS's kernel at 0070:0544 (`FF /3`, CALL FAR m16:16) reading A0A68h, and 1 from the game's code at 21D3:2005 reading A1951h |
| Wolfenstein 3D, to 14.49 s | 200,031 | 179: 129 on a wrong path, 50 to a restart | The boot's BIOS sites; then the game's mode-X code. The 32 commonest sites, 143 loads: 51 on wrong paths in scaler code in segment 614F, which ends each call with a RETF, 20 of them the faulted loads above; 38 at `MOV r8,m8` loads in 2D73:0059-01D1, half on wrong paths and half lost to interrupt restarts; 12 at the VGA-to-VGA REP MOVSB at 2C0B:08F5, 8 of them to restarts, the first at 12.127 s; 12 at 2CBB:08B5, 6 to restarts; 10 at 5F1F:034B |

The boot's 22 come from the BIOS, at 0.06 s and 0.30 s. The front end
fetches on past an unconditional JMP (F000:E3FE) and two IRETs (F000:FEE6,
F000:F84B) before WB redirects, and decodes the bytes after them, which start
with `00` (ADD r/m8,r8): a load, whose operand lands in A0000h-AFFFFh. The
stem the bench prints is read from guest memory at the end of the run. For
21D3:2005 that is `D4`, which loads nothing, so Ultima VI has replaced the
code there since.

In text mode such a read changes nothing. In mode 13h, mode X and the planar
modes a read loads the VGA's four latches, which a later write-mode-1 store
copies out, as Wolfenstein 3D's mode-X screen copies do. The bench does
not tell whether any of Wolfenstein 3D's landed between such a load and its
store. The design's uncached loads issue only when they are the oldest
instruction, so none of these would reach the VGA, provided a uop already
marked faulted sends none. That path comes before either game runs on a kit.

## Where the RTL's time went

In the boot the RTL retired 17% more instructions than the oracle in about
the same simulated time, at 3.4 clocks an instruction against the oracle
model's 4.1. In Ultima VI's intro it retired 5% fewer, at 5.5 clocks an
instruction against 5.2, and read the VGA status port 28% fewer times. The
intro spends 40% of the oracle's instructions in a three-instruction poll of
the retrace bit: IN AL,DX (`EC`), TEST AL,imm (`A8`), JZ back (`74`), 41.2
million times each. On the RTL every turn of that loop waits for its I/O
cycle and then takes the JZ, which the CPU predicts not taken, resolves in
EX and redirects from WB. In Wolfenstein 3D to 14.49 s it retired 7% more
than the oracle, at 4.4 clocks an instruction against 4.7, and read the
status port 28% fewer times. With no caches, these clocks are no
measure of the CPU on a kit.

## The instruction mix

The commonest stems:

| Ultima VI | Count | Share | Wolfenstein 3D | Count | Share |
| --- | ---: | ---: | --- | ---: | ---: |
| `74` | 45,594,492 | 14.8% | `74` | 59,377,485 | 11.1% |
| `EC` | 41,305,125 | 13.4% | `8B` | 51,703,262 | 9.7% |
| `A8` | 41,243,878 | 13.4% | `83.7` | 28,916,657 | 5.4% |
| `8B` | 15,273,521 | 5.0% | `EC` | 26,170,742 | 4.9% |
| `89` | 11,002,857 | 3.6% | `A8` | 25,424,502 | 4.8% |
| `03` | 6,896,316 | 2.2% | `8A` | 20,428,202 | 3.8% |
| `75` | 5,497,452 | 1.8% | `A3` | 20,131,140 | 3.8% |
| `8E` | 4,919,716 | 1.6% | `6689` | 19,537,768 | 3.7% |
| `D1.5` | 4,633,497 | 1.5% | `66A1` | 19,339,041 | 3.6% |
| `8A` | 4,147,347 | 1.4% | `2B` | 19,059,506 | 3.6% |
| `72` | 4,094,811 | 1.3% | `89` | 18,962,420 | 3.6% |
| `AD` | 3,826,969 | 1.2% | `88` | 17,489,272 | 3.3% |

Wolfenstein 3D's 32-bit code is a tenth of its instructions: 32-bit MOV to
and from memory (`6689`, `66A1`, `66A3`), compares (`663B`) and the
fixed-point arithmetic its renderer uses: SHRD (`660FAC`, 408,024), MOVSX
(`660FBF`, 549,407), MOVZX (`660FB7`), 32-bit shifts (`66C1`), IMUL
(`660FAF`), and CDQ with IDIV (`6699`, `66F7.7`, 1,539 each). Its 47 stems
the boot did not use also include NOP (`90`, 1.4 million), SHL r/m8,1
(`D0.4`, 1.2 million), JNS and JS, and the 32-bit immediate forms of ADD,
SUB, CMP and XOR. Ultima VI's 14 are mostly SHR r/m8,1 (`D0.5`, 1.9
million). The jsonl lists every stem of both censuses with its count and its
first CS:EIP.

## The VGA FIFO and the oracle

The boot record says the oracle path's stores never come close to filling
the VGA's front FIFO. That holds for the boot and for Ultima VI, but not for
Wolfenstein 3D. On `d8b0e63907`, before the base branch's FIFO rule, 64 of
the oracle's 622,060 VGA port writes took longer than their class time: 28
byte writes of 6 to 95 clocks and 36 word writes of 11 to 117. An oracle run
of the same game on 2026-10-27 has the same instruction count and VGA command
counts, with every port write at its class time. Its bench (`013cddcbd9`, on
`deano/pc386-m3-audio` only) already took the clocks the VGA refused a port
write for a full FIFO out of the access's time, as the base's rule now does.
Main's bench never had that, so main's oracle game suite would fail
Wolfenstein 3D's `vga-out` check.

The base's rule covers it. On the merged tip `1187fefd6d` the oracle game
suite passes Wolfenstein 3D. Its instruction count (532,715,552 steps) and
final frame are those of the `d8b0e63907` census, byte for byte. Every VGA
port write is at its class time net: 512,162 byte writes at 4 clocks and
109,898 word writes at 7. The 64 slow writes appear as `IOWAIT` lines
instead: 28 byte writes waited 2 to 91 clocks for FIFO space, and 36 word
writes 4 to 110.

## How the bench counts loads that never retire

`sim/pc386_rtl_cpu.sv` also exports the EIP of the load M1 sends, and, for
the edge just past, whether M1 sent a load, whether D to WB moved, whether WB
committed its uop and whether WB flushed every younger one. The bench keeps
a shadow of M2, EX and WB beside the CPU. A VGA load enters it as M1 sends
it; it moves when the stages move; it leaves when WB commits it. A flush
loses everything younger than WB: on a wrong path when WB's uop committed and
redirected, or to a restart when it did not (a fault or an interrupt). The
RTL's stop and restart around an oracle step loses whatever is in flight. The
bench reports the counts by kind (`vga_lost_path`, `vga_lost_restart`,
`vga_lost_stop`) and an `UNRETIRED` line for each of the 32 commonest sites:
the kind, the count, CS:EIP, the stem, the first address and its cycle. The
bench checks the shadow against the CPU: a load the bench saw that the CPU
says M1 did not send (or the reverse), a VGA load entering while the stages
hold, or a VGA load leaving WB without a commit counts in `shadow_faults`.
There were none in any run.

The data port queues each VGA dword a load reads and holds `dmem_valid` low
until the last comes back. Since `d6fb3af33d`, when the shadow loses a VGA
load on the edge M1 sent it, the port marks that load's reads abandoned.
They still go to the VGA, but their bytes go nowhere, and `dmem_valid` rises
at once. `vga_cancelled` counts such loads, and `vga_overlaps` counts the
loads issued while an abandoned read was still out. Since `69de303890` an
`OVERLAP` line, printed as it happens, gives the first load issued while
each abandoned read is out: its cycle, its physical address and the
abandoned read's. Since `037248ffae` the port also abandons, as M1 sends it,
the load of a uop that already carries a fault, which M2 never waits for:
the wrapper's `dmem_awaited` says which, and `vga_faulted` counts them.

`--trace-port FROM:TO` (`678f9aa62c`, repeatable) prints a `PORT` line for
each clock of the window: the load M1 sends, its address, bytes and EIP;
the store; the edge's load, move, flush and commit; the retirement;
`dmem_valid` and `dmem_rdata`; and the VGA queue, its length, the reads the
waiting load still needs and the kind of the op at its front.

The runner's game suite takes `--cpu rtl` and writes `game-<name>-rtl/`. The
sound suite still runs the oracle only.

## Run time

On Powerboat, one thread each, at loads from 5 to 60 on 24 threads:

| Run | CPU minutes | Wall minutes |
| --- | ---: | ---: |
| Ultima VI census, oracle, 16 s | 27 | 27 |
| Wolfenstein 3D census, oracle, 24 s | 47 | 47 |
| Ultima VI, RTL, 16 s | 91 | 95 |
| Wolfenstein 3D, RTL, to 14.49 s | 80 | 80 |
| Wolfenstein 3D, oracle, to 14.49 s | 23 | 23 |
| Wolfenstein 3D, RTL, to 14.49 s, with registers and dumps | — | 88 |
| Wolfenstein 3D, RTL, to 14.49 s, with two port-trace windows | 89 | 97 |
| Wolfenstein 3D, RTL, to 14.49 s, port fixed again, with a history and dumps | 116 | 134 |
| FreeDOS boot, RTL, 4.1 s | 30 | 32 |
| Wolfenstein 3D, oracle, merged tip, 24 s | 77 | 127 |

## What comes next for Phase 3

- The uncached path, so that loads on a wrong path no longer reach the VGA.
  The sites above are its test cases.
- Ultima VII needs a fixture with HIMEMX, and then protected-mode entry (LGDT,
  MOV CR0, segment loads from descriptors) for its unreal mode, none of which
  these games touch.
- Runs past Ultima VI's intro (keys through character creation into play)
  and longer in Wolfenstein 3D, whose instruction mix may differ in play.
- The sound suite on the RTL.

## Reproduce

With the eXoDOS collection at `$FES_PC386_GAMES`:

```sh
python3 scripts/sim_fes_pc386.py --suite game --game ultima6 --cpu rtl   # writes build/sim/fes-pc386/game-ultima6-rtl/
python3 scripts/sim_fes_pc386.py --suite game --game wolf3d --cpu rtl    # writes build/sim/fes-pc386/game-wolf3d-rtl/
python3 scripts/sim_fes_pc386.py --suite game --game wolf3d              # the oracle run
```

A census is `machine_tb --census FILE` on the oracle game run's arguments,
which its `report.json` records as `command`. The retire histories are
`machine_tb` on the RTL game run's arguments with `--limit-seconds` just past
the stop and `--stop-history N`; the jsonl records each run's command, with
`<games-work>` for the game disk the runner builds.
