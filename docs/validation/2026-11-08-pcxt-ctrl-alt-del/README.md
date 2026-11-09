# PC/XT Ctrl+Alt+Del and relaunch (fes#1534)

First run on 8 November 2026 on Kit A. Rechecked on 9 November 2026 on Kit B
at PR head `b03ad270ba8fe10687ca8a6138a3bc598ab6b752`, after the KF8259
edge-pulse change and a main merge.

## Cause

There were two defects.

**Ctrl+Alt+Del halted the warm POST.** GLaBIOS's Ctrl+Alt+Del handler sets
the warm-boot flag `0040:0072 = 1234h` and jumps to its POST with interrupts
off. The PIT keeps requesting IRQ0 until the POST re-initialises the 8259. The
POST then masks every IRQ (`OUT 21h,FFh`), executes STI, and halts if any
interrupt arrives (`E1BC`–`E1C3`, beep code 5 short / 3 long: `BL = 53h`).
The imported KF8259 latched INT until an acknowledge even after the BIOS masked
the request or ICW1 cleared IRR. So the CPU took a stale interrupt, and the
BIOS halted in `HALT_BEEP` with a black screen, a dead keyboard and the
523/698 Hz pattern from the issue. A cold boot never has a request pending,
so only warm reboots failed.

`reboot_tb.cpp` reproduced it in simulation. After Ctrl+Alt+Del, the trace
shows the IMR walk on port 21h, `OUT 21h,FFh`, and then the BIOS interrupt
handler reading an empty ISR (`OUT 20h,0Bh; IN 20h = 00h`) before
`HALT_BEEP`.

**A relaunch then warm-booted.** SDRAM keeps its contents across an FPGA
reconfiguration, so the halted guest's `1234h` survived Stop. A library
launch loads the core with the drive empty and then inserts the floppy, which
took about 5.8 s in the issue's run. A cold POST's RAM test outlasts that. The
warm POST skips it and reached INT 19h before the floppy was ready, which gave
`Disk Boot Fail`. A launch with an empty drive recovered because its POST ran
to the end and cleared the flag.

## Fix

- `KF8259_Control_Logic.sv`: INT is withdrawn when ICW1 clears IRR, or when
  an IMR write leaves no eligible request. The check runs one clock after the
  write, once the new mask has reached the priority resolver, and only while
  no acknowledge or poll is in progress. A request pin that falls by itself
  does not withdraw INT. The acknowledge then returns the default IR7 vector,
  as the 8259A datasheet describes for a request that ends before INTA. An
  acknowledge already in progress completes unchanged. The change is recorded
  in the vendor `source.json` and README.
- `pcxt_memory.sv`: a cold reset zeroes the BIOS warm-boot word before
  firmware, disk loads or the CPU can use the controller. Execution is held
  until firmware loads, so the BIOS always runs its full cold POST after a
  launch.

The first version of the KF8259 change (head `6478a8381`, package `7a5221ee`)
dropped INT whenever no unmasked request remained. That also withdrew INT
when an edge-triggered request ended before INTA. Commit `5d6c38078` limits
the withdrawal to ICW1 and IMR writes. Both #1534 paths are still covered:
the warm POST's ICW1 and its `OUT 21h,FFh` each withdraw the stale IRQ0.

## Tests

Run at `b03ad270b` on 9 November 2026.

- `tests/test_fes_kf8259_int_withdraw.py` replays the warm-POST PIC sequence
  against KF8259 alone in a few seconds. It checks that a short edge pulse
  keeps INT up and is acknowledged as IR7, that masking every IRQ withdraws
  INT, that unmasking a held request raises it again, and that ICW1
  withdraws it. Passes.
- `tests/test_fes_kf8259_priority.py` (with `YOSYS` set to the locked
  executable) passes.
- `scripts/sim_fes_pcxt_dos.py --scenario reboot` boots FreeDOS and presses
  Ctrl+Alt+Del through the host keyboard mailbox. It requires GLaBIOS's warm
  POST to finish and the floppy to boot to `A:\>` again. It halted in
  `HALT_BEEP` before the fix. Passes (4,866,019,614 fabric clocks).
- `--scenario relaunch` leaves `1234h` in SDRAM before the cold reset. It
  requires the word to read zero after firmware load, and a `Boot [ COLD ]`
  POST to `A:\>`. Passes (2,706,405,926 fabric clocks).
