# pc386 governor: reference-only order and prefix probe

16 November 2026. This is a **reference-emulator diagnostic**, not a pc386 kit
result, a physical 286 measurement, or a fix. The earlier
[pitch investigation](../2026-11-15-pc386-u6-lute-pitch-cause/summary.md)
left the reference's extra calibration-loop cost underdetermined between table
entries and fetch effects. This probe tests two limitations of an additive
per-instruction cost model, without claiming which physical mechanism causes
the timing. It does not establish a table entry or select a repair.

## Result

Three repetitions in one 86Box boot gave identical output. Counts are complete
1,000-iteration blocks in 64 BIOS ticks. Clocks per iteration use
`64 * 65536 / 1193182 * 12500000 / (1000 * blocks)`; they include interrupt,
outer-loop, tick synchronisation and count-quantisation overhead.

| Loop | Blocks, each of 3 runs | Effective clocks/iteration |
| --- | ---: | ---: |
| BD: `dec [counter]; jne` control | 2436 | 18.038 |
| O1: `inc [target]; cmp [zero],0; loop` | 1330 | 33.038 |
| O2: `cmp [zero],0; inc [target]; loop` | 1416 | 31.031 |
| P0: `cmp [zero],0; dec [counter]; jne` | 1625 | 27.040 |
| P1: same as P0, one redundant DS prefix on `cmp` | 1416 | 31.031 |
| P3: same as P0, three redundant DS prefixes on `cmp` | 1071 | 41.027 |

BD exactly reproduces the earlier reference's 2436-block control. O1 is 2.007
clocks slower than O2, and P1 and P3 are respectively 3.991 and 13.987 clocks
slower than P0. Both differences pass the predeclared 0.2-clock and
three-times-within-variant-spread thresholds; within this single boot the
observed repeat spread is zero. `loop` consumes CX, not the flags from either
instruction, fixing the earlier record's invalid swapped-order experiment.
Loop starts are even, but the instruction order also changes the positions of
the instructions within the loop. Thus the pair establishes sensitivity to
sequence or position in this reference, not order in isolation. P0 reproduces
the earlier B5 result (1625 blocks) as an additional observation.

The current governor charges the same classes and data accesses in O1/O2,
and ignores prefix and length for P0/P1/P3. Its additive per-instruction model
therefore cannot express these reference differences. The prefix result can be
prefix decoding, fetch, or another emulator timing effect; it does not separate
them. It does not justify a per-byte cost or establish that any particular
change improves other games. Equal governor charges are not a prediction of
equal elapsed times on the kit: its instruction alignment and fetch pipeline
can itself stall on prefixes. These repetitions share one emulator, VM, boot
and clock setup and are not independent hardware validation. The effects in
these artificial loops have not been quantitatively attributed to the actual
Ultima VI pitch divergence. The kit was not touched.

## Method, source and frozen inputs

[method.md](method.md) contains the pre-measurement thresholds and hypotheses;
[govprobe.asm](govprobe.asm) is the whole probe. Before the run, hashes of the
method, source and assembled binary were printed in the live task transcript;
[pre-run-digests.txt](pre-run-digests.txt) retains those same hashes. They are a
record of the agent's pre-run step, not an independently timestamped Git
commit or external attestation. Assemble and check the binary with:

```sh
nasm -f bin -o GOVPROBE.COM govprobe.asm
sha256sum GOVPROBE.COM
# 11e35806b36651c8bbdf91feabde4d7aaccdcd722829131b76cf3ecee61d56ba
```

The run used NASM 2.16.03 (binary SHA-256
`d75404632d151de8438081b817d09f56ab9eb4f688a151f55897f2a5396bf01b`),
86Box 6.0 build 9001 (extracted executable SHA-256
`ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969`),
and the same 12.5 MHz `award286` interpreter, one wait state, 1024 KiB,
IBM VGA and XT-IDE configuration as the
[reference](../2026-11-14-pc386-u6-u7-reference/reference.md). The private
copy of the reference ROM tree had a SHA-256 of
`ec38293be553dabc711ef5dd62cb61b45167d2a00e04cc6c49fb8032aac09b94`
over the sorted `sha256sum` file list; this is not a digest of a ROM archive.
The exact [machine-checkable list](roms.sha256) contains 1,829 files. From the
copied ROM directory it was generated and rechecked with:

```sh
find . -type f -print0 | sort -z | xargs -0 sha256sum > /absolute/path/roms.sha256
sha256sum /absolute/path/roms.sha256
sha256sum -c /absolute/path/roms.sha256
```

The list output must be placed outside the ROM directory while generating it,
as it was in this run, so it does not hash itself. All 1,829 entries rechecked
successfully against the private copy after the run; this is not an
independently authenticated upstream ROM set.

The input disk after adding the probe and before boot hashed to
`cb2f500bd5c321f6a772aa5f4d302c0236178d6de3b359f8a93d540d2e7f0ae9`.
Neither the disk nor ROMs are committed.

