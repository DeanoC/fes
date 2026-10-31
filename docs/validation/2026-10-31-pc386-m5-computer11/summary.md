# pc386 M5: the fes.computer 1.1 contracts in the shell, 31 October 2026

Slice 3 of [#1267](https://github.com/DeanoC/fes/issues/1267)
([#1319](https://github.com/DeanoC/fes/issues/1319), PR
[#1320](https://github.com/DeanoC/fes/pull/1320)). The pc386 shell now speaks
`fes.computer` 1.1 to the runtime: identity word 16, the ATA disk the runtime
writes into the DDR window with the core's CRC pass, the save fence and dirty
map, the `fes.pc386.machine` record and the wall clock. Slice 1's development
host link, its `pc386-host` tool and the bench's host model are gone.

**On Kit B, the runtime's library launch boots FreeDOS from a staged disk, and a
save round trip works.** The sealed package went through the FogCast agent and
libmister-runtime's own pc386 path, `load_library_core` with a game and a disk.
- The disk was staged by SHA-256 and the game's machine settings were set.
- The first launch reached `C:\>`. Its Stop saved 4 dirty disk chunks and the
  record.
- The relaunch, from the base image plus the saved delta, reports
  `MKDIR failed for 'C:\M5'.`: the directory the first session created came back.

The saved record carries the restored Sound Blaster setting in CMOS 60h-62h
and, from run m5c2 on, the clock's live registers.

- **Simulation.** The system bench drives `pc386_system` through the GP
  mailbox exactly as the runtime does:
  - identity, the record restore and the wall clock;
  - Prepare, then the image into the window;
  - a wrong-CRC Commit, which the core rejects, then the right one;
  - release.

  FreeDOS boots to the prompt with no stop, stall or underflow. At Stop the
  dirty map holds exactly the chunks the guest changed. The record's settings
  and CMOS RAM make a round trip through the RTC, and its 00h-0Dh hold the
  running clock.
- **Golden exchanges.** Every scenario of the M0 1.1 fixture
  (`exchanges-1.1.json`) replays against the RTL mailbox and storage
  endpoint.
- **Seal.** Quartus 17.0.2 sealed the head's RTL: build `d355dd17…`, package
  `6823af43…`, setup +1.420 ns.

This record has host-only checks (simulation and fixture replay), a sealed
package, and exact-artifact hardware evidence for the disk and record paths
on Kit B, through a kit-software overlay (below). It is not acceptance of the
four games, the keyboard or the governor, which are later M5 work.

## Inputs

- FES branch `deano/pc386-m5-computer11`, on `deano/pc386-m5-cpu`
  ([#1268](https://github.com/DeanoC/fes/pull/1268)). It was built on
  `2c13827a2f`, which holds [#1230](https://github.com/DeanoC/fes/pull/1230)'s
  head `45af99a23` and main `bb0680c66` with the M0 PRs (#1220, #1222 with
  #1225, and #1219). It has since merged #1268's `2bc8975cb` and `21f568d92`
  (#1230's squash and main `d6a5748f9`); those merges change no misteross
  source.
- RTL revision `e6de1a468`: sealed, simulated, and on the kit as run m5c2. It
  adds the record's live clock bytes (Codex's review) to `fde4ae4b5`, which run
  m5c1 tested. Later commits change only documents or merge #1268.
- Verilator 5.032 on Powerboat. The seal used Quartus Prime Lite 17.0.2, with
  the netlist from the pc386 lock's Yosys `0a7927bf`.

## What the shell does

- **Identity.** `fes_computer_mailbox_core` has `ENABLE_ATA_DISK`,
  `ENABLE_PERSISTENCE`, `ENABLE_PC386_MACHINE` and `ENABLE_WALL_CLOCK` set. It
  reports minor 1, with capabilities `0x3009` in word 7 (video, audio, HPS DDR,
  session display) and `0x000f` in word 16. The 1.1 opcodes go to
  `fes_computer_storage`, which holds each until it replies. C64, Spectrum and
  ZX81 tie the new ports off and stay 1.0.
- **Disk.**
  - Prepare and Eject wait until the drive's writer is idle and the dirty map
    is clear.
  - Commit makes the unit verifying. `pc386_disk` then reads `sectors` x 64
    words at window offset 0x08000000 in one-sector bursts and runs CRC-32
    over them. During the pass `ddr_hold` is low and the machine stays in reset.
  - Freeze raises `pc386_ata`'s fence at block boundaries. The ACK waits until
    every accepted sector has landed with its dirty mark.
  - The map survives machine resets. Prepare, Eject and Saved clear it.
- **Record.**
  - Words 2 and 4-6 are the straps at each release: the code profile, and the
    Sound Blaster's port, IRQ and DMA.
  - After each reset the RTC loads CMOS 0Eh-7Fh from the record, then writes
    its configuration bytes and checksum. Every RAM byte written goes back to
    the record.
  - 00h-0Dh are the clock's registers, not RAM. Freeze reads them from the RTC
    as the guest does (two bytes at a time, without clearing C). A restore
    leaves them to the clock: a missing record's zeros would stop the
    oscillator (A) and select 12-hour time (B), and the wall clock sets the
    time at every launch.
  - The clock has MC146818 RESET semantics: a machine reset clears only B's
    interrupt enables, SQWE and the flags.
- **Wall clock.** Word 3 applies it. The RTC takes it once its CMOS setup is
  done after release, and the CPU starts after that.
- **Hold.** The CPU stops at once; its warm reset lets port 0 finish. The
  machine resets once the CPU's memory and the drive's writer are idle, after
  `fes_hps_ddr` has taken the hold.

## Simulation

| Check | Result |
| --- | --- |
| `sim_fes_pc386.py --suite storage`: the five `exchanges-1.1.json` scenarios, each compiled with its interfaces | 25, 246, 118, 19 and 19 exchanges match (minor-0 endpoint, pc386 persistence and wall clock, pc386 ATA disk, persistence without layout, layout without persistence) |
| `--suite devices` (`pc386_at_core`, M10K collisions poisoned) | 87 checks pass, 10 of them new (below) |
| `--suite audio`, `opl2`, `benches` | pass |
| `--suite boot` (oracle CPU, `pc386_machine`) | FreeDOS prompt and `dir` at 4.3 s; VGA replay 85,234 events, no mismatch |
| `--suite system` (`pc386_system`, RTL CPU, runtime protocol) | pass ([report](system-report.json)) |
| `make sim-fes-apple2-mailbox`, Spectrum `mailbox_units` (1.0 replays) | pass |
| `make sim-fes-zx81`, `sim-fes-c64`, `sim-fes-spectrum`, `sim-fes-apple2` (the shells that gained tie-offs) | pass |
| `parent_tests.py` over the branch's paths | pass |
| misteross unit tests (`unittest discover`), FES `test_affected`, `test_test_changed`, `test_check_deadcode`, `consistency.py` | pass (1,570 misteross tests; 41 fixture copies match) |

The 1.1 replay compares every GPI word but one. In the minor-0 scenario, the
fixture's synthetic endpoint advertises the Apple II floppy bit with no unit
behind it, so its MediaInfo state reads 0. The RTL always implements unit 0
for that bit, so the bench compares only the error code of that one exchange.

New device checks:
- the RTC's writes reach the record;
- CMOS comes back from the record after a reset;
- a reset clears B's interrupt enables and keeps the clock;
- a reset leaves the dirty map, and `dirty_clear` clears it in 128 clocks;
- a frozen write command waits under BSY;
- Resume opens it, and its store lands once with its mark;
- a Freeze mid-block lets the block finish and fences the next one;
- the record reads the clock's registers as the guest does, without clearing C.

One existing check changed. The 8042's FF reply now allows one tick of its
1 us enable under 1 ms, which is how the device defines a byte time. The RTC's
shorter setup moved that enable's phase by 26 clocks.

System run, `e6de1a468` (run 4; `fde4ae4b5`'s run 3 matched it but for the
clock bytes):
- Launch over the mailbox: the CRC pass over the 32 MiB image took 4,474,459
  clocks, about one 64-bit word a clock, or 73 ms at 61.44 MHz. 128 MiB would
  take about 0.29 s.
- The first Commit carried a wrong CRC. The unit returned to loading with
  flag 8, the CRC error.
- `C:\>` appeared 2.70 simulated seconds after release. 45,544,393
  instructions had retired, and every underflow counter read zero.
- The bench's copy of the fixture runs `MD C:\M5` in AUTOEXEC.BAT. At Stop,
  DiskDirty returned chunks 0, 2, 4 and 6. Those are exactly the chunks that
  differ from the staged image (68 bytes), and the epoch read 6. Saved
  cleared the map.
- The record held:
  - the restored settings (Sound Blaster at 240h, IRQ 5);
  - the restored byte at CMOS 50h;
  - 60h-62h = 24h 05h 01h;
  - the configuration bytes;
  - a correct 2Eh-2Fh checksum;
  - century 20h;
  - 00h-0Dh as the guest reads them: 08h-09h = 10h 26h (October 2026),
    B = 02h, D = 80h.

  Restored again and released through the RTC's setup, its settings and CMOS
  RAM read back unchanged.

## Quartus seal

`make build-fes-pc386-quartus` at seed 1 and `e6de1a468`:
[`seal-summary.json`](seal-summary.json).

| | Seal, `e6de1a468` | `fde4ae4b5` (run m5c1) | First fit, `948a5ecb6` | Slice 1 (`0efa2213`) |
| --- | --- | --- | --- | --- |
| Build ID, package | `d355dd17…`, `6823af43…` | `083020ae…`, `ed6a8395…` | `5a4b86e8…`, `b1c954fe…` | `0efa2213…`, `538773ad…` |
| System Fmax, slow corners | 70.86-71.66 MHz | 70.24-71.06 MHz | 70.91-71.10 MHz | 72.53-72.70 MHz |
| Pixel Fmax | 83.03-84.13 MHz | 81.15-81.99 MHz | 79.57-80.67 MHz | 86.99 MHz |
| Setup, hold slack | +1.420, +0.113 ns | +1.141, +0.085 ns | +0.896, +0.095 ns | +1.969, +0.093 ns |
| Recovery, removal, pulse width | +7.851, +0.460, +1.122 ns | +4.951, +0.411, +1.122 ns | +6.038, +0.554, +1.122 ns | +6.914, +0.266, +1.122 ns |
| ALMs | 24,246 of 41,910 (58%) | 24,297 | 24,257 | 23,147 |
| Registers | 22,252 | 22,216 | 22,127 | 22,315 |
| RAM blocks | 254 of 553 | 254 | 254 | 251 |
| DSP blocks | 29 of 112 | 29 | 29 | 27 |
| RBF | 3,492,112 bytes, SHA-256 `c6ebe9d2…` | 3,558,228 bytes | | 3,465,012 bytes |

`948a5ecb6` is a data point only: the CRC pass's fix came after it. Every fit
passes every analysis, with no illegal or unconstrained clocks. Against slice 1,
the 1.1 endpoint, the CRC pass and the record add about 1,100 ALMs, 3 RAM
blocks and 2 DSP blocks. The RAM blocks are the record's buffer and CMOS
words; the DSP blocks are DiskCommit's cylinders x heads x sectors check.

Quartus reports warning 10999 for the record's CMOS words, which have byte-lane
writes. It builds them as two byte-wide RAMs, as slice 1 recorded for the
L1D's byte lanes. No workaround is in the source.

## On Kit B

Two runs, each claimed in Kit B's status file and released FREE with its
baseline restored:
- run m5c1, 18:11-18:15 EET, with `fde4ae4b5`'s package `ed6a8395…`;
- run m5c2, 19:40-19:44 EET, with the seal's exact package `6823af43…`.

- **Why a bundle.** Both kits run kit software (`a168c922` on B, `b5f6d8e5`
  on A) that predates the M0 runtime and agent (#1222, #1219). The kits'
  agent has no disk-image route, and their runtime admits no 1.1 package.
- **What was installed.** `make kit-software` over the running image
  `f904961e` produced a bundle from each run's head: `941f8ec3…` (m5c1, from
  `fde4ae4b5`) and `1a781f22…` (m5c2, from the merged head `590cb2dd2`). Each
  was installed and activated, then baseline `a168c922` was activated again.
  This is a bundle install-then-restore: no image or boot change, no reboot,
  and no change to the shared FogCast host.
- **Disk.** The FreeDOS fixture with `MD C:\M5` in AUTOEXEC.BAT, SHA-256
  `64cf7a4d…`.

Run m5c1, through the agent, under a kit lease, as FogCast's host does for a
pc386 library game (`fogcast/core_packages.go` `launchCoreEntry`):

| Step | Result |
| --- | --- |
| `/v1/library/disk-images/probe`, then upload | not staged, then staged: 33,546,240 bytes in 23.3 s |
| `/v1/library/core/data/inspect` | record `absent`, defaults |
| `/v1/library/core/settings` (240h, IRQ 5) | revision `60be76e0…` |
| `/v1/library/core/load` with the game and the disk (`65/16/63`) | active in 9.12 s, including the window write and the core's CRC pass; FreeDOS reached `C:\>` ([grab](kit-m5c1-first-prompt.png)) |
| `/v1/stop` | idle; record revision `8c4549b4…`; disk delta `FESDELT1` with 4 chunks of 64 KiB |
| `/v1/library/core/load` again | active in 9.06 s; the boot, from the base plus the delta, prints `MKDIR failed for 'C:\M5'.` ([grab](kit-m5c1-second-mkdir.png)) |
| `/v1/stop` | idle; the record is unchanged |

The saved record, read from the kit, is `fes.pc386.machine` 1.0: 72 words
with a valid hash and settings 0, 0, 0, 0, 576, 5, 1, 0. Its CMOS bytes are:
- 60h-62h = 24h 05h 01h;
- 12h = F0h;
- 15h-16h = 0280h;
- a correct checksum.

Century 32h is 19h: the kit's clock reads 1970, so the runtime sent
1980-01-01, as the contract clamps it. The run's summary is in
[`kit-m5c1-result.json`](kit-m5c1-result.json).

Run m5c2 used the same game, so the disk was already staged and its record
was m5c1's. Both launches booted from m5c1's saved delta and reached `C:\>`
after `MKDIR failed for 'C:\M5'.` ([grab](kit-m5c2-prompt.png)); they were
active in 9.68 s and 8.90 s. Stop saved record revision `0a08f41c…`. Its
00h-0Dh are now the clock's live registers:
`25 00 00 00 00 00 03 01 01 80 26 02 50 80`. That is 00:00:25 on Tuesday,
1980-01-01, 25 seconds into the session, with A 26h, B 02h, C 50h (pending
periodic and update flags) and D 80h. Settings and CMOS 60h-62h are as in
m5c1. Summary: [`kit-m5c2-result.json`](kit-m5c2-result.json).

**Restore.** After each run, baseline `a168c922` was selected and active
again, with nothing pending. Image good `f904961e`, previous `4db74864`, the boot ID (so no
reboot), idle RBF `64f1f16d`, the launcher and the USB quirk all matched the
probe. The agent was idle and the lease free.

After m5c1 the runner first logged HALTED, because it compared the software
status's previous-bundle field, which any install-then-restore changes
(`74fbe741` to `941f8ec3`). Every other key matched. The kit was checked by
hand and logged FREE. The runner no longer compares that field, and m5c2
released Kit B itself.

The staged disk image and the game's core data stay in FogCast's data
directories on the kit. They are listed in the kit status entry.

## Not done

- **Governor.** The record's preset, frequency and turbo words are stored,
  validated and round-tripped. `fes_i386_cpu` has no governor to consume
  them yet.
- **Kit baselines.** The kits run software from before the M0 runtime, so a
  1.1 package needs a newer bundle. That is promotion of a main build after
  #1219 and #1222 (Lander's baseline), not this PR.
- **Keyboard and mouse** (slice 2) and **the four games** (slice 4).
- The session-display menu opcodes, which the runtime sends at launch, ran
  on the kit but not in the system bench.
