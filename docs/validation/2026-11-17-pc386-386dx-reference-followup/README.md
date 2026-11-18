# pc386: two further 386DX-25 reference screens

17 November 2026. These are **86Box reference screens**, not kit, game-corpus,
or tier-1 acceptance. The previously fixed [board-selection rule](../2026-11-14-pc386-u7-npc-pace/method.md#the-reference-board-and-the-rule-it-was-chosen-by)
asks for a cached 386DX at 25 MHz near the middle of scaled real machines'
TopBench distribution: its interquartile range is **47–65**, median 57. Neither
configuration below reaches that range, so neither is selected. This rejects
only the tested configurations; it does not rule out other BIOS settings,
boards, 86Box versions or references.

| Configuration | Same-boot TopBench 0.38a | Score evidence | 100% speed samples |
| --- | ---: | --- | ---: |
| `spc6000a`, Samsung SPC-6000A | **36** | [start](samsung/bench-start.png), [final](samsung/final.png) | 152/152 |
| `dataexpert386wb`, AMIBIOS 050591 | **40** | [start](ami/bench-start.png), [final](ami/final.png) | 152/152 |

The two screenshots per machine are iterations within **one boot**, not
independent repeats. The score was read visually from the captures; the
verifier does not OCR it. The Samsung result is a prompt-aware repeat of
[#1927's no-score attempt](../2026-11-16-pc386-more-386dx-candidates/summary.md#screens):
that attempt sent input before DOS was ready. This run required two matching
BIOS and DOS prompt samples and independently reviewed the two directory
prompts before issuing TopBench. The actual [plan](samsung/PLAN.md),
[driver](samsung/run.sh), [gate program](samsung/prompt_gate.py), templates,
[gate logs](samsung/prompt-gate.log) and full [screen recording](samsung/screening.mkv)
are retained. No Samsung cache setting was verified.

The DataExpert model was previously screened with its default MR BIOS at 36;
this is its distinct **AMIBIOS 050591** configuration. A preceding private setup
boot showed [Cache Memory: Enabled](ami/setup-cache.png) and saved the CMOS used
for this run. On the benchmark boot a small guest [probe](ami/cache.asm) read
OPTi register 21h as [`50h`](ami/cache.png), with bit 0x10 set. The corresponding
[86Box 6.0 model](https://github.com/86Box/86Box/blob/4fef696a4eead1d55a28d6ac0e5bd2864e5454da/src/chipset/opti391.c)
connects that bit to the external-cache flag **when written**. The value read
and BIOS setting do not directly observe the effective CPU flag, and the
emulator binary was not independently rebuilt from that source. The probe
writes the index selector and cannot prove it restored the original latch;
it does not write the data register. The included [disassembly](ami/probe-disassembly.txt)
confirms the recorded binary uses those ports and index; NASM was unavailable
for a fresh source-to-binary rebuild in this checkout. These limits do not
affect the finding that this particular observed score is below 47.

The first AMI benchmark attempt stopped at its DOS review gate with no benchmark
input. Its retained [gate frame](ami/old-gate.png) was originally described as
black because the review display rendered the guest area that way. A later
inspection, including a [losslessly enlarged copy](ami/old-gate-review-2x.png),
showed the DOS prompt in the unchanged PNG. [The correction](ami/CORRECTION.md)
records the file hashes and does not change that attempt's outcome. The new
[score plan](ami/SCORE2-PLAN.md) was frozen before the measured run; it retains
native screenshots and enlarged review copies. The [driver](ami/score2.sh),
[heartbeat](ami/heartbeat.log), [recording](ami/discovery.mkv), [DOS](ami/dos.png),
[probe](ami/cache.png) and [directory](ami/topbench.png) captures are retained.
The staged driver digest in [the frozen manifest](ami/SCORE2-FROZEN.sha256)
matches the retained `ami/score2.sh` byte for byte (SHA-256
`7c39d9caafa80e19d426e7143fe2f887c26579acb4bb0ac78c6c67fa23aef6bb`). The
[supervisor](ami/supervise-score2.sh) verifies that digest, logs the driver path
and digest, and invokes that retained script under the 480-second cap. The
retained `supervisor.log` records only the old wrapper's exit status; it does not
record the digest of its root-local `bench.sh`, so that log alone cannot prove
which driver bytes the historical run executed. A replay with the corrected
supervisor will identify the exact driver. To repeat the workflow, run
`bash ami/supervise-score2.sh /absolute/private-run-root`; the root must contain
the private `vm/`, `app/` and `roms/` inputs described in the frozen plan. Those
private inputs remain omitted, so this record does not by itself reconstruct the
full run environment.
The review display sometimes rendered even enlarged dark frames incorrectly,
so the independent reviewer checked the native final frame as well. The AMI
run's score captures and the 100% speed sample were independently checked.

## Inputs and speed evidence

Both runs used the interpreter in 86Box 6.0 build 9001 on the 3090 host,
`i386dx` at 25,000,000 Hz, 8 MiB RAM, IBM VGA, XT-IDE and Sound Blaster 2.0.
Their actual [Samsung](samsung/pre-86box.cfg) and [AMI](ami/pre-86box.cfg)
configs, pre-run CMOS copies and input-digest records are retained. The Samsung
pre-run benchmark disk had SHA-256
`279ac36b8cbf0a77c99899e60be45e975fd33a828e725dd13568e451eee90027`; the AMI
disk was a separate copy with the named probe added and had SHA-256
`802fccc74abf296ed4f96bbe7343bd4685bf5566246a9a481901b6b2ccd3f073`. On each,
`TOPBENCH.EXE` extracted before launch had SHA-256
`4428fc27d5c2987e5bd177540843190ebb33f31ca6ddacc6e9f8fb994076e63d`.
The disks, emulator executable and ROM bytes are **not included** here: those
input hashes record the retained private snapshots, and the verifier cannot
rehash their omitted bytes. The emulator executable hash was
`ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969`.
The copied ROM tree was checked before and after each run against the prior
[1,829-entry manifest](../2026-11-16-pc386-386dx-reference-candidates/roms.sha256)
(SHA-256 `ec38293be553dabc711ef5dd62cb61b45167d2a00e04cc6c49fb8032aac09b94`);
the quiet post-run logs are empty. The complete private snapshots remain under
`out/dev/1902-samsung-rerun/` on Powerboat and
`/data/fes1902-dataexpert-ami-score2-20261117/` on the 3090 respectively.

Both recordings are 800×520, 4 fps, H.264 CRF 18. From each, a contiguous
38-second interval wholly inside visible TopBench execution was cropped around
the speed indicator and decoded to 152 frames. Every thresholded frame in each
interval matches its independently inspected 100% indicator: Samsung uses
100–138 s from capture start; AMI uses 245–283 s. Their benchmark-command
logs and visible captures bracket those intervals. The [Samsung](samsung/speed.mkv)
and [AMI](ami/speed.mkv) clips are lossless relative to their decoded recordings.
This checks **sampled** indicator output, not the speed between samples or the
accuracy of the indicator. [`verify.py`](verify.py) rehashes included evidence,
checks the configs and recorded input digests, and regenerates the thresholded
frame hashes from the full recordings. It does not establish omitted disk or
ROM contents, visually read scores, or establish effective cache.

Both boot screens report that UMB RAM was unavailable at C800. Neither run
launches Ultima VII, measures a game's pace, tests a kit or resolves [#1853](https://github.com/DeanoC/fes/issues/1853).
No IBM reference disk, GPU or kit was used.
