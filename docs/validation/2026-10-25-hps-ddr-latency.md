# HPS DDR and SDRAM read latency at 100 MHz (2026-10-25)

The i386 CPU design (DeanoC/fes#1068, `sources/misteross/docs/i386-cpu-design.md`)
puts guest RAM in HPS DDR on `fes.memory.hps-ddr` port 0, behind an L2 with
32-byte lines. That choice carries a check: "If DDR's first data takes much
more than about 40 cycles at 100 MHz, or Linux activity makes it erratic,
revisit the choice." This record measures it.

**Verdict:** DDR port 0 passes the 40-cycle check for typical reads, but its
tail does not.

- **Typical read:** first data takes 18-19 cycles at the median with Linux
  idle, and 19-29 cycles under heavy HPS CPU and memory load.
- **Tail:** 1% of reads take 47-51 cycles idle and 64-87 under load. The
  worst read in a window was 60-101 cycles idle and 103-135 under load.
- **Linux activity:** load widens the distribution but does not make it
  unbounded. The worst read seen was 135 cycles (1.35 us). The SDRAM addon is
  unaffected.

These are **hardware diagnostics** on Kits A and B with one exact sealed
package. They are not package or image acceptance.

## Method

`fes.ramtest` gained a latency mode in DeanoC/fes#1076. After its pattern
scans end, it measures one memory at a time, in turn: DDR port 0, port 1,
port 2, then the SDRAM addon.

- **Window:** 32768 single reads of one 32-byte line at pseudo-random,
  line-aligned addresses in that memory's span. One read is in flight, and
  nothing else touches DDR during a DDR window.
- **Line read, by memory:**
  - Port 0: a burst of two 128-bit beats.
  - Ports 1 and 2: a burst of four 64-bit beats.
  - SDRAM addon: the ramtest controller (CAS 2 at 100 MHz, single word,
    auto-precharge), so a line is 16 back-to-back halfword reads.
- **Counts, in 100 MHz cycles:** from the edge the read is taken to the edge
  of its first data, and to the edge of its last data.
  - On DDR the counts are taken at the core side of `fes_hps_ddr`. They
    include the guard's command register and its two read-data registers,
    which an i386 L2 on the same wrapper would also see.
  - On SDRAM a read starts on the first edge the controller samples the
    request, and data counts at each `done`. That is two cycles after the
    capture, because this controller precharges before it acknowledges.
- **Statistics:**
  - The minimum and maximum are exact.
  - The median and 99th percentile come from a histogram based on that
    memory's previous-window minimum. They are exact within 31 cycles of it.
    Beyond that each is the top of its bin, at most 1/8 of the offset high.
- **Data check:** every read checks its data against the last scan pattern,
  so the reads are known to hit the intended lines. No read failed.

The results appear on the core's HDMI status screen as `LAT` rows. Each frame
was captured from the kit's HDMI at 1920x1080 and read back with the core's
own 8x8 font ([`ocr_ramtest.py`](hps-ddr-latency-2026-10-25/ocr_ramtest.py)).
Frames are 4 s apart, so each shows a later window of every memory.

## Package

| Field | Value |
| --- | --- |
| Core | `fes.ramtest` 1.1.0, OSS 100 MHz (`make build-fes-ramtest-100`) |
| Source | FES `96b69412bcfd68965cdd1d31a77aa8c51dc5eaf0` (branch `deano/ramtest-latency`) |
| Package ID | `b467aa1494ac1e3df8c51abb536809876dc8a2ecfec85528141dcac74ac38a97` |
| Archive | `.fcore` sha256 `16b6fb2843edf3a961b88395a34e67d288f052176b56d2a14dbfe41030fdd4b6`, 2317824 bytes |
| Build ID | `17e52f8fca4ec732dd7f27cac76bae09` |
| RBF | sha256 `1ae2da920ba5436a734456d25d471d9fabdc272f3e4ce12aa569636499d7900c`, 2314137 bytes |
| Tools | `cores/fes-ramtest/toolchain.lock`: Yosys `3a5accbd`, nextpnr `9128800f`, Mistral `8fcc4cb4` |
| Route | seed 2 (first in the recipe order), HIP on GPU 0 (RX 7900 XTX) through `gpu-run` |
| Signoff | memory 101.80 MHz (100), pixel 78.36 MHz (74.25), capture 291.55 MHz (100). Seal checks unchanged |
| Size | 11310 COMB and 13617 FF (from 6945 and 8572); no M10K, MLAB or DSP |

## Kit A: matched idle and loaded runs

- **Kit:** Kit A (`192.168.10.186`, target `dev`), claimed and released in
  `~/kit-status/kitA-STATUS.md`.
- **Baseline:** image `4db74864` (previous `230eb8f5`), bundle `35d58489`
  selected and active (generation 435), idle `64f1f16d`, launcher
  `dbbb3ba7`. `/run/mister-runtime-boot-hps-ddr` read `latched`.
- **Load path:** core only. The package went through a private FogCast host
  (the shared host's `fogcast-api-58f029e0` in the isolated container),
  then import, compatibility, entry and launch. No bundle install, no image
  or boot change, no reboot.
- **Scans:** SDRAM 6/6 PASS and DDR ports 0-2 7/7 PASS, with no errors in
  any frame. The first latency results appeared 106 s after launch. MB/s
  write/read: P0 1558/1530, P1 746/653, P2 746/641.
- **Idle:** 24 frames from 05:04:26 to 05:05:58 EET. Kit A was not fully
  quiet: `fes-kit-software` used 38% of a core, the CPUs were 52% idle, and
  the load average was 1.79.
- **Load:** two `dd if=/dev/zero of=/dev/null bs=8M` writers stream 8 MiB
  buffers through memory, past the 512 KiB L2. A `cat` loop reads a 64 MiB
  tmpfs file. 24 frames were taken from 05:06:24 to 05:07:56 EET, starting
  15 s into the load. `top` showed both A9 cores 0% idle (50% user, 45%
  system), with the load average rising to 4.7.
- **After:** four frames after the load was stopped returned to the idle
  figures.
- **Restore:** the load ran in its own `setsid` session, separate from
  dropbear's. The stop killed only the three PIDs it had recorded, after
  checking each was in that session and was neither dropbear nor the stop
  script's shell. Nothing was left in the session afterwards.
  - A fresh ssh connection then succeeded.
  - The ramtest session was stopped and the private host removed.
  - Kit A came back to the same boot, image, software generation, idle and
    launcher, and was released idle and lease-free.

Each cell is the range over 24 windows, with the median of the 24 in
brackets. Cycles at 100 MHz.

**First data:**

| Memory | Condition | Min | Median | p99 | Max |
| --- | --- | --- | --- | --- | --- |
| DDR P0 (128-bit) | idle | 16-17 | 18-19 (19) | 47-51 (47) | 60-101 (74) |
| DDR P0 (128-bit) | load | 16-17 | 19-29 (23) | 64-87 (71.5) | 103-135 (119.5) |
| DDR P1 (64-bit) | idle | 16-17 | 17-18 (18) | 32-51 (43) | 62-99 (76) |
| DDR P1 (64-bit) | load | 16-17 | 19-29 (22.5) | 67-79 (71) | 103-134 (118) |
| DDR P2 (64-bit) | idle | 16 | 17-18 (18) | 32-51 (46) | 65-94 (75) |
| DDR P2 (64-bit) | load | 16-17 | 18-29 (24) | 63-79 (71) | 100-133 (116.5) |
| SDRAM addon | idle | 11 | 11 | 39 | 46 |
| SDRAM addon | load | 11 | 11 | 39 | 46 |

**Last data (whole 32-byte line):**

| Memory | Condition | Min | Median | p99 | Max |
| --- | --- | --- | --- | --- | --- |
| DDR P0 (128-bit) | idle | 17-18 | 19-20 (20) | 48-52 (48) | 61-102 (75) |
| DDR P0 (128-bit) | load | 17-18 | 20-30 (24) | 65-88 (72.5) | 104-136 (120.5) |
| DDR P1 (64-bit) | idle | 19-20 | 20-21 (21) | 35-54 (46) | 65-102 (79) |
| DDR P1 (64-bit) | load | 19-20 | 22-32 (26) | 70-82 (74) | 106-137 (121) |
| DDR P2 (64-bit) | idle | 19 | 20-21 (21) | 35-54 (49) | 68-97 (78) |
| DDR P2 (64-bit) | load | 19-20 | 21-32 (27) | 66-82 (74) | 103-136 (119.5) |
| SDRAM addon | idle | 206 | 206 | 242-243 | 242-243 |
| SDRAM addon | load | 206 | 206 | 242-243 | 242-243 |

In nanoseconds, port 0's first data is 160-170 ns at best and about 190 ns
typically with Linux idle, and about 230 ns typically under load. One read in
a hundred takes 470-510 ns idle and 640-870 ns under load. The worst read in
a window was 0.6-1.0 us idle and 1.0-1.35 us under load. Port 0's second beat
usually follows its first by one cycle.

SDRAM's line time is the cost of 16 separate accesses through a single-word
controller. It is not what a burst controller would give. Its first data,
11 cycles, is the reference point the design named. A line that meets a
refresh takes 36-37 cycles longer.

## Kit B: idle run

This was an earlier, separate run of the same package on Kit B
(`192.168.10.85`, target `kit2`). Baseline: image `4db74864`, bundle
`35d58489`, idle `64f1f16d`, launcher `5d4ee551`; HPS DDR `latched`. Scans
passed (SDRAM 6/6, DDR 7/7, no errors), and results appeared 107 s after
launch. MB/s write/read: P0 1572/1564, P1 707/650, P2 706/638.

24 frames were taken from 00:31:18 to 00:32:50 EEST, with a load average of
0.17 and the CPUs 86% idle. Each cell is the range over the 24 windows, with
the median in brackets.

| Memory | First min | First median | First p99 | First max | Line min | Line median | Line p99 | Line max |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| DDR P0 (128-bit) | 16-17 (16) | 18-19 (18.5) | 45-51 (47) | 55-84 (65) | 17-18 (17) | 19-20 (20) | 46-52 (48) | 56-85 (66) |
| DDR P1 (64-bit) | 16 | 17-18 (18) | 32-51 (34.5) | 54-79 (65.5) | 19 | 20-21 (21) | 35-54 (38) | 57-82 (68.5) |
| DDR P2 (64-bit) | 16 | 17-18 (17.5) | 32-51 (38) | 54-77 (64.5) | 19 | 20-21 (21) | 35-54 (41) | 57-80 (68) |
| SDRAM addon | 11 | 11 | 39 | 46 | 206 | 206 | 242-243 | 242-243 |

Kit B's idle figures agree with Kit A's.

Kit B's loaded run did not happen. A parsing bug in that run's driver read
its own `exit 0` line as a fourth PID, so the load step aborted before any
loaded frame. Its cleanup then ran `kill 3192 3193 3194 0` over ssh. The `0`
signalled the ssh session's process group, which on this image includes the
dropbear listener, so Kit B's sshd stopped at 00:33 EEST.

- **Kit state:** after the private-host Stop, the agent API still reported
  Kit B idle and lease-free, with bundle `35d58489` and image `4db74864`
  unchanged.
- **Waiting:** Kit B is logged in `~/kit-status/kitB-STATUS.md` as waiting
  for Deano's power cycle. No other recovery was attempted.
- **The fix:** the Kit A driver
  ([`hil_lat.py`](hps-ddr-latency-2026-10-25/hil_lat.py)) starts the load
  under `setsid` and records its session. Its stop script signals only
  recorded PIDs, and only after checking each is in that session and is
  neither dropbear nor its own shell. It never signals 0, -1, a negative
  PID or a process group, and it ends with a fresh ssh connection.

## Evidence

[`hps-ddr-latency-2026-10-25/`](hps-ddr-latency-2026-10-25/):

- `kitA/`: the Kit A matched pair.
  - `10-idle.json`, `20-load.json` and `30-after-load.json`: every frame, as
    read. `ocr-frames.txt` has the full text of each.
  - `10-idle-05.png` and `20-load-14.png`: one idle and one loaded frame.
  - `idle-top.txt`, `load-top.txt` and `load-top-end.txt`: the kit's CPU
    load.
  - `load-start.txt` and `load-stop.txt`: the load's session and PIDs, the
    checks and kills. `sshd-check.txt` is the fresh connection afterwards.
  - `baseline-before.txt`, `baseline-after.txt`, `software-before.txt` and
    `software-after.txt`: the restore check.
  - `compat.json` and `launch.json`: the package records.
- `kitB/`: the Kit B idle run, its frames and records, and the aborted load
  step's `load-start.txt` and `load-stop.txt`.
- `hil_lat.py`: the hardened driver used on Kit A. `ocr_ramtest.py` is the
  frame reader.
