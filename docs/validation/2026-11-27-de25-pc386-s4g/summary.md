# DE25 pc386 Slice 4g: Kit D25 486SX FreeDOS, DOOM, Audio, and Save/Relaunch Run

Slice 4g of the DE25 pc386 HPS-DDR window lane (`#2029`, plan in [../2026-11-22-de25-pc386-s4-plan/plan.md](../2026-11-22-de25-pc386-s4-plan/plan.md)) exercises the real `fes.memory.hps-ddr` window ioctls on Kit D25 (`de25-j8g`, serial `TRWU8J8G`) using an exact-`main` 486SX package and exact-`main` DE25 kit-software bundle.

All three `fes.pc386` launches and clean Stops completed on the reserved 256 MiB `fes-window-pool` with **zero `arm-smmu` faults**, booted FreeDOS from the 128 MiB `pmode` ATA disk, accepted interactive HID keyboard input, launched DOOM into live E1M1 gameplay with non-silent HDMI audio, persisted file changes (`C:\S4G.TXT` containing `S4G_DE25_OK`) across Stop and relaunch, and restored cleanly to the J8G baseline.

## Exact artifacts verified

- **Source commit (`main`):** `0902997a891e58c446e76d93c471299eda34d635`
- **486SX package (`fes.pc386`):**
  - Package ID: `ce472a1a39dca2b24ab1fc7072992ee42bda5a08a09a0e01a6721a5637c13534`
  - Package SHA-256: `4f2b0ce569f3e5cc0d0077b32bcd45b6b5850a73ff3e698fda35990dca9b55b7`
  - Build ID (`identities.design[:32]`): `433303e77a04ebd10d2128208358f480`
  - Record digest (`identities.record`): `6425ab16cb8d90b29d46aea8346e5b38b5e005652d6c414486320449cf419d76`
  - `rbf_sha256`: `ab95329ec80d674db0b30349b4329e8d36cbc1a082e7bfdadc1ab923ad84d72f`
  - Toolchain & signoff: Quartus Pro `26.3.0 Build 114` (seed `1`), `machine_clock_hz = 92156863`, `f2sdram_data_bits = 128`, `f2sdram_master_profile = "scanout+window"`, `guest_memory = "hps-window"`, `window_contract = "fes.fabric.de25-hps-window 1.0"`, `package_timing_admission = true`, `all_reported_timing_pass = true`, `unreviewed_failed_rules = 0`, `166` timing checks (minimum slack `0.001 ns`, maximum `9.729 ns`), `3095` window/bridge async reset paths reviewed.
- **DE25 kit-software bundle (`de25-s4g-0902997a8`):**
  - Bundle ID: `4abfc6d26e317df79c9fc4ed6b9483230a0bab1fb1a666988736a2aa6082ef3e` (`bundle.tar` SHA-256 `108775554c9d9c5ba5ce96491e6ba3159b3bd1da4d0388d050c0a2a07a1881d2`)
  - Provenance: `fogcast = 0902997a891e58c446e76d93c471299eda34d635`, `runtime = 0902997a891e58c446e76d93c471299eda34d635`
  - Running `mister-runtime` SHA-256 on J8G (verified against bundle manifest): `a02d4de8c2593d1c2eb9dff8fd19a83a3c3abe710f998e5676a2b1868ca89e35`
- **Kit D25 (`de25-j8g`, serial `TRWU8J8G`, MAC `02:8e:2a:b1:d3:bd`):**
  - Kernel: `6.12.11-fes-de25-owner`
  - Authorized image SHA-256: `a1c51ac341ea39a429d97300a71d57a10dfce8e980d221b3c786db583d65467d` (`boot_id` `2c4855a1-cbe1-4235-a03b-8ed7e9a400b4` unchanged throughout)
  - Reserved pool & IOMMU: `0x0000000085800000..0x00000000957fffff` (`262144 KiB` non-reusable `fes-window-pool`), `fes-de25-dma` (`20010200.dma-owner`) bound to `iommu group 0`
- **ATA disk image (`pmode`, pre-cached on J8G):**
  - `b4be6e29297168abb31ca536829cbd8a42e9fb05093d16793932ea51d72814b3.img` (`134,184,960` bytes, geometry `260/16/63`)

## Measured launch, window, and Stop timings

Measured from `hil_games.py` `/v1/library/core/load` completion (`acks.jsonl`) and target `/run/mister-runtime.events.json` monotonic timestamps on library game `s4g-doom-g`:

| Launch | Total `/v1/library/core/load` | FPGA program (`fence.handoff` → `fence.program`) | Window alloc+zero + 128 MiB ATA write + CRC (`fence.program` → `fifo.consume`) | Stop duration (`cap.fd.open` → `fifo.consume`) |
| --- | --- | --- | --- | --- |
| Pass 1 Launch 1 (`s4g`) | `11.86 s` | `2.41 s` (`43425` → `45835 ms`) | `7.62 s` (`45835` → `53456 ms`) | `2.55 s` (`127938` → `130491 ms`) |
| Pass 1 Launch 2 (`s4g`) | `8.54 s` | `2.43 s` (`135723` → `138154 ms`) | `4.32 s` (`138154` → `142470 ms`) | `2.73 s` (`175814` → `178547 ms`) |
| Pass 2 Relaunch (`s4g-pass2`) | `7.90 s` | `2.28 s` (`268238` → `270514 ms`) | `4.29 s` (`270514` → `274805 ms`) | `2.55 s` (`385314` → `387864 ms`) |

