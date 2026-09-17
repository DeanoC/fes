# fes.sms Quartus oracle bring-up (2026-09-17)

Status: **GREEN** for Quartus 17.0.2 compile-only (oracle/reference). Not OSS, not HIP seal, not kit HIL.

## Identity
- Core: `fes.sms` (FogCast `SystemSMS` / library SMS)
- Tip base: misteross `bbbcef4` (#64 merged)
- Mac wt: `/Users/clawzai/Developer/misteross-wt-sms-quartus` (branch `feat/fes-sms-quartus`, dirty)
- Powerboat wt: `/home/deano/fes-worktrees/misteross-sms-quartus`

## Evidence
- Device: `5CSEBA6U23I7`
- Toolchain: Quartus Prime Lite 17.0.2
- `build/fes-sms-quartus/core.rbf` sha256 `1868bbaacafb125a17efa573ebdd6f625acb501b3c403c15d7531b6cd863e171` (2396332 bytes)
- Fitter: Successful; ALMs 1480/41910 (4%); RAM blocks 110/553 (20%); PLLs 2/6
- Timing (summary): Setup slack 3.302 ns TNS 0; Hold 0.161 ns TNS 0 (from build-summary.json)
- `build-summary.json` status=`pass`, sealed=`false` (compile-only / uncommitted)

## Scope note
SuperGrok PID 41242 exited `exit=0` at 06:43Z after fitter start without overwriting RESULT; Caster closed RESULT from Powerboat artifacts on Bob nudge (~07:24Z). Validation doc written post-hoc.

## Next (not this cut)
OSS/formic-Mistral gap ladder is `docs/validation/2026-09-17-sms-oss-gap-ladder.md` (GREEN). Next kick is the OSS producer (`scripts/build_fes_sms_oss.py` as a Coleco/SG-1000 copy) then HIP synth-only route. No linux.img / closed-set / FogCast allowlist / kit HIL until Bob re-GOs admit.