The [sanitized initial 86Box configuration](86box.cfg.sanitized) is a
byte-for-byte reconstruction of the pre-run file except for the private disk
path. Restoring that path produces the SHA-256 recorded immediately before the
run, `c7263f8282f908cac9863f66c91f0fba54537486890424a824d9afb51fdfab75`.
The emulator rewrote its private config during execution, so the retained file
was reconstructed from the untouched source config and verified against the
pre-run hash. The VM's CMOS/NVR was copied before boot, but its pre-run bytes
were not separately hashed or retained; the copy was changed by the run. The
exact initial CMOS therefore cannot now be independently reconstructed from
this record. That is a reproducibility limit, not a claim that a current
scratch NVR file is the run's initial one.

A private copy of `~/fes-scratch/u6ref/refbox.py`, the emulator, ROM tree,
reference VM and disk was placed under `out/dev/pc386-gov-diagnostic`; the
copied config pointed to the copied disk, and `mcopy` added the binary. The
copies and disk preparation used these commands, with the probe assembled as
above:

```sh
probe_dir="$PWD/out/dev/pc386-gov-diagnostic"
cp -a --reflink=auto /home/deano/fes-scratch/u6ref/app "$probe_dir/app"
cp -a --reflink=auto /home/deano/fes-scratch/u6ref/roms "$probe_dir/roms"
cp -a --reflink=auto /home/deano/fes-scratch/u6ref/vm/u6cal-286 "$probe_dir/vm"
cp --reflink=auto /home/deano/fes-scratch/u6ref/drv/run-u6cal.img "$probe_dir/disk.img"
cp /home/deano/fes-scratch/u6ref/refbox.py "$probe_dir/refbox.py"
cp /home/deano/fes-scratch/u6ref/alsoft.conf "$probe_dir/alsoft.conf"
```

The copied VM's original `../../drv/run-u6cal.img` config value was replaced
with the absolute path to `"$probe_dir/disk.img"`. An mtools config containing
`drive x: file="<probe_dir>/disk.img" partition=1` was written, then
`MTOOLSRC="$probe_dir/mtools.conf" mcopy -o "$probe_dir/GOVPROBE.COM" x:/GOVPROBE.COM`
added the probe to the private disk. The
copied driver was changed only to use the private directory. A private `box.sh`
executes its copied `app/sq/AppRun`; `xvfb-run` supplies a fresh display. The
original driver is retained as [refbox-original.py](refbox-original.py),
SHA-256 `23b63d6f7e40974806b3def007c167b28cdad03f823ac23d3d0d4f2545dfdb94`.
The exact patch was the following string replacement, where `probe_dir` was
`/home/deano/.codex/worktrees/d31c/fes/out/dev/pc386-gov-diagnostic`:

```python
driver = driver.replace("U = Path('/home/deano/fes-scratch/u6ref')",
                        f'U = Path({str(probe_dir)!r})')
```

The resulting driver SHA-256 was
`d156b0ff9ae280774c644a8a4fbe02345560b4669c9c3e2934e19011241e526b`.
The launcher's exact bytes were `#!/bin/sh\n` followed by
`exec "<probe_dir>/app/sq/AppRun" "$@"\n`, with `<probe_dir>` expanded to
the path above; its SHA-256 was
`7776e17391e6560a9cd67a7a882dbc6920d63829095c4e24ef927d077f6901eb`.
The copied driver and launcher were rechecked against these hashes.

The
actual bounded invocation, from the repository root, was:

```sh
probe_dir="$PWD/out/dev/pc386-gov-diagnostic"
timeout 210s xvfb-run -a -s '-screen 0 1280x1024x24 -nolisten tcp' \
  sh -c 'exec python3 "$1/refbox.py" "$DISPLAY" "$1/vm" "$1/run1" "$1/commands.jsonl"' sh "$probe_dir"
```

[commands.jsonl](commands.jsonl) shows the 45-second boot wait and each DOS
command and capture; [driver.log](driver.log) records times. The driver ends
its own emulator process after the last capture. Output was extracted from the
private disk with mtools after the VM stopped: [G1.TXT](G1.TXT),
[G2.TXT](G2.TXT), [G3.TXT](G3.TXT). The [boot capture](boot.png) shows
FreeDOS ready and 86Box reporting 100%; the three subsequent captures show the
probe returning to DOS. A run-1 screenshot shows 97%, so no claim of
continuously real-time host speed is made. The guest's own BIOS ticks, not
wall-clock sleeps, bound the loops. The raw emulator's nonfatal renderer/audio
messages are in [box.out](box.out).

Next, a proposed governor change needs an independently chosen cost model and
regression measurements across representative instructions, presets and
titles, followed by its normal seal and exact-artifact kit comparison. These
observations alone cannot close #1368 or promote the DE10 cell to tier 1.
