# pc386 ATA software reset race, 9 November 2026

Issue [#1589](https://github.com/DeanoC/fes/issues/1589). Machine bench on the
RTL CPU and the Bochs oracle CPU, `devices_tb`, test386, a Quartus seal and
Kit B runs `f1` and `f2`.

**Result.** This is a stall, not a slowdown, and the bug is in the ATA model,
not in the CPU. The faster RTL CPU now beats a race in `pc386_ata.sv`, and
the BIOS then waits out a 32-second IDE timeout for a drive that isn't
there.

- **Cause.** On a software reset the drive loaded its task-file signature,
  including `drive_head <= 0` (DEV clear, device 0 selected), when BSY
  cleared, `RESET_CLOCKS` (1000 bench clocks, 10 µs) after the host cleared
  SRST. The Bochs BIOS's device 1 probe (`ata_reset`, then `ata_detect`)
  clears SRST and soon after selects device 1 (`OUT 1F6h, B0h`) to read its
  status. When that select lands inside the window, the reset completion
  overwrites it and puts device 0 back. The BIOS then reads device 0's
  status (50h, DRDY and DSC) as device 1's. With CL and CH 00h and a non-zero
  status, it types the absent device 1 as ATA, sends it IDENTIFY
  (`OUT 1F6h, F0h; OUT 1F7h, ECh`), and polls `await_ide(NOT_BSY_DRQ, …,
  IDE_TIMEOUT)`. Device 1's status reads 00h, so the loop runs to its
  32,000 × 2,048-poll limit, about 160 simulated seconds at about 240 clocks a
  poll. The suite's 8 s limit ends inside it, and the screen shows only
  `ata0 master`.
- **Why it appeared on the RTL now.** The window is fixed in clocks. The
  Phase 4 front-end slices (BTB, RAS, store-to-load, early ALU, load
  forwarding and indirect prediction) made the RTL run the BIOS between the
  SRST-clear write and the device 1 select in under 1000 clocks. No single
  slice is the cause: each made the CPU faster and exposed the model's race.
  The oracle CPU still takes longer than the window and was never affected.
- **The counters.** `port0_writes=51,437,136` is the poll loop's stack
  traffic: `inb`'s frame, `time++` through `lincl` (`inc dword ss:[bx]`) and
  the `lsrul`/`lcmpul` helpers' pushes, about 16 writes a poll. The
  `IO isa8` count (3,224,866 against 69,352 on 5 November) is the same loop
  reading 1F7h.
- **Fix.** The task file's reset signature (count 01h, number 01h, cylinder
  0000h, drive/head 00h, error 01h) loads at the write that clears SRST. BSY
  still stays set for `RESET_CLOCKS`, and reset completion now clears only
  BSY, ERR and the phase. A Device/Head write in the window stands. This
  matches Bochs's `harddrv`, which resets the drive select as SRST clears.
  Device 0's own reset is unchanged in effect: the BIOS now sees 01h/01h at
  step (g) and waits for `NOT_BSY_RDY` instead of `NOT_BSY`, with the same
  outcome.

## How it was found

The suite's counters first. On 5 November the RTL passed with 69,352 8-bit
ISA I/O cycles, and on main it fails with 3,224,866, nearly all of them reads
of 1F7h. So the BIOS was polling, not running slowly. An I/O trace
(`PC386_TRACE_IO=1`, 0.15 simulated s) showed the poll inside the device 1
probe. The machine bench was then built with temporary prints of the BIOS's
`time` local at SS:BP-4 for each 1F7h read. They showed that the first
`await_ide(BSY, …, 20)` in `ata_reset` times out correctly, after 43,009
polls on both CPUs. The stall is a second `await_ide`, entered from another
frame.

Before that, the `await_ide` loop (`inb`, `lincl`, `lsrul`, `lcmpul`, as bcc
compiles it) ran in lockstep against the oracle twice, once with a stub `inb`
and once with a real `IN` reading 00h. It timed out after 4,096 polls with
no divergence, which ruled out a CPU fault in that arithmetic. These were
temporary harness changes, not committed.

Paired I/O traces of the RTL and the oracle (0.16 and 0.5 simulated s,
values masked to the access width, repeated keyboard polls folded) agree up
to the device 1 status read after the reset:

| Step | RTL clock | Oracle clock |
| --- | --- | --- |
| `OUT 3F6h, 0Ah` (SRST clear) | 13,084,758 | 21,542,401 |
| `OUT 1F6h, B0h` (select device 1) | 13,085,701 (+943) | 21,543,679 (+1,278) |
| reset completion | +1,000: after the select | +1,000: before the select |
| `IN 1F7h` (device 1 status, `st`) | **50h** | 00h |
| next | `OUT 1F6h, F0h; OUT 1F7h, ECh`, then 00h for ever | `OUT 376h, 0Ah`: channel 1 probe |