- pc386 shares KF8259. `scripts/sim_fes_pc386.py --suite devices` and
  `--suite boot` on the oracle CPU pass. `--suite boot --cpu rtl`
  fails on this head and on main alike. At this
  head, which predates #1596, it stalls in the #1589 ATA reset race. With current
  main (`fafd20953`, #1596) merged in locally, it reaches the prompt at 3.5 s and
  fails only the stale-code check (#1595: 11 instructions, first at
  `f000:1df1`). Main alone gives the same result, with identical instruction,
  store and port counts, so the KF8259 change does not alter the RTL boot.
- `motherboard_tb.cpp` sampled INTR on the clock its memory wait ended. With
  PIT mode 2, OUT0 goes low for one PIT clock each period. The bench waits for
  OUT0 to rise again, as its own acknowledge step below already does.

## Package

- Source commit `b03ad270ba8fe10687ca8a6138a3bc598ab6b752`.
- Command, from `sources/misteross` in a clean tree:
  `QUARTUS_ROOTDIR=$HOME/intelFPGA_lite/17.0/quartus python3 scripts/build_fes_pcxt_quartus.py --dos-profile --cache-root $HOME/fes/out/cache/misteross-toolchains --package-output <dir>`
  (18 minutes).
- Quartus 17.0.2 Lite, seeds 1–3, build `07ea6d228db01656b02c17d1d3491a2a`.
- Package `8f557f1414151535ff0fd9586ffdceca456f3fba1402592a21d3c2925b1babc1`.
- Archive `718301bb36beeaa138d2b7e8e18fa287c9446f14e4c206ccf95b7ad916dcf4f3`.
- RBF `ca362084e5664169ca32a0de0df4416908e28494a3f4537fe2a0bd4a33a207d8`. The
  independent seed-1 repeat produced the same RBF.
- Internal timing passed on all three seeds with no failing endpoint. These are
  the worst slow-corner Fmax figures across the seeds:

  | Clock | Worst Fmax | Required |
  | --- | --- | --- |
  | Memory | 100.43 MHz (seed 1, −40 °C) | 100 MHz |
  | Video | 108.32 MHz | 74.25 MHz |
  | Audio | 212.77 MHz | 12.288 MHz |

  The memory clock has about 0.04 ns of slack at its worst corner. The
  package carries main's #1565 CGA font change.

The 8 November package `7a5221ee` (source `f6a9e179f`) predates
`5d6c38078` and is superseded.

## Kit B run b1

On 9 November 2026, 13:33–13:39 EET. The kit was on its post-upgrade baseline:
image `be0be0ce`, software `4aa8421f`. The run was core-only, with no install,
no image change and no reboot. It used the #1529 `cat.img` (FreeDOS with
Alley Cat, SHA-256 `188045787914ab25…`), loaded and inserted through the
agent as library game `x1569-cat`. Every load reported package `8f557f14…`
active.

1. **First launch:** `Boot [ COLD ]` to `A:\>`.
2. **Ctrl+Alt+Del at `A:\>`:** GLaBIOS showed `Boot [ WARM ]` within 2 s
   ([`kit-b-warm-post.png`](kit-b-warm-post.png)), booted FreeDOS, and `DIR`
   listed the disk.
3. **Ctrl+Alt+Del at Alley Cat's joystick question:** a warm POST to `A:\>`,
   and `DIR` listed the disk.
4. **Ctrl+Alt+Del during Alley Cat play**
   ([`kit-b-alley-cat.png`](kit-b-alley-cat.png)): a warm POST to `A:\>`,
   and `ECHO ABC` echoed
   ([`kit-b-after-game-reboot.png`](kit-b-after-game-reboot.png)).
5. **Stop during a warm POST, then relaunch:** Ctrl+Alt+Del, then Stop sent
   within 1 s; an HDMI grab between them still showed the DOS screen. The
   warm POST was still at its RAM count 2 s after the keys in step 2, so the
   flag was most likely still `1234h` at Stop. The kit cannot read it back. The relaunch
   gave `Boot [ COLD ]`, `A:\>`
   ([`kit-b-relaunch-cold.png`](kit-b-relaunch-cold.png)) and a working `DIR`.
   A second Stop and relaunch also booted COLD to `A:\>`.

The run removed the one FogCast data tree it created. Kit B was left on its
baseline, idle with its lease free, and logged FREE.

### Repeating the run

The runner and driver are on Powerboat in `~/fes-scratch/x1569`:
`run-x1569.sh` claims the kit in `~/kit-status`, checks it is on the given
baseline, runs `hil_xt.py`, removes the FogCast data the run created, and logs
FREE only when the kit matches its pre-run snapshot. The commands it fed are
in `runs/b1/commands.jsonl` (a copy is `commands-template.jsonl`). The run was:

```bash
cd ~/fes-scratch/x1569 && mkdir -p runs/b1 && cp commands-template.jsonl runs/b1/commands.jsonl
HEAD_SHA=b03ad270ba8fe10687ca8a6138a3bc598ab6b752 \
PKG=$HOME/fes-scratch/x1569/pkg/8f557f1414151535ff0fd9586ffdceca456f3fba1402592a21d3c2925b1babc1.fcore \
GOOD=be0be0ceab0ec773d665515dc421936d890064386a1bf712878e5a8ed35b4526 BASE_SW8=4aa8421f \
./run-x1569.sh B b1 $HOME/fes-scratch/x1569/disk/cat.img x1569-cat
```

The evidence is in `~/fes-scratch/x1569/runs/b1`: `result.json`, the
`fr/*.png` frames, the `cad-*.wav` audio, and the wrapper and driver logs.

Not covered: the HALT_BEEP audio no longer plays, but no tone analysis was run.
The library launch still inserts the floppy after release, so a fast POST can
reach INT 19h first; only the cold POST's length hides that. Fixing the
launch order is outside this change.
