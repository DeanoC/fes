# DE25 `fes.pc386` `sealed` Smoke Validation (`2026-11-28`)

## Scope and exact artifact

This receipt advances `fes.pc386` under `de25_tier1` in [support-matrix.json](../../support-matrix.json) from `builds` to `sealed` using an exact-`main` Quartus Pro 26.3 package tested on Kit D25 (`de25-j8g`, serial `TRWU8J8G`).

- **Source revision:** `e6f1d16e6278e81bfa3a028c7f983ea9def934c6` (includes `#2368`, `#2366` / `#2353`, and `#2264`)
- **Core / Model:** `fes.pc386` / `486sx` (`de25-pc386-package-candidate`, `package_timing_admission: true`)
- **Package ID:** `96f14b2697e81979167a034b29296d57d1b5fb3a987bfbe9cdd8ed8d30df27a8`
- **`.fcore` SHA-256 (`3,850,752` bytes):** `213e914b156a270aa641b06ab48a58fcfa4f42338ebb95d0ffcab310956fbd16`
- **`core.rbf` SHA-256 (`3,846,144` bytes):** `32025679d606b3227ea62828d424cd4e5a4058919b1ce857d6a1d79cf91c89d0`
- **Build ID:** `e34a05c2a73887f06db29dba4b354199` (`design`: `e34a05c2a73887f06db29dba4b3541995c7abb89d802794ed99eff3bf16de846`, `record`: `8cef53ff1685df882be5f7910899107ece800f8e741d0a813ffe98968046ce53`, `toolchain`: `5bcaf2444bf8ef82ff44be2a632fc449eaa38a488bffa4821e11eabc806253b2`)
- **Firmware digests:** BIOS `0f831b5a0900940d80815e6815ac5afb6b582c6f117f4d4dcd30577fe06b73a4`, VGABIOS `8522568e32a33b2f5649109260fec70b0b2cd0d80a6216ac7af6a1041c102e02`, `rom_hex` `abe6f29c14ad3cc354f13408004a5c1868e706fd04a375a1c32c3beaea5ac28a`

## Kit D25 (`de25-j8g` / `TRWU8J8G`) run summary

Because the kit's promoted baseline software bundle (`cbf468031a31e6af75f385ced40b03f7d255bdf580c44353fb5518bf9736a1f6`) predated `fes.pc386` standard-profile registration in `mister-runtime`, the run temporarily activated an exact-`main` `e6f1d16e6` bundle (`b00daf6b89b374367fa95dc74efdb2339fea0489827c29a7ac493654e798958f`, `mister-runtime` `ab6227eb5e930a1019ed260b4d0ec3ef8b2ca9e5f3f2456497a39d3d06ca0863`) alongside FreeDOS+DOOM ATA image `b4be6e29297168abb31ca536829cbd8a42e9fb05093d16793932ea51d72814b3`, then restored and verified the baseline bundle afterward.

1. **Lease 1, Load 1 (FreeDOS + HID keyboard + DOOM E1M1 + Audio):**
   - Core loaded in `11.92s`; FreeDOS boot captured in [p1-t15.png](frames/p1-t15.png); HID `ver` changed `12.77%` in [p1-t-ver.png](frames/p1-t-ver.png).
   - Launched DOOM E1M1 ([p1-d1-doom.png](frames/p1-d1-doom.png), `dark_inner_rows=0`, `"PICKED UP A STIMPACK."` crisp on HUD); 8.0s HDMI audio measured `rms=1666.98`, `peak=13130`.
   - `stop` while DOOM was active saved CMOS `0Fh = 09`.
2. **Lease 1, Load 2 (In-lease relaunch after DOOM stop — `#2353` / `#2366`):**
   - Core reloaded in `12.26s`, restored cleanly into FreeDOS ([p2-t15.png](frames/p2-t15.png)) instead of hanging on `09h` warm-reset POST, and accepted HID `ver` (`12.77%` diff, [p2-t-ver.png](frames/p2-t-ver.png)); `stop` cleared CMOS `0Fh = 00`.
3. **Lease 1, Load 3 -> DOOM Stop:**
   - Reloaded in `8.59s`, launched DOOM ([p3-d1-doom.png](frames/p3-d1-doom.png), `dark_inner_rows=0`), and stopped again with CMOS `0Fh = 09`.
4. **Lease 2, Load 1 (New-lease relaunch after DOOM stop):**
   - New lease loaded in `11.41s`, booted cleanly into FreeDOS ([q1-t15.png](frames/q1-t15.png)), ran HID `ver` (`12.76%` diff, [q1-t-ver.png](frames/q1-t-ver.png)), relaunched DOOM gameplay ([q1-d1-doom.png](frames/q1-d1-doom.png), [q1-d2-doom.png](frames/q1-d2-doom.png), zero scanline dropouts), and stopped cleanly to `idle`.
5. **SMMU & Restore Verification:**
   - `arm-smmu` and `C_BAD_STREAMID` fault deltas were both `0`.
   - Reactivated baseline bundle `cbf468031a31e6af75f385ced40b03f7d255bdf580c44353fb5518bf9736a1f6`; `de25_restore.py` passed `21/21` checks (`passed: true`); MENU HDMI verified in [hdmi-after-restore.png](frames/hdmi-after-restore.png) (`mean=231.39, lit=0.9939`) matching [hdmi-before.png](frames/hdmi-before.png).
   - Note on ledger entry: `de25-TRWU8J8G-STATUS.md` recorded `smoke=FAIL` only because the runner's post-test inline metric snippet failed importing `PIL` on Powerboat after all loads, captures, audio, CMOS, SMMU, and restore steps finished cleanly; all captured PNG/WAV/SMMU/restore artifacts were verified and stored in [smoke-receipt.json](smoke-receipt.json).
