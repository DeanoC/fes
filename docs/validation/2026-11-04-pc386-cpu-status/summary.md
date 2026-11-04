# pc386 CPU status ring, 4 November 2026

Issue [#1433](https://github.com/DeanoC/fes/issues/1433) needed the running
CPU's state on a kit, where only HDMI, audio and guest-memory dumps existed.
`pc386_system`'s status words were open in the shell.

**Result:** `pc386_status` writes the six status words and the system clock
count into a 256-record ring in the core's DDR window every 2^16 clocks
(1.07 ms), and `scripts/pc386_cpu_status.py` reads it from the kit's HPS with
busybox `devmem` over ssh while the core runs. No runtime, agent, FogCast or
mailbox change is needed, so it works core-only on any kit software. The seal
at `b4558d4cd` (Quartus 17.0.2, package `01e1eebb…`) meets every analysis at
61.44 MHz. On Kit B the reader located #1433's stall: a #GP loop at
INTRO.EXE's offset 0172 ([#1433](https://github.com/DeanoC/fes/issues/1433#issuecomment-6638535792)).

## What changed

- **The ring.** Head `{MAGIC 50333836h, records written}` at window offset
  `0x01000000`; record n at `0x01001000 + 64 × (n mod 256)`: word 0
  `{n, MAGIC}`, words 1–6 the status words 0–5 latched in one clock, word 7
  the clock count at the latch. The count and records reset with port 2; the
  clock count never does. The region was unused in the pc386 window map
  ([design](../../../sources/misteross/docs/pc386-machine-design.md#hps-ddr-window-and-ports)).
- **Port 2.** The ring's beats are single writes on the aux port in front of
  `pc386_disk`. A beat takes the port only on a clock the disk drives neither
  read nor write and holds it until accepted; the disk sees waitrequest
  meanwhile. Nine beats a period; the disk keeps priority.
- **Why not the mailbox.** The runtime owns the GP mailbox while a core runs,
  and a new opcode would have changed shared `fes_computer_mailbox` RTL (C64,
  Spectrum, ZX81) and the runtime. No `fes_computer_*` file changes here.
- **The reader.** `sample` runs one busybox loop on the kit that reads the
  head and the newest record's words; a sample is whole when word 0 still
  names the record, which is not rewritten for 255 periods (273 ms). About
  5 ms per `devmem` read gives 11–28 samples a second. `snapshot` prints the
  newest record as JSON. Linux refuses `read()` of the window (`dd`: Bad
  address), so `devmem`'s mmap is the path. The loop stops when its reader
  goes away, and the reader stops its ssh command's whole process group.

## Evidence

| Check | Kind | Result |
| --- | --- | --- |
| `sim_fes_pc386.py --suite system` ([report](system-report.json)) | Host simulation | PASS: FreeDOS prompt, input exercise, Stop, round trip, hold sweep; 2877 ring records equal the status words at their stamp's clock, 1 late (the launch's first record, held behind the CRC pass past the bench's 2^18-clock history) |
| `tests/test_pc386_cpu_status.py` | Host unit test | PASS |
| Quartus 17.0.2 seal, seed 1, clean checkout `b4558d4cd` ([summary](seal-summary.json)) | Sealed package | Timing pass: setup +0.712 ns, hold +0.084 ns, recovery +7.363 ns, removal +0.488 ns, pulse width +1.122 ns; 29,485 ALMs; package `01e1eebb1ab03acd51958ed15b3a4be4400d6a4a3aa952f1c0e19cf83e07b9e2` |
| Kit B, core-only on baseline a8cd30c4, runs cs1–cs4 | Hardware diagnostic | Live readout through boot, FreeDOS and MS-DOS games: 48,513 whole samples, 9 torn ones detected and dropped; #1433 stall located |

The bench reads the core's clock count one behind its own cycles: its first
`clock()` is Verilator's settle, not a rising edge.

Later commits on the branch change only `system_tb.cpp` and the reader, not
the sealed RTL.
