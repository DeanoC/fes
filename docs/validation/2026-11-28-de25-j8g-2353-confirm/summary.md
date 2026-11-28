# DE25 pc386 486SX: the #2353 fix confirmed on J8G

A confirm run on Kit D25 (`de25-j8g`, serial `TRWU8J8G`) on 28 Nov 2026, 08:29–08:37 +02:00, with Deano's OK in the session ("OK, run it"). It follows the [diagnostic run](../2026-11-27-de25-j8g-2353-relaunch-diagnostic/summary.md), which found that a load is black whenever the pc386 record it restores holds CMOS 0Fh = 09h.

**Result: PASS.** With the fixed BIOS, loads that restored 09h went live in the same lease and in a new lease. FreeDOS booted by 10 s, `ver` printed, and DOOM ran.

## What ran

- **Package:** `fes.pc386` 486SX `cc021eee…` (`.fcore` sha256 `98fb32ed…`). It was built from PR #2366 at `b7a74aba4e` with the new BIOS (`rom_hex` `abe6f29c…`) by Quartus Pro 26.3 on Powerboat, under the Quartus lease ([kit/package-job.py](kit/package-job.py); build record [kit/package-build-inputs.json](kit/package-build-inputs.json), design identity `f9b6393a…`). Later commits on the PR are evidence only.
- **Software and media:** run 3's bundle `69d7cfdc…`; the running `mister-runtime` hashed to `d44560bc…`, the same as in the diagnostic run ([kit/runtime-active.sha256](kit/runtime-active.sha256)). The FreeDOS games disk `b4be6e29…` and library game `run3-486sx`, as before.
- **Image:** baseline `a1c51ac3…`. Nothing was flashed and the kit was not rebooted.
- **CMOS 0Fh:** read after each Stop from the newest saved record, `record.bin` byte 128 ([kit/cmos0f.sh](kit/cmos0f.sh)). Each reading came with the record's write time, after that Stop, so no reading was stale.

## Loads

| Lease | Load | Restored CMOS 0Fh | Result | Stop saved |
| --- | --- | --- | --- | --- |
| 1 | 1 | (earlier record) | live by 10 s, `ver` | **09h** after DOOM ([kit/cmos-after-p1-stop.txt](kit/cmos-after-p1-stop.txt)) |
| 1 | 2 | **09h** | **live** by 10 s; `ver` changed the frame | 00h ([kit/cmos-after-p2-stop.txt](kit/cmos-after-p2-stop.txt)) |
| 1 | 3 | 00h | live | **09h** after DOOM ([kit/cmos-after-p3-stop.txt](kit/cmos-after-p3-stop.txt)) |
| 2 | 1 (**new lease**) | **09h** | **live** by 10 s; `ver`; DOOM ran into play | 00h ([kit/cmos-after-q1-stop.txt](kit/cmos-after-q1-stop.txt)) |

On main's BIOS, loads 1-2 and 2-1 are the black loads (diagnostic run loads 1-4 and 4-1). On this BIOS, the frames at 10, 15 and 30 s match the first load's live FreeDOS prompt (mean luma 14.9, 23.7% lit), and typing `ver` changed 18% of the frame ([kit/frames-c1.jsonl](kit/frames-c1.jsonl), [kit/frames-c2.jsonl](kit/frames-c2.jsonl)). The 00h saved after load 1-2 shows its POST cleared the restored 09h, as the `system_tb` check expects.

Sample frames: [load 1 at 10 s](frames/c1-p1-t10.png), [DOOM before the 09h Stop](frames/c1-p1-d1-doom.png), [load 2 at 0 s](frames/c1-p2-t00.png), [load 2 at 10 s](frames/c1-p2-t10.png), [load 2 after `ver`](frames/c1-p2-t-ver.png), [new lease at 10 s](frames/c2-q1-t10.png), [new lease `ver`](frames/c2-q1-t-ver.png), [new lease DOOM](frames/c2-q1-d2-doom.png).

Commands, acknowledgements and driver logs are in `kit/commands-c*.jsonl`, `kit/acks-c*.jsonl` and `kit/driver-c*.log`.

## Restore

- **Leftovers:** the diagnostic run's `/tmp/record.bin` on the kit was removed at the claim ([kit/tmp-record-removed.txt](kit/tmp-record-removed.txt)). This run's save data for `run3-486sx` was deleted ([kit/cleanup.txt](kit/cleanup.txt)).
- **Bundle:** baseline `cbf46803…` reactivated, selected with nothing pending ([kit/software-status-restore.json](kit/software-status-restore.json)).
- **Verifier:** `de25_restore.py` passes with no failed checks ([kit/restore-verification.json](kit/restore-verification.json)). `boot_id` is still `7fe4f294…`, and the native read-only state is unchanged ([kit/native-diff.txt](kit/native-diff.txt) is empty).
- **MENU:** the frame after the restore has the same metrics as the one before the run ([frames/hdmi-after-restore.png](frames/hdmi-after-restore.png)).
- **FREE:** logged with Deano's OK at 08:37:21 +02:00. The claim holder, UART reader and private host were stopped, and all three J8G locks were checked free.

The full evidence, including full-size frames, is on Powerboat in `/data/de25-2353-confirm`.
