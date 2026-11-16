# pc386: four more 386DX-25 reference configurations

16 November 2026. This is a reference-emulator screen, not game or kit
acceptance. The existing [selection rule](../2026-11-14-pc386-u7-npc-pace/method.md#the-rule)
requires a cached 386DX at 25 MHz near the middle of the scaled real-board
TopBench distribution: its lower quartile is 47 and median is 57. Four more
configurations of 86Box 6.0 were screened. Their displayed scores are all below
47. **No board is selected**, and this does not rule out other configurations
or versions of 86Box. The effective cache state was not measured.

## Screens

| 86Box machine | 25 MHz TopBench | Score capture | 100% speed samples |
| --- | ---: | --- | ---: |
| `micronics386`, Tandon BIOS | 40 | [start](micronics386/start.png), [later](micronics386/final.png) | 152/152 |
| `micronics386px`, Phoenix BIOS | 36 | [start](micronics386px/start.png), [later](micronics386px/final.png) | 152/152 |
| `tandy4000` | 36 | [start](tandy4000/start.png), [later](tandy4000/final.png) | 152/152 |
| `deskpro386` | 36 | [start](deskpro386/start.png), [later](deskpro386/final.png) | 152/152 |

`start.png` and `final.png` show iterations in the **same boot**, not independent
repeats. `deskpro386` uses 86Box's distinct `i386dx_deskpro386` CPU family;
the other three use `i386dx`. The Phoenix and Tandy BIOSes reported invalid
CMOS on boot; the Deskpro asked for a setup disk. For each, the run continued
with the BIOS's offered F1 action, so the results are specific to those
conditions. The Tandon [setup screen](setup/tandon-cache.png) on a preceding
boot showed Cache Enabled before the saved CMOS was used for the screen; the
benchmark boot did not re-read that screen. None of these observations proves
86Box's effective CPU cache flag. The other three cache states are unknown.
The DOS boot text reports no upper memory at C800; no game was launched.

A fifth screen of `asus3863364k` displayed **40** ([start](asus3863364k/start.png),
[later](asus3863364k/final.png), 152/152 speed samples). It is a replication:
[#1838's earlier record](../2026-11-14-pc386-u6-u7-reference/reference.md)
already measured that model at 40. Its setup on a separate boot showed
[Cache Enable](setup/asus-cache.png), which does not establish the effective
cache during the score run.

The [Samsung SPC-6000A attempt](setup/samsung-no-score.png) produced **no score**:
its DOS boot took longer than the scripted input delay, so the benchmark keys
were sent before the prompt. Its [POST](setup/samsung-prompt.png) had reported
invalid CMOS, and this configuration remains unmeasured. A first Phoenix screen
also [stopped at its CMOS prompt](setup/phoenix-no-score.png); the table uses a
separate F1-continuation run. The complete failed and setup captures remain in
the private directory below.

## Inputs and procedure

The original band and 25 MHz clock were fixed before these screens. The
[plans](PLAN.md) record the selected batches; [PLAN-3](PLAN-3.md) removes an
`ecs386` run before it started after the earlier evidence was found, and
[PLAN-4](PLAN-4.md) covers Samsung, Tandy and Deskpro. The initial ASUS
candidate was found **after** its screen to have been measured already and is
reported as a replication above. No game pace was captured.

The runs used 86Box 6.0 build 9001 on `chip-30x0-ai-dev`, one emulator at a
time, the interpreter, 8 MiB RAM, IBM VGA, XT-IDE and Sound Blaster 2.0.
Actual pre-run config and CMOS, their digests, heartbeat, emulator warnings and
score captures are retained under each machine name. The pre-run disk is a
separate writable copy with SHA-256
`279ac36b8cbf0a77c99899e60be45e975fd33a828e725dd13568e451eee90027`;
`TOPBENCH.EXE` extracted from each of the five disk snapshots had SHA-256
`4428fc27d5c2987e5bd177540843190ebb33f31ca6ddacc6e9f8fb994076e63d`.
Neither the disks nor that executable are committed. The emulator binary's
SHA-256 was `ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969`.
The copied ROM tree passed the existing [1,829-entry manifest](../2026-11-16-pc386-386dx-reference-candidates/roms.sha256)
(SHA-256 `ec38293be553dabc711ef5dd62cb61b45167d2a00e04cc6c49fb8032aac09b94`)
before and after each scored run; no ROM bytes are committed. The executable
was not rebuilt from the cited 86Box source.

[drivers/](drivers/) retains the actual scripts. They copy inputs before each
run, launch the emulator and 4 fps 800×520 screen recorder, send the shown BIOS
and DOS keys and log a heartbeat. The recorder used H.264 CRF 18; each
`speed.mkv` is a lossless 65×22 crop of the **decoded** capture from 54–92 s,
or 104–142 s for Deskpro's longer boot. The input-time log places these in the
benchmark interval. All 152 thresholded frames per run match a visibly checked
100% indicator. Readout **between** samples and the accuracy of the indicator
are not established. The scores in the table were read from screenshots; the
verifier does not OCR them. Qt reported Vulkan/DRI3 and audio-capture warnings;
the configs select Qt software video, and the retained screenshots show the
target and benchmark running.

The first ASUS discovery stopped on the blank-CMOS prompt when the driver called
the unavailable `import` command. No benchmark ran. Its cleanup then hung on
its own emulator process, which was killed by exact PID; the exit-127 log and
capture remain private. The corrected screens used ffmpeg and distinct private
disks. Full screen captures, the pre-run disks, the failure and setup logs, and
the experiment index remain on Powerboat at
`/home/deano/.codex/worktrees/d31c/fes/out/dev/1902-tier-reference-survey/`.
The private ROM tree and emulator are retained on the 3090 under
`/data/fes1902-tier-survey-4oxl7m1s/`. These paths are evidence locations,
not distributed fixtures.

Run `python3 verify.py` in this directory to check the retained configuration
and CMOS against their recorded hashes, CPU and device selection, the recorded
private disk/executable/ROM-manifest identities, and the 152 decoded speed
frames for each run. The verifier does **not** check absent private disk or ROM
bytes, infer effective cache, establish guest game compatibility, or verify
speed between samples.

## Remaining models

The 86Box v6.0 [machine table](https://github.com/86Box/86Box/blob/4fef696a4eead1d55a28d6ac0e5bd2864e5454da/src/machine/machine_table.c#L7124-L8142)
contains 21 386DX-package model entries, including the Deskpro's special
package. Fourteen named models have now been screened across these and the
[previous records](../2026-11-16-pc386-386dx-reference-candidates/summary.md),
including their distinct measured BIOS entries. `portableiii386` only allows
20 MHz. Samsung remains unmeasured as above. Five MCA entries remain
unscreened: `ibmps2_m80`, `ibmps55_m50t`, `ibmps2_m70_type3`,
`ibmps2_m80_type3` and `ibmps55_m50v`. Those need a disk and C800 memory plan;
the existing ISA XT-IDE/expansion path cannot be assumed to work on MCA.

This inventory is a source-table check, not proof that every 86Box
configuration or another emulator has been exhausted. The Ultima VII reference,
game-corpus acceptance, and #1902 remain open.
