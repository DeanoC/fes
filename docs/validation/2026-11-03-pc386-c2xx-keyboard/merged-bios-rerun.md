# Merged-BIOS rerun (2026-11-04)

This rerun supplies the post-merge evidence requested for PR #1452. All six
simulation runs used the BIOS rebuilt once from the PR head's `firmware.lock`:

- BIOS SHA-256: `2b07c1aad92feae47745711e8bee06d1f74e2ec11abfc3712a9f5d5d91af9bba`
- `firmware.lock` BIOS SHA-256: the same value
- VGA BIOS SHA-256: `69401dc856ec9e9377840123b9163e4a683733a13920eceaf72658a1b69b343c`
- Verilator: 5.032, built from the CI pin `8ff77e9d47351b0a59114929880687839a51840b`
- Source: PR head `2463a17c1341b27929f9c883abd37ae671798281` with the wait-limit
  correction now recorded in `23f26b62c40409897a8eb78daf8e2069d0ceeea6`. The
  source files were unchanged between the runs and that commit.

| Suite | Result |
| --- | --- |
| `c2xx` (oracle CPU) | Pass. Stale key, stale aux, masked key and typing all pass; 41 resets, with `abcdefghijklmnopq` received in order. ([report](merged-bios-rerun/c2xx-report.json)) |
| `c2xx --cpu rtl` | Pass. Same five checks; 42 resets. 99,816,063 instructions retired, zero oracle steps, no stops. ([report](merged-bios-rerun/c2xx-rtl-report.json)) |
| `wait` (oracle CPU) | Pass under the restored 8-second default. All eight checks pass, including the 300 ms wait. ([report](merged-bios-rerun/wait-report.json)) |
| `wait --cpu rtl` | Pass under the restored 8-second default. All eight checks pass, including the 300 ms wait. 108,064,440 instructions retired, zero oracle steps, no stops. ([report](merged-bios-rerun/wait-rtl-report.json)) |
| `system` | Pass. All 17 record checks pass. `INPUT.COM` reports mouse ready; mouse sums, keyboard codes, disk save, dirty map, restored record/CMOS, clock and Stop checks pass. ([report](merged-bios-rerun/system-report.json)) |
| `boot` (oracle CPU) | Pass. FreeDOS reaches the prompt at 3.75 simulated seconds. VGA replay matches 81,109 events with zero read, VRAM or frame mismatches. ([report](merged-bios-rerun/boot-report.json)) |

The full pc386 unit suite passed: 47 tests in 287.724 seconds with Verilator
5.032. The focused c2xx/wait unit tests also passed: 10 tests.

## Commands

Commands ran from `sources/misteross`. The local Verilator and mtools paths
below were under ignored `out/` directories. The host lacked mtools, so
mtools 4.0.43 was extracted there without installing it system-wide.

```sh
python3 scripts/build_pc386_firmware.py --download
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite c2xx --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite c2xx --cpu rtl --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite wait --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite wait --cpu rtl --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite system --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite boot --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
python3 -m unittest tests.test_pc386_machine.C2xxTestTest tests.test_pc386_machine.WaitTestTest
VERILATOR="$PWD/../../out/tools/ci-verilator/bin/verilator" python3 -m unittest tests.test_pc386_machine
```
