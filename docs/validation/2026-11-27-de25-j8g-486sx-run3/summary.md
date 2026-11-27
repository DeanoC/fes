# DE25 pc386 486SX on Kit D25 (J8G), run 3: HDMI, HPS window, warm reset

Run 3 of the DE25 pc386 lane (#2029) was run on Kit D25 (`de25-j8g`, serial `TRWU8J8G`) on 27 Nov 2026, from main `ebe9d522ee`. Deano gave his OK in the session ("OK, run it").
The pass rules were written before the kit was touched: [PASS-RULE.md](PASS-RULE.md).

| Item | Result |
| --- | --- |
| 1. HDMI picture: lit, live and changing | **PASS** (launch A and fresh-session launch B) |
| 1b. Relaunch in the same session (recorded only, not a gate) | **FAIL**, reproduced for the third time today: [#2353](https://github.com/DeanoC/fes/issues/2353) |
| 2. Warm reset with a window machine loaded (#2246) | **PASS for an orderly `reboot`**: recovered in about 31 s, no SMMU events, owner probed, network and agent up, MENU on HDMI. **Not measured** for the #2245 case: an abrupt reset while the fabric is still mastering. The orderly shutdown stopped the machine first, so #2246's fabric reset was never exercised under mastering. |
| 3. HPS-window path (#2214, #2215, #2226) | **PARTIAL: not a full pass.** Rules 3a, 3c and 3e pass: ABI 1.1 admission with the window interfaces, FreeDOS from ATA, and after Stop no window fd, a free lease and no SMMU faults. **Not measurable:** rule 3b (the window's base and span observed inside the pool), because the owner ABI exposes no address, and rule 3d (a hold/drain/release sequence in the runtime events), because the runtime does not log those steps. Both are carried forward as open checks. |
| Restore | **Pass.** The baseline bundle and image are back. The baseline `boot_id` was rebased with Deano's OK, `de25_restore.py` passes, and `KIT D25 FREE (baseline verified)` was logged at 14:33:07+02:00. |

## Exactly what ran

- **Source:** `ebe9d522eee31beb1ddc25be5ea9a6662e08e060` (origin/main).
- **Package:** `fes.pc386` 486SX `36540290d89653c33de09e9e3600cc88adb5b316da2442cdc981ccc256b876e1.fcore`, sha256 `870f9cb2b6cd5aaf63d65c27df7f9f8942f24026af0421cecdacedd1268d9189`. Its design BUILD_ID is `296e4bce19f6bebe31d8e12b259e1ed6`.
  - It was built at `15c3b748`, the #2344 PR head, by the underflow run.
  - The 486sx design identity computed at `ebe9d522` is the same `296e4bce…`. So this package has main's design, but it is not an exact-main package byte-for-byte.
  - `/v1/status` reported the same `build_id` on the kit. Probe: [kit/build-id-probe.py](kit/build-id-probe.py). Its output is in [kit/build-id-main.txt](kit/build-id-main.txt), rerun at this docs commit, which changes no design input. The package's provenance is in [kit/package-provenance.json](kit/package-provenance.json).
- **Bundle:** `de25-run3-ebe9d522ee`, built from main with `scripts/kit_software.py` against the HILQ base `f6630dea…`.
  - Bundle id `69d7cfdcedd0d247fe5bc72c7b9f016ea88d79f4887452d6bb39f24bc8283518`, `bundle.tar` sha256 `0ab97aec87f2f2b9fc781ab867fea6e57aaebde10040fd8f4f955a4c8def1be3`.
  - `runtime-digest --head ebe9d522…` gave `d44560bc…a83`, and the running `mister-runtime` on J8G hashed to the same value ([kit/runtime-active.sha256](kit/runtime-active.sha256)).
- **Image:** the J8G baseline `a1c51ac3…` (kernel `6.12.11-fes-de25-owner`, reserved `fes-window-pool`). Nothing was flashed. Everything #2246 and #2344 changed since that image is on the fabric side (`hps/build.py`, `scanout/linux_dma.py`, `bridge_reset.py`, RTL), so it is in the package.
- **Disk:** the FreeDOS `pmode` image `b4be6e29…` (128 MiB, 260/16/63).

## Item 3: the HPS-window path (partial: 3b and 3d not measurable)

Launch A (session 1) — [kit/driver-a.log](kit/driver-a.log), [kit/kitstate-a-running.txt](kit/kitstate-a-running.txt), [kit/status-a-running.txt](kit/status-a-running.txt) (status with ABI and `build_id`, plus the runtime's open window fd):

- **Load and admission:** the load acked `active` in 10.81 s. `/v1/status` showed `observed_core: fes.pc386` and `abi fes.computer 1.1`, which is #2215's FogCast admission. The active interfaces included `fes.memory.hps-ddr 1.0` and `fes.fabric.de25-hps-window 1.0`.
- **Rule 3b, window inside the reserved pool: not measurable.** The rule required the window's base and span to be observed inside the pool, and no address was observed. What follows is supporting context, not 3b evidence:
  - The kernel reserves `0x85800000..0x957fffff (262144 KiB) map non-reusable fes-window-pool` at boot, and `/proc/iomem` lists that range as reserved.
  - The owner (`fes-de25-dma`, bound to `20010200.dma-owner`, iommu group 0) allocates the window from that pool. The pool holds exactly one window (`image/kernel/de25-dma/de25_dma_window.c`).
  - During the run, `mister-runtime` held an `anon_inode:fes-de25-window` fd.
  - The ABI deliberately exposes no physical address, so the pool is shown from the kernel's reservation and the owner's design, not from a reported address.
  - CMA is unaffected: `CmaFree` was 728 kB before and 7,592 kB while running, so the 256 MiB did not come from CMA.
- **FreeDOS booted from ATA:** the C: partition was seen, the FreeDOS boot menu passed, and the prompt appeared by 10 s after the load ack. Typing `ver` worked, and DOOM ran from the disk.
- **Stop:** it returned `{"state":"idle"}` and the runtime events show the reload of `fes.menu` and `fifo.consume stop ok`. The HDMI frame after Stop is byte-identical to the MENU frame before the load.
  - After Stop the runtime held no `fes-de25-window` fds ([kit/a-after-stop-window-fds.txt](kit/a-after-stop-window-fds.txt)), and the lease went to `free`.
  - Launch B's Stop repeated this: no window fds and lease free ([kit/b-end-release.txt](kit/b-end-release.txt)).
  - **Pass rule 3d's "hold/drain/release sequence in the runtime events": not measurable.** The runtime events log fence and FIFO operations but no window hold, drain or revoke steps.
  - What the release does show: the fd closed. Three later window allocations succeeded: the in-session relaunch, launch B and launch C. The pool holds exactly one window, so each of those needed the earlier window released.
- **SMMU:** dmesg held only the 4 arm-smmu probe lines throughout, with 0 events or faults (`kitstate-*.txt`).

## Item 1: HDMI picture

The pass rule used timed grabs, not `wait_stable`. Metrics are in [kit/frames-a.jsonl](kit/frames-a.jsonl) and [kit/frames-b.jsonl](kit/frames-b.jsonl): mean luma, the fraction of pixels above 16, and the fraction changed from the previous frame, all on a 160×90 gray downscale.

- **Launch A:** at 10, 20, 30, 45 and 60 s the frame shows FreeDOS text (luma 14.9, 23.7% lit). Typing `ver` changed 18.4% of the frame. The DOOM title and demo frames changed 54%, 50% and 21% between grabs. See [t10](frames/a-t10.png), [t-ver](frames/a-t-ver.png), [d1-doom](frames/a-d1-doom.png) and [d2-doom](frames/a-d2-doom.png).
- **Launch B (fresh lease session, after A's saved disk revision):** black at 0 s, which is the load moment. The FreeDOS prompt is up by 10 s, and `ver` changed 18.4%. See [b-t10](frames/b-t10.png) and [b-t-ver](frames/b-t-ver.png).
- **Scanline dropouts:** not checked systematically. The two full-size FreeDOS text frames inspected by eye (`a-t10` and `a-t-ver` at 1080p) show no dropout lines.

### 1b: relaunch inside the same session ([#2353](https://github.com/DeanoC/fes/issues/2353))

After A's Stop, a second load with the same lease token acked `active` in 8.62 s. Status was generation 2, `fes.pc386` observed, and the window fd was held. But every grab at 0, 5, 10, 20, 30, 45 and 60 s, and after `ver`, was solid black: luma 2.93, 0% lit ([r30](frames/a-r30.png), [r-ver](frames/a-r-ver.png)). The Stop after it was clean.

The same symptom appeared in the s4g run and the underflow run today. A fresh session is live even with the saved disk revision, so the fault follows the session, not the media.

## Item 2: warm reset with the 486SX loaded

- **Before the reset:** launch C (session 3) loaded the 486SX. FreeDOS was live at 15 s and `ver` changed the frame. Status was `active` with `fes.pc386` observed, `window_fds=1`, and 0 SMMU events ([kit/c-pre-reboot-status.txt](kit/c-pre-reboot-status.txt), [kit/kitstate-c-pre-reboot.txt](kit/kitstate-c-pre-reboot.txt)).
- **The reset:** one `reboot` over ssh at 2026-11-27T14:22:41+02:00, sent while the machine was running and not Stopped through the API. There was no SysRq and no retry.

**What the reset actually exercised.** `reboot` runs the init shutdown, and the UART ([kit/uart-reboot-segment.txt](kit/uart-reboot-segment.txt)) shows the runtime reacting to it:

- It saved the session (the ATA delta files are dated 12:22:42–43Z).
- It reprogrammed MENU: `fpga_manager … writing fes-runtime-core.rbf` at 34884.29 s, after `Stopping dropbear` and before `reboot: Restarting system` at 34889.24 s.
- **So the 486SX was not mastering when the HPS reset.** This run shows that an orderly reboot with a window machine loaded is safe. It does not test #2246's fabric reset (`h2f_reset` resetting the window while the machine masters).

**The #2245 hang was a different case.** In run 2 the machine was loaded at 67.7 s and the reset at 526.46 s was abrupt: `reboot: Restarting system` with no init shutdown and no MENU reprogram. It was followed by `arm-smmu-v3 … CMD_SYNC timeout` and the C_BAD_STREAMID flood on the next boot ([kit/run2-2245-abrupt-reset-excerpt.txt](kit/run2-2245-abrupt-reset-excerpt.txt), from `/data/de25-s4w-run/kit-run-107b23031/logs/uart-j8g.log`). An abrupt reset (for example `reboot -f`) with the machine mastering is what is still **not measured**.

Recovery from the orderly reset:

- **Network and boot:** the same target answered with a new `boot_id` `7fe4f294-65e1-48d7-a6a7-f74d70355ed7` about 31 s after the command ([kit/reboot-watch.txt](kit/reboot-watch.txt)).
- **SMMU:** 0 arm-smmu event or C_BAD_STREAMID lines on the UART from the reset onwards, only the 4 probe lines. dmesg agrees ([kit/dmesg-post-reboot-relevant.txt](kit/dmesg-post-reboot-relevant.txt)).
- **Owner probe:** `fes-de25-dma 20010200.dma-owner: Adding to iommu group 0` at 1.48 s. The driver is bound and the pool is reserved again ([kit/kitstate-c-post-reboot.txt](kit/kitstate-c-post-reboot.txt)).
- **Agent and fabric:** health reports `ready: true` with the lease `free` ([kit/c-post-reboot-agent.txt](kit/c-post-reboot-agent.txt)). The runtime programmed `fes.menu` at start, and MENU is on HDMI ([frame](frames/hdmi-post-reboot.png)).
- **Not exercised:** a window load after the reset.

## Restore

- **Saves deleted:** this run's save data for library game `run3-486sx` (`core-data/games/eee95a7e…`, `core-data/media/1f92c9a8…`) was deleted ([kit/cleanup.txt](kit/cleanup.txt)).
- **Baseline bundle reactivated:** bundle `cbf468031a31e6af75f385ced40b03f7d255bdf580c44353fb5518bf9736a1f6` is selected and active, with nothing pending (generation 573). The image `a1c51ac3…` is unchanged.
- **Restore verifier:** `scripts/de25_restore.py` against `current-restore-de25.json` ([kit/restore-verification.json](kit/restore-verification.json)) passes every check except `after_boot_id` and the native-state hash. The native read-only diff is exactly one line, the `boot_id` ([kit/native-diff.txt](kit/native-diff.txt)). The warm reset in item 2 changes this by design, and there is no serial receipt because the reset was not a serial reboot.
- **MENU:** the MENU on HDMI matches the pre-run frame (luma 230.92 both; [frame](frames/hdmi-after-restore.png)).
- **Baseline rebase:** with Deano's OK ("No: rebase and FREE"), `~/lander-hilq/de25_baseline_rebase.py --serial-recovery --pr j8g-run3-warm-reboot` rebased `current-restore-de25.json` from `boot_id` `2c4855a1…` to `7fe4f294…`. The backup is `current-restore-de25.before-boot-rebase-j8g-run3-warm-reboot-20261127T123236Z.json`. The tool's "serial SysRq" note is boilerplate: this was an ssh `reboot`.
- **Restore verifier after the rebase:** `de25_restore.py` with the rebased record and two fresh snapshots on the new boot gives `passed: true` with no failed checks ([kit/restore-verification-rebased.json](kit/restore-verification-rebased.json)). The final MENU frame matches the pre-run frame (identical metrics; OCR "FES Home"; [frame](frames/hdmi-final.png)).
- **FREE:** logged 2026-11-27T14:33:07+02:00. The claim holder, UART recorder and private host were stopped, and all three J8G locks were checked free with `flock -n`.

The full Powerboat evidence, including full-size frames, is in `/data/de25-run3`.
