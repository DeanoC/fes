# pc386: Windows for Workgroups 3.11 in the test corpus, 12 November 2026

This record covers the first runs of the Windows corpus checks
([`pc386-windows-corpus.md`](../../../sources/misteross/docs/pc386-windows-corpus.md),
[#1763](https://github.com/DeanoC/fes/issues/1763)). It changes no RTL.

## The drive

`wfw-msdos`, SHA-256
`a2afd3c7689526eba919cfbfe77810f4751525dea1827d0c196dd81e0c8d0c6c`, in
`~/fes-drives/pc386` on Powerboat. It is the plan record's `win-msdos-32bit`
(`54dad425…`: MS-DOS 6.22 and WfW 3.11 as SETUP left them, with WDCTRL
configured on), plus a permanent swap file. The swap file was made in QEMU with
Windows' own Virtual Memory dialog: Permanent, 20,480 KB, "Use 32-Bit Disk
Access" checked, then Restart Windows.
- QEMU's IDE fails WDCTRL's validation ("phase 07, 30"), so Windows ran
  without it there.
- SYSTEM.INI kept `32BitDiskAccess=on`, `*int13` and `*wdctrl`. It gained
  `PermSwapDOSDrive=C` and `PermSwapSizeK=20480` in place of the temporary
  swap's `PagingFile` and `MaxPagingFileSize`.
- `C:\386SPART.PAR` is 20,967,424 bytes. `chkdsk c:` found no errors.
- The drive is commercial and is never committed.

## Kit acceptance

`scripts/pc386_wfw_kit_acceptance.py` on Kit A, core-only on baseline software
`a2756499` and image `179affbb…`, under a status-file claim. A scratch wrapper
(`~/fes-scratch/wfwcorpus/run-wfw.sh`) took each claim, checked the baseline
before and after, and logged the release.

| Run | Package | Result | Notes |
| --- | --- | --- | --- |
| `d1` | `27f478e0…` (#1760's seal of main `7e08be726`) | fail | Program Manager came up, but the detector missed it on Kit A's capture: the scaled "Main" caption split one row of the group title bar. Fixed in the band rule. |
| `d2` | the same | fail | All four goals passed. The exit to DOS was missed: the runner cut the near-empty `C:\>` screen to its text before judging it. Fixed: text screens are judged on the whole grab. |
| `d3` | the same | fail | All four goals passed. The read-back stopped on the scrub boot's media directory, which holds no delta record. Fixed: such directories are skipped. |
| `d4` | the same | **pass** | [`kit-a-d4.json`](kit-a-d4.json) |

`d4` in detail:
- `program-manager`: detected 5.2 s after `win` was typed, at a grab every
  second or so.
- `mouse`: the pointer moved 0.25 counts per pixel on both axes. It landed at
  (208, 65) for the target (203, 61) on the title bar, and the double-click
  maximized Program Manager.
- `dos-window`: a full-screen MS-DOS Prompt opened in 3.2 s, and `exit` was
  back at Program Manager in 2.9 s.
- `notepad`: saved and exited, back at Program Manager in 2.7 s.
- The read-back applied the run's 10 delta chunks to a private copy of the
  drive. `C:\WFWDOS.TXT` read `FESWFW DOS WJR00M435KED` and
  `C:\WFWNOTE.TXT` read `FESWFW NOTE WJR00M435KED`: both hold the run's nonce.
- The runner removed the five FogCast data trees the run created: two game
  records, two media directories and the staged drive. Afterwards the agent
  was idle with a free lease, and the kit had not rebooted. The wrapper's
  find listing matched the pre-run one, and the baseline snapshot keys
  matched.
- The whole run, the scrub boot included, took about 3.5 minutes.

Every kit run is listed in Kit A's status file with its claim and release.
The kit check on a seal of current main is below.

**Seal of main.** Package `28441b4e6f8ea42581a4bc1ee597915232bb00b6837eb1d804ab4168498d5477`
([`seal-summary.json`](seal-summary.json)):
- a Quartus 17.0.2 seal (seed 1) of main `f793494f2`, from a clean clone, with
  `scripts/build_fes_pc386_quartus.py --gpu-router CUDA` on chip-30x0-ai-dev
  (`gpu_backend: cuda`), `build_id` `9171384593786428d7bb1ab7e479ec31`;
- every analysis met: setup +0.821 ns, hold +0.091 ns, recovery +6.898 ns,
  removal +0.297 ns, minimum pulse width +1.122 ns;
- 36,864 of 41,910 ALMs, 269 of 553 RAM blocks, 38 DSP blocks.

Main `f793494f2` changes nothing under `sources/misteross/cores` since
`7b33b38bf`, the tree the simulation below runs.

| Run | Kit | Result | `win` to detection | Pointer (target) | DOS window open / back | Notepad back | Read back |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `k1` | B (software `7ddb0bfb`, image `179affbb…`) | **pass** ([`kit-b-k1.json`](kit-b-k1.json)) | 5.3 s | (208, 64) for (203, 60) | 4.4 s / 3.9 s | 2.9 s | both nonces, 10 delta chunks |
| `k2` | A (software `a2756499`, image `179affbb…`) | **pass** ([`kit-a-k2.json`](kit-a-k2.json)) | 6.0 s | (208, 65) for (203, 61) | 3.7 s / 3.7 s | 3.1 s | both nonces, 10 delta chunks |

On both kits the pointer moved 0.25 counts per pixel, and each runner left
no FogCast data tree behind. Afterwards the agent was idle with a free lease
and had not rebooted, and the wrapper's baseline keys matched. The "`win` to
detection" times include the grab cadence (about one grab a second, each
taking about a second). They are not a measure of Windows' start-up: the
#1760 record's frame-by-frame 2.0–2.5 s is.

## Simulation

Pending: `sim_fes_pc386.py --suite wfw --cpu rtl` on main `7b33b38bf` with this branch's sim changes, running on chip-30x0-ai-dev.
