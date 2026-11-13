# pc386 removable media F1: `pc386_fdc` and its Bochs replay, 12 November 2026

This record covers slice F1 of the
[removable-media design](../../design/pc386-removable-media.md)
([#1739](https://github.com/DeanoC/fes/issues/1739), tracker
[#1694](https://github.com/DeanoC/fes/issues/1694)).

**Result:**
- **What is built.**
  - `pc386_fdc`, a fresh MIT 82077AA subset: DOR, MSR/DSR, FIFO and DIR/CCR at 3F2h, 3F4h, 3F5h and 3F7h, IRQ 6, and DMA channel 2 on controller 1's data path.
  - Two drives, with disk-change latches, spin-up, SRT stepping and settle.
  - The sector path to the window at `0x01800000` (A:) and `0x01C00000` (B:): 64-beat sector reads and whole-sector stores as a new storage client of port 2. Port 2's aux master and the FDC take turns.
  - Non-DMA (ND) transfers.
- **Out, as the slice says.** CMOS still reports no floppy drives, and the storage endpoint is not connected: `pc386_system` ties both drives off until F3 commits disks. No BIOS patch, `machine_tb` units or suites (F2).
- **Replay** (`sim_fes_pc386.py --suite fdc` at `d92925c38`, [log](fdc-replay.log)). 34 cases, each a guest's script run on `pc386_fdc` and on Bochs's `iodev/floppy.cc`:
  - 20 match line for line.
  - The 11 that [`fdc_divergences.toml`](../../../sources/misteross/cores/fes-pc386/sim/fdc_divergences.toml) lists differ as listed.
  - The 3 RTL-only cases (non-DMA, the store fence and a disk swapped mid-write, timing) pass their checks, and so does every case's own RTL check.
- **Existing suites.** `devices`, `boot` (oracle and `--cpu rtl`), `storage`, `system` and `benches` pass ([below](#suites)).
- **Seal.** Quartus 17.0.2, seed 1, CUDA slot on chip-30x0-ai-dev, from a clean checkout of `d92925c38` (this PR's RTL head), meets every analysis: setup +0.885 ns, hold +0.055 ns.
  - It uses 37,073 ALMs and 271 RAM blocks, against main `2f6829c2c` sealed on the same host the same way: 36,731 ALMs, 269 RAM blocks, setup +0.514 ns, hold +0.074 ns.
  - That is +342 ALMs, +2 M10K and +2 DSP, at 88.5% of the device.
  - Earlier seals of this branch, all passing: `149e6436f` (36,918 ALMs, 277 RAM blocks, setup +0.503 ns), `191a71d8c` (37,144 ALMs, 271 RAM blocks, setup +0.780 ns), `9cb6c331b` (37,242 ALMs, 271 RAM blocks, setup +0.564 ns), `cb9f41722` (37,161 ALMs, 271 RAM blocks, setup +0.421 ns), `eceba46e1` (37,119 ALMs, 271 RAM blocks, setup +0.693 ns), `6f4412abd` (37,145 ALMs, 271 RAM blocks, setup +0.646 ns), `35264ac27` (37,083 ALMs, 271 RAM blocks, setup +0.894 ns) `048fbb104` (37,215 ALMs, 271 RAM blocks, setup +0.566 ns), `e2ea0cd70` (37,114 ALMs, 271 RAM blocks, setup +0.892 ns) and `75e48e925` (37,171 ALMs, 271 RAM blocks, setup +0.662 ns), before later review fixes to the fence, `busy`, the store queue and a disk leaving mid-command.

## Area

| Build | ALMs | RAM blocks | DSP | Registers | Setup | Hold |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| main `2f6829c2c` ([summary](main-build-summary.json)) | 36,731 | 269 | 38 | 32,636 | +0.514 | +0.074 |
| F1 seal `d92925c38` ([summary](seal-build-summary.json)) | 37,073 | 271 | 40 | 33,097 | +0.885 | +0.055 |
| Area experiment on `191a71d8c` ([summary](area-experiment-build-summary.json)) | 37,431 | 271 | 40 | 33,265 | +0.494 | +0.083 |
| Doc's F1 estimate | +1,000 | +1 | 0 | | | |

**The F1 seal is a floor.** With no drive fitted, every drive input is a constant in the shell, and Quartus removes logic that only a present disk reaches.

**The area experiment** is not a package and is not in the branch. It is `191a71d8c`, before the fence fixes, with the drive inputs taken from unused bits of the machine record's settings, so nothing can be pruned. It measures +700 ALMs, +2 M10K and +2 DSP: the controller's full cost, the figure F3 will pay once its endpoint drives those inputs. Fit noise is about ±200 ALMs ([area audit](../2026-11-08-i386-area-audit/summary.md)).

**Against the estimate:**
- **ALMs** are under the doc's +1,000.
- **M10K** is one over. The sector buffer is 64 words of 64 bits, so that a port 2 beat lands in it every clock. That width takes two M10K, because an M10K is at most 40 bits wide.
- **What was tried first.** The first seal (`149e6436f`, setup +0.503 ns, hold +0.085 ns, 36,918 ALMs) wrote DMA bytes into the buffer with byte enables, which Quartus split into eight M10K, one per lane. The buffer now takes whole words: each byte rewrites its word together with the bytes before it, from a gather register.
- **DSP.** The extra DSP blocks are the sector address (cylinder × heads × sectors per track) and the step time (SRT × the rate's unit).

## Replay

The two models keep different time. Bochs completes a sector in one timer period, with no spin-up, settle or byte pacing. So the replay is closed-loop, not a timed trace:
- Each case is a script a guest would run: program the controller, send a command, wait for that model's own RQM, IRQ or DRQ, then read the results.
- It runs once on each model.
- What the guest reads is logged: port reads, result bytes, interrupts, the DMA streams with their CRCs, timeouts, and each disk image's final CRC.

| Group | Cases |
| --- | --- |
| Match Bochs | registers, invalid, seek, seek_below_zero, recalibrate_no_drive, read_track, read_eot_no_tc, read_tc_mid_sector, write_read, write_eot, read_id, errors, format, disk_change, media_360_in_12, media_720_in_144, media_dmf, media_288, no_media, dsr_reset |
| Listed divergences | soft_reset_lock, status_registers, fm_read, read_deleted, write_deleted, write_tc_mid_sector, write_protected, unsupported, format_bad_id, insert_into_empty, dma_gate |
| RTL only | pio_read_write (non-DMA, which Bochs does not finish), fence_and_swap (the store fence, and a disk swapped mid-write, during spin-up, with the last command byte, in each of Read ID's last clocks, while a read's last byte or a format's ID byte waits for its DACK, and while a write's last sector is being stored; a DOR or DSR reset just after a write's last byte or a format's last ID byte, which still stores it; a format sent while that cut store runs, which keeps the cut store's fill byte; and a TC within a write's first word), timing |

The divergences follow the design or the 82077AA datasheet, and each entry names its deciding case. In summary:
- After a software reset, LOCK stays clear (Bochs sets it).
- 3F0h, 3F1h and 3F3h read FFh, as on an AT.
- Commands with MFM clear end with ST1 MA (Bochs: invalid).
- Read Deleted without SK reports ST2 CM.
- Write Deleted writes as data (Bochs: invalid).
- A TC mid-sector pads the rest of the sector with 00h (Bochs writes its stale buffer).
- Writing a write-protected disk gives ST1 NW (Bochs: 27h/31h).
- Verify, Read Track and Scan are invalid.
- Format IDs must match the disk.
- A disk arriving in an empty drive sets the change latch.
- DOR bit 3 gates INT and DRQ.

The list also records five deviations or cases the replay does not compare:
- the data rate and no double-stepping, which both models share;
- time;
- non-DMA;
- what Bochs panics or hangs on.

**Timing** (RTL only, at the bench's 4 clocks per µs):
- a seek of 10 cylinders at SRT 0Dh and 250 kbit/s takes 75 ms, which is 10 steps of 6 ms plus the 15 ms settle;
- the first byte waits out the 500 ms spin-up;
- a 1.44M sector's 512 bytes come at 16 µs a byte.

## Suites

Every suite ran at `d92925c38`, the RTL head.

| Suite | Result |
| --- | --- |
| `fdc` (new) | PASS |
| `devices` | PASS |
| `boot` (oracle) | PASS |
| `boot --cpu rtl` | PASS: FreeDOS to the prompt in 3.5 simulated s, 138,961,848 retired, 0 oracle steps, no stops |
| `storage` | PASS |
| `system` | PASS: holds cut a port 2 write burst in 4 of 186 trials, with port 2 writes after each release; status ring 2,783 records |
| `benches` | PASS |
| `tests.test_pc386_machine` (`FdcReplayTest`, port 2 harness), `tests.test_build_fes_pc386_quartus` | PASS |

## Reproduce

```bash
python3 scripts/sim_fes_pc386.py --suite fdc --download
python3 scripts/build_fes_pc386_quartus.py --root "$PWD" --seed 1 --gpu-router CUDA
```

Run both from `sources/misteross`; the seal needs the pc386 lock's CUDA toolchain slot.
