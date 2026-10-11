# RISC-V static-video diagnostic, 2026-10-11

The column-loss report in [#673](https://github.com/DeanoC/fes/issues/673)
was not reproduced. Numerical inspection of the preserved recordings does not
support the reported change between 3 and 8 seconds. This does not establish
a cause or exclude a failure outside the recorded intervals.

## Source and scope

Base FES commit: `896f33ea5102b49559b787ec1a98ef6914d1eab2`.
The change extends the misteross RISC-V system simulation: every static output
pixel is compared against the complete border, gradient and nine-glyph banner
for 600 consecutive frames after initialization, with the real CPU and timer
running. Only the stationary animated box footprint is excluded. Existing
movement, interrupt, clamping, hold and board-mailbox checks remain.
No production RTL, ALU, firmware, shared contract, recipe or image selection
changes. No new FPGA compilation was needed for this diagnostic.

The full-pixel check passed for **549,849,600 output pixels**. A temporary,
uncommitted RTL fault forced one off-centre column black (output x=164);
the new oracle rejected it at `(164,0)` in frame 3. The old centre sampling
would not sample that column. The injected fault was removed before the full
suite; [the rejection log](riscv-video-2026-10-11/mutation-check.log) is retained.

## Preserved evidence

Original captures and receipts remain in their original host directories.
[The artifact index](riscv-video-2026-10-11/artifact-index.json) records their
absolute paths, sizes and SHA-256 digests alongside the fresh captures.

- Baseline: source `896f33ea5102b49559b787ec1a98ef6914d1eab2`, package
  `72f3b2bed67dd9ddfe7d7404845f8456885947b43897cd546d10ea0b14f21809`,
  BuildID `39ccdf04c4d0877b75034c8319e2b099`.
- Candidate: source `322e38353f64b7a86752d5251670e5af07aa7cd8`, package
  `85bf8676b1638958d78262217f112697448e3c774b4bba4f8f443b906a9df1fb`,
  BuildID `c9dbfbd64f13d5fe6770d55b52c0eaf4`.

The original kit B baseline and candidate recordings have **zero static
capture pixels changing by more than 30 RGB levels** relative to the 3-second
frame at each sampled second from 3 through 9. All 1,512 banner cell centres
match the firmware glyph occupancy in every sampled frame, on both kits.
Kit A has a small number of changed capture pixels near the animated box;
there are no banner-cell mismatches. These are comparisons of captured pixels,
not reads of FPGA memory. Compression and scaling prevent exact RGB equality.

[The numerical analysis](riscv-video-2026-10-11/original-and-repeat-analysis.json)
includes full-resolution changed-column counts. Reproduce using Python 3 and
FFmpeg; the evidence-local script assumes the recorded 1920x1080 geometry,
checks every static capture pixel, excludes a four-pixel halo around the box,
and separately checks banner cell occupancy:

```sh
python3 docs/validation/riscv-video-2026-10-11/analyze.py /absolute/path/capture.mkv
```

Adjacent-frame differences were also measured for **every decoded frame after
3 seconds**, excluding a nine-pixel halo around the animated box:

```sh
ffmpeg -v error -i /absolute/path/capture.mkv \
  -vf 'trim=start=3,drawbox=x=897:y=477:w=126:h=126:color=black:t=fill,tblend=all_mode=difference,signalstats,metadata=mode=print:file=/tmp/riscv-adjacent.txt' \
  -f null -
```

[The adjacent-frame summary](riscv-video-2026-10-11/adjacent-summary.json)
records 204 comparisons for the original kit A video and 209 each for the other
five videos. The maximum luma difference is 6 in both original kit B videos,
15 in the original kit A video, and 6/7/7 in the fresh B MJPEG/B YUYV/A MJPEG
videos. This additional check found no abrupt static banner/background loss.

## Fresh exact-package hardware diagnostics

The unchanged baseline archive (SHA-256
`a30e75a67266a1bbb2e411293b8ee1b319123ad548add7300dcebc14bd84fc33`)
was validated locally, inspected on the target, then loaded under each target's
existing lease. Every load returned the exact package and BuildID above.
One operator performed the tests sequentially; there was no takeover, service
replacement, reboot, cable swap or appliance-image deployment.

| Kit / capture | Input | Duration | Sampled static changes | Cleanup |
| --- | --- | --- | --- | --- |
| B / ASUS 4KPRO | MJPEG 1920x1080, 30 fps | 10 s | zero above threshold | Stop idle, lease free |
| B / ASUS 4KPRO | YUYV422 1920x1080, 30 fps | 10 s | zero above threshold | Stop idle, lease free |
| A / ShadowCast 3 | MJPEG 1920x1080, 30 fps | 10 s | at most 29 capture pixels near box; no banner mismatches | Stop idle, lease free |

The exact [B MJPEG](riscv-video-2026-10-11/kit2-mjpeg-receipt.json),
[B YUYV](riscv-video-2026-10-11/kit2-yuyv422-receipt.json) and
[A MJPEG](riscv-video-2026-10-11/dev-mjpeg-receipt.json) receipts bind inspection,
load identity and cleanup. [The remaining capture analysis](riscv-video-2026-10-11/remaining-analysis.json)
contains the YUYV and A repeat results. `EOI missing, emulating` in an MJPEG
log alone is not evidence of lost static columns; uncompressed input provided
an independent encoding-path comparison on B. Kit/capture combinations were
fixed, so this does not separate board, cable and capture-device effects if a
future failure occurs.

Hardware classification: **contained exact-package developer diagnostics**.
No factory-image or full hardware acceptance, optimizer requalification or
candidate promotion is claimed. Keep #673 open for an objectively failing frame
or further observation; do not change the ALU based on the original report.

## Host validation

From `sources/misteross`, `python3 scripts/sim_fes_riscv.py` passed all six
cases (ALU, CPU with six randomized seeds, firmware, 600-frame system soak,
bus faults, and board shell). `python3 -m unittest tests.test_build_fes_riscv`
passed all seven producer tests. The deliberate off-centre RTL mutation failed
as expected and was reverted. `git diff --check` passed.
`make check` passed: package YAML valid, 18 generated consumers and 37 fixture
copies match, with zero copied source pins.
The full [simulation log](riscv-video-2026-10-11/full-sim.log) is retained.
