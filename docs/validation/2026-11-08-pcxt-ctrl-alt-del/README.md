# PC/XT Ctrl+Alt+Del and relaunch (fes#1534)

Run on 8 November 2026 on Kit A.

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
so only warm reboots failed. An 8259 drives INT from the unmasked requests, so
a real XT does not hit this.

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

- `KF8259_Control_Logic.sv`: while no acknowledge or poll is in progress, INT
  follows the unmasked requests, as on an 8259. An acknowledge already in
  progress completes unchanged. The change is recorded in the vendor
  `source.json` and README.
- `pcxt_memory.sv`: a cold reset zeroes the BIOS warm-boot word before
  firmware, disk loads or the CPU can use the controller. Execution is held
  until firmware loads, so the BIOS always runs its full cold POST after a
  launch.

## Tests

- `tests/test_fes_kf8259_int_withdraw.py` replays the warm-POST PIC sequence
  against KF8259 alone in a few seconds. It fails on the old latch ("masking
  every IRQ withdraws INT") and passes with the fix.
- `scripts/sim_fes_pcxt_dos.py --scenario reboot` boots FreeDOS and presses
  Ctrl+Alt+Del through the host keyboard mailbox. It requires GLaBIOS's warm
  POST to finish and the floppy to boot to `A:\>` again. It halted in
  `HALT_BEEP` before the fix and passes after it (4.87e9 fabric clocks).
- `--scenario relaunch` leaves `1234h` in SDRAM before the cold reset. It
  requires the word to read zero after firmware load, and a `Boot [ COLD ]`
  POST to `A:\>`.
- `motherboard_tb.cpp` sampled INTR on the clock its memory wait ended. With
  PIT mode 2, OUT0 goes low for one PIT clock each period, and INT now follows
  it. The bench waits for OUT0 to rise again, as its own acknowledge step below
  already does.
- `sim_fes_pcxt.py --suite all` and `sim_fes_pc386.py --suite devices` pass. pc386
  shares KF8259.

## Package

- Source commit `f6a9e179f59a395713f82339e3026aad722ec0f4`.
- Quartus 17.0.2 Lite, seeds 1–3, build `7eb569aea37f935b2a9d72d67fac1191`.
- Package `7a5221ee33d9b7197666baec148e49234500d08f8c06c07381b4a3f3c754317f`.
- Archive `1c85e9c6aae885b7377c9501cc632e3ba2d512c078962f01d4318edc2ee58da0`.
- RBF `ac0e317996c72cee46b9e941eb74047f62a2e3bdae107b309e54360c33bf6ec4`. The
  independent seed-1 repeat produced the same RBF.
- Internal timing passed on all three seeds with no failing endpoint. These are
  the worst slow-corner Fmax figures across the seeds:

  | Clock | Worst Fmax | Required |
  | --- | --- | --- |
  | Memory | 107.30 MHz | 100 MHz |
  | Video | 105.34 MHz | 74.25 MHz |
  | Audio | 204.58 MHz | 12.288 MHz |

## Kit A run k1

The run was core-only on baseline software `4efd34bf`, image `31b39063`, with
no install, no image change and no reboot. It used the #1529 `cat.img`
(FreeDOS with Alley Cat) as library game `x1534-cat`, loaded and inserted
through the agent as in the issue.

1. **First launch:** `Boot [ COLD ]` to `A:\>`.
2. **Ctrl+Alt+Del at `A:\>`:** GLaBIOS showed `Boot [ WARM ]` within 2 s
   ([`kit-a-warm-post.png`](kit-a-warm-post.png)), booted FreeDOS, and `DIR`
   listed the disk.
3. **Ctrl+Alt+Del during Alley Cat play**
   ([`kit-a-alley-cat.png`](kit-a-alley-cat.png)): a warm POST to `A:\>`,
   and `ECHO ABC` echoed ([`kit-a-after-game-reboot.png`](kit-a-after-game-reboot.png)).
   An earlier Ctrl+Alt+Del at the game's graphics-mode joystick question also
   rebooted to `A:\>`.
4. **Stop during a warm POST, then relaunch:** Ctrl+Alt+Del, then Stop sent
   about 1 s later. The warm POST was still at its RAM count 2 s after the keys
   in step 2, so the flag was most likely still `1234h` at Stop; the kit cannot
   read it back. The relaunch gave `Boot [ COLD ]`, `A:\>`
   ([`kit-a-relaunch-cold.png`](kit-a-relaunch-cold.png)) and a working `DIR`.
   A second Stop and relaunch also booted COLD to `A:\>`.

The run removed the FogCast data it created. Kit A was restored to its baseline
and released, idle with its lease free. The runner, driver and all frames and
audio are on Powerboat in `~/fes-scratch/xt1534`.

Not covered: the HALT_BEEP audio no longer plays, but no tone analysis was run.
The library launch still inserts the floppy after release, so a fast POST can
reach INT 19h first; only the cold POST's length hides that. Fixing the
launch order is outside this change.