This answers the open launch-latency question in [../2026-11-22-de25-pc386-s4-plan/plan.md#open](../2026-11-22-de25-pc386-s4-plan/plan.md): allocating and zeroing the 256 MiB contiguous pool window plus copying 128 MiB of ATA disk image over the coherent uncached mapping completes in `7.62 s` on first launch and `4.29–4.32 s` on relaunch, without requiring a cache-maintenance ioctl.

## Functional, HDMI, audio, and persistence results

1. **FreeDOS boot and HID keyboard (`s4g` Launch 1):**
   - `wait_stable` settled at `27.5 s` (`20-freedos-prompt.png` showed the booted FreeDOS environment).
   - Remote-input keyboard stream attached (`fes.keyboard.hid 1.0`) and typed `ver`, `echo S4G_DE25_OK > C:\S4G.TXT`, and `type C:\S4G.TXT` (`21-freedos-write.png`).
2. **DOOM startup, gameplay, and HDMI audio (`s4g` Launch 1 and `s4g-pass2`):**
   - `s4g` Launch 1 showed DOOM DOS/4GW initialization (`30-doom-start.png`, `40-doom-play-a.png`) and the DOOM title menu (`41-doom-play-b.png`). MS2109 ALSA capture recorded `19.0 s` at `48 kHz` stereo (`doom-audio.wav`, `peak = 4827`, max 100 ms RMS `-27.2 dBFS`).
   - `s4g-pass2` waited `22 s` for DOOM initialization (`p7-doom-menu.png`), entered E1M1 (`p8-e1m1-a.png`), moved and interacted via HID (`down`/`up` `up`, `lctrl`, `left`, `lctrl` → `p9-e1m1-b.png` showing player movement and `PICKED UP A STIMPACK.`), and recorded `16.5 s` of active E1M1 OPL2 + SFX audio (`doom-e1m1.wav`, `peak = 13308`, max 100 ms RMS `-19.2 dBFS`).
   - Leaving F2SDRAM write credits at `1` did not starve FreeDOS, DOOM CPU execution, or DOOM audio.
3. **Dirty ATA save and relaunch persistence across Stop (`s4g` Stop 1 → `s4g-pass2`):**
   - Stop 1 returned to idle MENU (`50-after-stop-1.png`) and wrote four 65,536-byte dirty ATA chunks plus `record.bin` (`318` bytes) under `/media/fat/fogcast/core-data/media/5ef63c7aa4a35692a74bc35935e7990dde2e7ff59dddacf876b09e21cbd1bf61/`.
   - On `s4g-pass2` relaunch (`s4g-doom-g`), typing `type C:\S4G.TXT` returned `S4G_DE25_OK` on the FreeDOS console (`p6-readback.png`), confirming durable round-trip persistence through the DE25 HPS DDR window.
4. **Zero SMMU faults:**
   - `dmesg | grep -i arm-smmu` after both Pass 1 and Pass 2 contained only normal probe lines (`ias 48-bit`, `allocated 65536 entries`, `allocated 32768 entries`, `msi_domain absent`); zero translation or permission faults occurred across all three launches.

## Observed caveats and open items

- **Pass 1 Launch 2 black-frame timing:** During Pass 1 (`s4g`) Launch 2, `wait_stable` compared two black frames at `27.5 s` (`changed = 0.0`), leaving both `60-relaunch-prompt.png` and `61-relaunch-readback.png` at `6154` bytes before Stop 2. In Pass 2 (`s4g-pass2`), timed grabs at `15 s`, `25 s`, `35 s`, `45 s`, and `60 s` captured live FreeDOS frames continuously through `p6-readback.png` (`S4G_DE25_OK`).
- **Periodic horizontal scanline dropouts under VGA text / active E1M1 frames:** Captured 720p frames of 720×400 VGA text mode (`20-freedos-prompt.png`, `21-freedos-write.png`, `p2-25s.png`–`p6-readback.png`, `p7-doom-menu.png`) and active E1M1 3D gameplay (`p8-e1m1-a.png`, `p9-e1m1-b.png`) exhibited periodic thin horizontal black scanlines, while the static DOOM title screen (`41-doom-play-b.png`) and MENU frames rendered solidly without missing lines. Root cause between VGA/scaler/F2SDRAM contention and MS2109 capture remains open for follow-up investigation.
- **Outside this slice:** 486DX DE25 builds and HIL, broader DOS/Windows/Linux compatibility corpus on DE25, lost-daemon/power-cycle recovery, and registering a DE25 `fes.pc386` recipe in `config/core-recipes.toml` (pending disk-bearing DE25 HILQ smoke support).

## Baseline cleanup and restore

- Removed the temporary `s4g-doom-g` saved core-data directories (`f4491196...` and `5ef63c7a...`) while J8G was idle and lease-free.
- Reactivated baseline kit-software bundle `cbf468031a31e6af75f385ced40b03f7d255bdf580c44353fb5518bf9736a1f6` (`generation` `557` → `558` → `559`).
- Verified `/home/deano/fes-hil/baselines/current-restore-de25.json` with `scripts/de25_restore.py` (`passed = true`, `native_state_unchanged = true`, empty `native_readonly` diff), confirmed FES Home MENU visible on `/dev/v4l/by-id/fes-ms2109-hil`, and logged `KIT D25 FREE (baseline verified)` in `~/kit-status/de25-TRWU8J8G-STATUS.md`.
- Powerboat evidence directory: `/data/de25-s4g-run`.
