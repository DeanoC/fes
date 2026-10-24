# HPS DDR and SDRAM read latency at 100 MHz (2026-10-25)

The i386 CPU design (DeanoC/fes#1068, `sources/misteross/docs/i386-cpu-design.md`)
puts guest RAM in HPS DDR on `fes.memory.hps-ddr` port 0, behind an L2 with
32-byte lines. That choice carries a check: "If DDR's first data takes much
more than about 40 cycles at 100 MHz, or Linux activity makes it erratic,
revisit the choice." This record measures it.

**Verdict (Linux idle; the loaded run is pending):** TBD

This is a **hardware diagnostic** on Kit B with an exact sealed package. It is
not package or image acceptance.

## Method

`fes.ramtest` gained a latency mode in this PR. After its pattern scans end,
it measures one memory at a time, in turn: DDR port 0, port 1, port 2, then
the SDRAM addon.

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
The frames are 4 s apart, so each shows a later window of every memory.

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

## Kit run

- **Kit:** Kit B (`192.168.10.85`, target `kit2`), claimed in
  `~/kit-status/kitB-STATUS.md`.
- **Baseline:** image `4db74864`, bundle `35d58489` selected and active,
  idle `64f1f16d`. `/run/mister-runtime-boot-hps-ddr` read `latched`.
- **Load path:** core only. The package went through a private FogCast host
  (the shared host's `fogcast-api-58f029e0` in the isolated container),
  then import, compatibility, entry and launch. No bundle install and no
  image or boot change.
- **Scans:** SDRAM 6/6 PASS and DDR ports 0-2 7/7 PASS with no errors. The
  first latency results appeared 107 s after launch. MB/s write/read: P0
  1572/1564, P1 707/650, P2 706/638.

## Results

### Linux idle

Run 1, 24 frames from 00:31:18 to 00:32:50 EEST. The kit's load average was
0.17 and `top` showed 86% idle. Each cell is the range over the 24 windows,
with the median of the 24 in brackets.

| Memory | First min | First median | First p99 | First max | Line min | Line median | Line p99 | Line max |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| DDR P0 (128-bit) | 16-17 (16) | 18-19 (18.5) | 45-51 (47) | 55-84 (65) | 17-18 (17) | 19-20 (20) | 46-52 (48) | 56-85 (66) |
| DDR P1 (64-bit) | 16 | 17-18 (18) | 32-51 (34.5) | 54-79 (65.5) | 19 | 20-21 (21) | 35-54 (38) | 57-82 (68.5) |
| DDR P2 (64-bit) | 16 | 17-18 (17.5) | 32-51 (38) | 54-77 (64.5) | 19 | 20-21 (21) | 35-54 (41) | 57-80 (68) |
| SDRAM addon | 11 | 11 | 39 | 46 | 206 | 206 | 242-243 | 242-243 |

In nanoseconds, DDR port 0's first data is 160-170 ns at best and about
185 ns typically. One read in a hundred takes 450-510 ns, and the worst read
in a window 550-840 ns. Port 0's second beat usually follows its first by one cycle.

SDRAM's line time is the cost of 16 separate accesses through a single-word
controller. It is not what a burst controller would give. Its first data,
11 cycles, is the reference point the design named. A line that meets a
refresh takes 36-37 cycles longer.

### HPS CPU and memory load

Pending.

## Incident

Run 1's load step did not run. A parsing bug in the driver script read its
own `exit 0` line as a fourth PID. The cleanup then ran
`kill 3192 3193 3194 0` over ssh. The `0` signalled the ssh session's process
group, which on this image includes the dropbear listener, so Kit B's sshd
stopped. The agent still reported idle, lease free, and the same bundle and
image. Kit B was logged HELD for a power cycle. The fixed driver
([`hil_lat.py`](hps-ddr-latency-2026-10-25/hil_lat.py)) passes `kill` only
PIDs above 1 from the PID file, one at a time.

## Evidence

[`hps-ddr-latency-2026-10-25/run1/`](hps-ddr-latency-2026-10-25/run1/):

- `10-idle.json`: the 24 idle frames, as read.
- `ocr-frames.txt`: the full text of every frame.
- `05-wait-106.png` (the first frame with results) and `10-idle-05.png`.
- `before-kit.txt`, `idle-top.txt`, `compat.json` and `launch.json`: kit
  state, load and package records.
