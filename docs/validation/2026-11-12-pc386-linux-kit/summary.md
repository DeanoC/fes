# pc386: early Linux on a kit, 12 November 2026

This record boots the three early Linux kernels from the early-Linux plan
record ([`2026-11-11-pc386-linux-plan`](../2026-11-11-pc386-linux-plan/summary.md),
PR [#1709](https://github.com/DeanoC/fes/pull/1709)) on a real kit with
main's pc386 core. The plan record had no kit run: before Phase 5 slice 4
(task switches, [#1684](https://github.com/DeanoC/fes/pull/1684)) merged, a
kit would have stopped at the first `switch_to`. It also reruns all three
kernels on main's RTL in simulation, including 0.99.15, which had no
slice-4 run. It changes no RTL.
Refs [#1708](https://github.com/DeanoC/fes/issues/1708).

**Result.**
- **All three kernels boot to `login:` on Kit B and run a root shell.**
  Linux 0.99.15, 1.2.13 and 2.0.0 start from FreeDOS through LOADLIN,
  mount the minix root on `/dev/hda2`, log in as root and run `uname -a`,
  `cat /proc/meminfo`, `ls /`, `dmesg | head` and a shell `for` loop.
- **The kit's output matches the oracle's.** `uname -a` and every
  `/proc/meminfo` value equal the 42 s oracle frames of #1709's runs for
  all three kernels, as do the boot messages (devices found, memory
  available, `Checking 'hlt' instruction... Ok.`).
- **Boot is fast at max.** From the agent's load acknowledgement, which
  covers the BIOS, FreeDOS and LOADLIN, `login:` is on screen within
  8.4–9.4 s for 2.0.0 and 5.2–8.3 s for 1.2.13 and 0.99.15.
- **No core bug was found.** The only difference from the sim is the
  BogoMIPS value, which follows the clock (see below). No issue was filed.
- **Main's RTL runs all three kernels exactly as the oracle does.** On
  main's RTL in simulation, each reaches `login:` and runs the oracle runs'
  commands with 0 oracle steps and no stop. Each 42 s frame is
  byte-identical to the oracle's.

## Package

- **Package.** `522ac3eb17849e6c4b1bddeba87c733e3deb2eeec70429174c04e92318893cca`
  (`.fcore` SHA-256 `e79875c2…`, `build_id` `9d784de212ea1f149acc3749228a8749`),
  a Quartus 17.0.2 seal (seed 1) of main `763085fc3`, built in a clean
  detached worktree with `scripts/build_fes_pc386_quartus.py --gpu-router CUDA`.
  [`seal-summary.json`](seal-summary.json) is its `build-summary.json`.
- **Host and backend.** chip-30x0-ai-dev, CUDA (`gpu_backend: cuda`). Main's
  pc386 toolchain lock names slot `a4d627f7…`, which chip did not have
  (the lock moved with #1680), so the slot was bootstrapped there first. The
  sealer takes only the slot's Yosys (`0a7927bf`); Quartus places and routes.
  The seal took 11 minutes.
- **Timing.** Every analysis passed. Worst slacks: setup +0.597 ns,
  hold +0.100 ns, recovery +6.322 ns, removal +0.436 ns and minimum pulse
  width +1.122 ns. Clocks: system 61.44 MHz, pixel 74.27 MHz, audio
  12.29 MHz.
- **Resources.** 36,229 of 41,910 ALMs, 269 of 553 RAM blocks, 38 DSP
  blocks.
- **Why this is main's core.** Under `cores/fes-pc386` and
  `cores/fes-common`, main has changed only the pc386 README since #1684
  merged as `b080fa896`; the sealer gained `--gpu-router` (#1699). Main has
  since moved to `78b7ee4b7` (#1709's record), which changes no source.

## Kit and disks

- **Kit.** Kit B, core-only on its baseline: image `179affbb…`, software
  bundle `7ddb0bfb…`. Nothing was installed, and there was no reboot. The
  claim, the restore check, the cleanup and the release are logged in Kit B's
  status file. The run staged the three disks and left their game records
  and disk deltas in FogCast's data directories. A housekeeping claim then
  removed exactly those nine trees, and the kit's free space was back to
  353.5 MB, as before the run.
- **Disks.** #1709's disks, by SHA-256 in `~/fes-drives/pc386/` on
  Powerboat, not committed: a FreeDOS partition with LOADLIN 1.6 and the
  kernel, whose `AUTOEXEC.BAT` runs
  `LOADLIN ZIMAGE root=/dev/hda2 rw no387`, and the Slackware root floppy as
  partition 2. Each disk is 20/16/63. The kernels and roots are Slackware's
  period boot and root disks (GPL).

  | Kernel | Disk SHA-256 | Root |
  | --- | --- | --- |
  | 0.99.15 | `2b1a340f95df97c9502831449f21b252c6f22bdb015edab8f358d2c64495a48a` | Slackware 1.1.2 `color144` |
  | 1.2.13 | `36973148fff814a3764aab368fc93153b7ff5c5200a3c7cdf4ccd92d55ec822b` | Slackware 3.0 `color` |
  | 2.0.0 | `f6f284d3b9c1e9df18233db0624277e3268a9a334ce74a836ed7e0a004fed978` | Slackware 3.1 `rescue` |

- **Run `k1`.** One kit lease, at max (preset 0). The run first boots the
  shared memory-scrub disk (SCRUBX). Then, for each kernel, it launches the
  package as a fresh library game on that kernel's disk and grabs HDMI about
  every 2 s for 120 s. It types `root`, then each command, through the
  agent's input stream, and grabs a frame 5 s after each. Scratch runner and
  all frames: `~/fes-scratch/lnxkit` on Powerboat.

## What the kit shows

| Kernel | `login:` after the load ack | BogoMIPS | `uname -a` | Commands |
| --- | --- | --- | --- | --- |
| 0.99.15 | 5.2–8.3 s | 61.00 | `Linux darkstar 0.99.15 #3 Fri Feb 4 17:08:32 CST 1994 i386` | All ran |
| 1.2.13 | 5.2–8.3 s | 61.56 | `Linux slackware 1.2.13 #1 Tue Aug 22 22:23:13 CDT 1995 i386` | All ran |
| 2.0.0 | 8.4–9.4 s | 61.24 | `Linux slackware 2.0.0 #1 Mon Jun 10 21:11:56 CDT 1996 i386` | All ran |

- **Login times are brackets.** Each is the gap between the last grab
  without `login:` and the first with it. Grabs were 1–3 s apart, which is
  close enough to show the boot takes seconds, not minutes. t=0 is when the
  agent acknowledges the load, after it has staged the disk and loaded the
  core. By the first grab, about 1 s later, the BIOS has run and FreeDOS is
  starting.
- **`/proc/meminfo`** equals the oracle's in every field. 2.0.0 shows
  `MemTotal: 14748 kB` and `MemFree: 13372 kB`. 1.2.13 shows total
  15286272, free 14102528. 0.99.15 shows total 15663104, free 14200832.
- **`dmesg | head`.** None of the three Slackware root floppies has `dmesg`
  or `head`, so the shell prints `not found` for both. The `for` loop
  (`for i in 1 2 3; do echo loop $i; done`) prints `loop 1` to `loop 3`.
- **`ls /`** lists each root floppy's directories (`bin`, `dev`, `etc`,
  `proc`, ...), in colour.
- **Boot messages** match the oracle's: for 2.0.0, `Console: colour VGA+
  80x25`, `pci_init: no BIOS32 detected`, `Memory: 14748k/16384k available`,
  `Checking 'hlt' instruction... Ok.`, the PS/2 aux port, `hda: FES pc386
  ATA disk, 9MB w/0kB Cache, LBA, CHS=20/16/63`, `floppy0: no floppy
  controllers found`, `Partition check: hda: hda1 hda2` and `VFS: Mounted
  root (minix filesystem).` 0.99.15 prints `lp_init: no lp devices found`;
  1.2.13 also reports `ftape: allocated 3 buffers`.

Frames (cropped to the text, 960 px wide, grey):
[`kit-b-2.0.0-boot.png`](kit-b-2.0.0-boot.png) (2.0.0's boot messages,
4.2 s after the load ack), [`kit-b-2.0.0-shell.png`](kit-b-2.0.0-shell.png),
[`kit-b-1.2.13-shell.png`](kit-b-1.2.13-shell.png) and
[`kit-b-0.99.15-shell.png`](kit-b-0.99.15-shell.png). The 1.2.13 shell frame
is after the last command and visibly includes `dmesg | head` and the `for`
loop, including the `not found` messages and loop output.

## Kit against the sim

- **BogoMIPS follows the CPU clock.** The kit's 61.0–61.6 BogoMIPS match the
  seal's 61.44 MHz system clock. `machine_tb` runs the CPU at 100 MHz of
  simulated time (`CLOCK_HZ = 100e6`), so #1709's slice-4 sim showed 99.74.
  The ratio, 1.63, is 100/61.44. The Bochs oracle shows 24.93, its own
  instruction rate. This is a bench time base, not a core difference.
- **Boot time in the sim is not kit time.** For the same reason a simulated
  second holds 1.63 times as many CPU clocks as a kit second. The sim also
  starts the clock at reset, before the BIOS, and its keys are typed at fixed
  times. So the sim's `login:` times are not compared with the kit's.

## Main's RTL in simulation

Each kernel ran on main's RTL for 42 simulated seconds with #1709's oracle
keys: `root` at 24 s, then `uname -a`, `cat /proc/meminfo`, `ps` and
`ls /bin` 3, 6, 9 and 12 s later. That way each frame compares with the
oracle run's frame at the same second (`machine_tb --cpu rtl --rtl-from 3`;
the oracle boots FreeDOS and the RTL takes over at 3.0 s, before LOADLIN).
Host: chip-30x0-ai-dev. `machine_tb` was built from main `763085fc3` with
`sim_fes_pc386.py --suite boot` and Verilator 5.032; chip's system
Verilator, 5.020, stops on two width warnings. The BIOS and VGA BIOS images
are the seal's (`162e92ca…`, `8522568e…`).

| Kernel | Result on main's RTL | Retired | Oracle steps | Frames equal to the oracle's |
| --- | --- | --- | --- | --- |
| 0.99.15 | `darkstar login:` by 8 s; logs in, runs all four commands; runs to the 42 s limit | 830,419,333 | 0 | 39 of 42, the 42 s frame included |
| 1.2.13 | `slackware login:` by 10 s; logs in, runs all four commands; runs to the 42 s limit | 340,595,005 | 0 | 37 of 42, the 42 s frame included |
| 2.0.0 | `slackware login:` by 10 s; logs in, runs all four commands; runs to the 42 s limit | 108,540,480 | 0 | 34 of 42, the 42 s frame included |

- **No stop and no oracle step** in any run: the logs have no `STOP` or
  `GAP` line, and `oracle_steps=0`.
- **2.0.0 retires exactly as #1709's slice-4 keyed run did** (108,540,480
  instructions, at `46995175a`), so #1684's merge as `b080fa896` changed
  nothing Linux sees.
- **0.99.15 was #1709's missing slice-4 run.** On main before #1684 it
  stopped at its first `switch_to` at 6.0 s and the oracle ran 845.9M steps
  to the end. Now the RTL runs it all: its idle task's `INT 80h` loop at
  CPL 3 retires 830.4M instructions in 39 s with no oracle step.
- **The frames that differ** come during the boot (5–9 s for 2.0.0,
  7–9 s for 1.2.13, 6–8 s for 0.99.15), where the RTL and the Bochs oracle run at different
  instruction rates (99.74 against 24.93 BogoMIPS), so messages land on
  different seconds. The rest are single frames where the blinking cursor
  differs (13, 34 and 38 s for 2.0.0; 13 and 38 s for 1.2.13; none for
  0.99.15). Apart from those, every frame from 10 s on is byte-identical to
  the oracle's.
- `machine_tb` exits 1 at the time limit, as in #1709's runs: the
  runs give no `--expect` text, so the end reason is `limit`.

## Follow-ups

- None from the kit. #1709's follow-ups (the paged `STOP` byte read,
  [#1707](https://github.com/DeanoC/fes/issues/1707), and `machine_tb`'s
  screen reader) stand.
