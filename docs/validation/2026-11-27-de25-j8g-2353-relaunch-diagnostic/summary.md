# DE25 pc386 486SX: the #2353 "relaunch in the same session is black" diagnostic on J8G

A diagnostic run on Kit D25 (`de25-j8g`, serial `TRWU8J8G`) on 27 Nov 2026, 14:48–15:03 +02:00. Deano gave his OK in the session ("OK, run it").

**Result: the lease session plays no part.** A load is black when the pc386 record it restores holds CMOS 0Fh (the shutdown status) = 09h. DOS/4GW writes that value while DOOM runs. A Stop that catches it saves it in the record. The next launch's BIOS POST reads 09h as a resume request. The Bochs BIOS has no handler for code 09h, so `shutdown_status_panic` halts before video is set up. That POST also clears the byte, so the load after it is live. All three earlier occurrences matched this pattern: the first load ran DOOM, the relaunch was black, and the next fresh-session load was live.

## What ran

- **Source and runtime:** main `ebe9d522ee`, run 3's bundle `69d7cfdc…` (`bundle.tar` sha256 `0ab97aec…`). The running `mister-runtime` hashed to `d44560bc…`, the same as run 3 ([kit/runtime-active.sha256](kit/runtime-active.sha256)).
- **Package and disk:** `fes.pc386` 486SX package `36540290…` (BUILD_ID `296e4bce…`) and the FreeDOS games disk `b4be6e29…`, as in run 3.
- **Image:** baseline `a1c51ac3…`. Nothing was flashed and the kit was not rebooted.
- **Tooling:** run 3's driver, a read-only probe ([kit/diag.sh](kit/diag.sh)) and frame metrics ([kit/frames.py](kit/frames.py)): mean luma and the fraction of pixels above 16. A frame of luma 2.93 with 0% lit is solid black.
- **CMOS 0Fh:** read from the saved record (`core-data/games/eee95a7e…/record.bin`, byte offset 128, which is CMOS 0Fh) after each Stop.

## Loads

| Lease | Load | What came before it | Restored CMOS 0Fh | Result |
| --- | --- | --- | --- | --- |
| 1 | 1 | first load after the bundle activation | (earlier record) | live by 10 s |
| 1 | 2 | Stop 3 s earlier; no input yet in this lease | 00h | **live** by 10 s |
| 1 | 3 | input attached, `ver` typed, Stop, 3 s gap | 00h | **live** by 10 s |
| 1 | 4 | DOOM ran (title and demo), Stop, 3 s gap | **09h** ([kit/record-after-doom-stop.txt](kit/record-after-doom-stop.txt)) | **black** at 0, 10 and 30 s |
| 1 | 5 | load 4's Stop | 00h ([kit/record-after-black-stop.txt](kit/record-after-black-stop.txt)) | **live** by 10 s |
| 2 | 1 | new lease | 00h | live; DOOM ran, but this Stop saved 00h |
| 3 | 1 | new lease | 00h | live; DOOM ran into play, and this Stop saved **09h** |
| 4 | 1 | **new lease**, about 20 s after lease 3's Stop | **09h** | **black** at 0, 10 and 30 s |
| 4 | 2 | load 1's Stop, same lease | 00h | **live** by 12 s |

Frame metrics are in `kit/frames-l1.jsonl` to `kit/frames-l4.jsonl`, and the driver logs in `kit/driver-l*.log`. Sample frames: [load 2](frames/l1-l2-t10.png), [DOOM](frames/l1-l3-d2-doom.png), [black load 4](frames/l1-l4-t30.png), [load 5](frames/l1-l5-t10.png), [lease 4 black](frames/l4-p1-t30.png), [lease 4 live](frames/l4-p2-t12.png).

What each part of this shows:

- **Loads 1-2 to 1-3** rule out the lease session, a short gap since Stop, and earlier input. Load 1-2 had all three conditions the issue listed apart from DOOM, and it was live.
- **Loads 1-4 and 4-1** show the black load follows the restored 09h, whether in the same lease or in a new one.
- **Loads 1-5 and 4-2** are live with the very next load in the same lease, after the black load's own POST cleared the byte to 00h.
- **Whether a DOOM Stop catches 09h is down to timing.** Lease 2's DOOM Stop saved 00h and the next load (lease 3) was live; leases 1 and 3 caught 09h. In run 3, the underflow run and the s4g run, the first load ran DOOM and was stopped mid-game. The black relaunch that followed each time, and the live fresh-session load after it (run 3's B and s4g's pass 2), fit this pattern. Those runs did not read their records, so it is not shown for them directly.
- **The guest CPU does not run during a black load.** In load 1-4, `echo BLK > C:\BLK.TXT` was typed blind and Stop followed. The disk's media record stayed at revision `5b3c796f…`, so nothing was written.
- **The record otherwise only moves the RTC clock bytes:** CMOS 00h and 02h (seconds and minutes) and the trailing digest. Nothing else in it changed between the DOOM Stop and the black Stop.

During the black load 1-4, the probe ([kit/diag-l4-black.txt](kit/diag-l4-black.txt)) showed:

- `/v1/status` active, generation 4, `fes.pc386` observed, no `last_error`;
- runtime events with no error entries;
- dmesg showing only the usual FPGA manager and overlay lines.

The fault is in the guest, not in the runtime, the window or the scanout.

## The cause in the BIOS

pc386's BIOS is Bochs `rombios.c`. Its POST reads CMOS 0Fh on every reset and acts on it:

- 05h, 0Ah, 0Bh and 0Ch jump through 40:67h;
- other nonzero codes below 0Dh, including 09h, go to `shutdown_status_panic`.

On a launch, guest RAM has just been cleared, so either branch leaves a black screen. The IBM PC/AT BIOS reads the shutdown byte only when the 8042's system flag (status bit 2) is set (TEST1.ASM, "CHECK FOR PROCESSOR SHUTDOWN": `IN AL,STATUS_PORT` / `TEST AL,SYS_FLAG` / `JMP SHUT0`). That flag is clear at power-on and set by POST's command byte (45h). pc386's 8042 (`pc386_kbc.sv`) already models the flag that way, but the Bochs POST never tests it, and its `keyboard_init` writes command byte 61h, which clears the flag. The fix is in this PR.

## Restore

- **Save data:** this run's save data for library game `run3-486sx` (`core-data/games/eee95a7e…` and `core-data/media/1f92c9a8…`) was deleted ([kit/cleanup.txt](kit/cleanup.txt)).
- **Bundle:** baseline bundle `cbf46803…` reactivated, selected with nothing pending.
- **Verifier:** `de25_restore.py` passes with no failed checks ([kit/restore-verification.json](kit/restore-verification.json)). The native read-only state is unchanged and `boot_id` is still `7fe4f294…` ([kit/native-diff.txt](kit/native-diff.txt) is empty).
- **MENU:** the frame after the restore has the same metrics as the one before the run.
- **FREE:** logged with Deano's OK at 15:03:49 +02:00. The claim holder and the private host were stopped, and all three J8G locks were checked free.
- **One leftover:** the probe left a copy of the record in the kit's `/tmp` (`/tmp/record.bin`, tmpfs). It goes at the next reboot, or at the confirm run's claim.

The full evidence, including full-size frames, is on Powerboat in `/data/de25-2353-diag`.