## Bench

| Check | Result |
| --- | --- |
| `devices_tb` new check: SRST set, SRST clear, `OUT 1F6h, B0h` 500 clocks later, 2,000 clocks on | fixed RTL: 01h 01h, device 1 00h, device 0 DRDY; old RTL: FAIL `1 1 50 50` |
| `--suite devices` | PASS, 0 failures (138 checks) |
| `--suite storage` | PASS |
| `--suite boot` (oracle CPU) | PASS, prompt at 3.75 s |
| `--suite scroll` (oracle CPU) | PASS, `SCR DONE ok` and the prompt at 3.85 s |
| `--suite scroll --cpu rtl` | `SCR DONE ok` and the prompt at **3.70 s** (main: stalled at 8 s); suite FAIL on the separate stale-code check, below |
| `test_fes_i386_test386.py` | PASS: 44,926 EE lines, 0 differing; cross-check 0 disagree |

The RTL scroll run's counters, against the 5 November pass and main's stall:

| Run | End | Retired | `IO isa8` | `port0_writes` |
| --- | --- | --- | --- | --- |
| 5 Nov (`2026-11-05-pc386-u7-view-roll`) | prompt, 3.85 s | 103,972,064 | 69,352 | 38,035,656 |
| main `d6589de42` (#1589) | 8.00 s, in POST | 304,265,424 | 3,224,866 | 51,437,136 |
| this PR | prompt, 3.70 s | 143,731,486 | 70,633 | 45,876,776 |

The lockstep suite was not run. This change touches no CPU file, and the
diff is `pc386_ata.sv` and `devices_tb.cpp`.

### The stale-code failure is a separate fault (#1595)

With the stall gone, the RTL scroll suite fails its check that the RTL never
runs code over bytes stored after it fetched them: 11 instructions at
`F000:1DF1`, 2,830,890 clocks in. This failure is on main, it doesn't
involve this PR, and it is
[#1595](https://github.com/DeanoC/fes/issues/1595). The same 0.04 s run on
main's ATA file and with #1587's TLB file reverted logs the same events at
the same clocks. Every flagged store is a real-mode stack store (`PUSH`,
`CALL`, SS=0) in BIOS code. The CPU reports its physical page
(`trace_store_pg0`, `wb_p0`) as the code's page (F0h, F1h or F2h) instead of
the stack's. That came with paging (#1579), which landed after the #1589
report's base `d6589de42`. The #1589 run there had `smc_stores=0` through
the same POST clocks. This PR doesn't relax or change that check.

## Seal

Quartus 17.0.2, seed 1, from a clean checkout of `aa296803c`: package
`80a407d3907c914279a1e317c20941a1c147a606bf1c21d29fa07b86d837c37f`. Every
timing analysis passed at 61.44 MHz system, 74.27 MHz pixel and 12.29 MHz
audio. The worst slacks over the four corners: setup 0.782 ns, hold
0.044 ns, recovery 6.816 ns, removal 0.454 ns, minimum pulse width
1.122 ns.

## Kit check (`f1`, `f2`)

Kit B, core-only on baseline software `a8cd30c4` (no install, image change or
reboot), the shared store's xms games drive `e83b8185…`. `f1` launches
main's sealed package with #1553 (`90914417…`, before this fix). `f2`
launches this PR's package (`80a407d3…`). Each grabs HDMI through POST and
the boot menu to the prompt.

| Run | POST screen | Prompt | Frame SHA-256 (first 16): POST, prompt |
| --- | --- | --- | --- |
| `f1` (main) | `ata0 master` 127 MB, `ata0 slave: Unknown device`, booting about 2 s after launch | `C:\>` | `fcdbdb03f9f686be`, `e607d11aa6e9a9a1` |
| `f2` (this PR) | the same | `C:\>` | `fc26bb933cdb3a2e`, `bc1526056b8c641f` |

The kit doesn't hit the race on either package. Its top level sets
`ATA_RESET_CLOCKS` to 615, and the device 1 select reaches the drive
after completion there. Both packages take the same POST path that the
oracle bench takes. The kit run checks for regressions only. The bench above
is what shows the fix. After the runs, `f1-clean` and `f2-clean` removed the
FogCast data trees they had created: the game records, the disk deltas and
the staged drive image. Kit B was logged FREE, and its baseline is unchanged.
The claims and releases are in Kit B's status file.

Frames stay on Powerboat in `~/fes-scratch/ata1589/kit/runs`.
