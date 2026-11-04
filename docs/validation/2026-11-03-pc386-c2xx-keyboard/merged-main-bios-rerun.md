# Merged-main BIOS rerun (2026-11-04)

This is the final merged-head rerun for PR #1452. It follows merge commit
`6f8ce300f627060ca53776e032605a2c306a7d80`, which merged `origin/main`
`bb51f4454eb72048cefeea16aa16b28eb2aadaad` into the PR branch. The earlier
[merged-BIOS rerun](merged-bios-rerun.md) remains as evidence for its earlier
tree; these reports use the combined BIOS source after the main merge.

## Pinned inputs

- System BIOS SHA-256: `26b37cca11e390175cc3c9ccb7755f806e5ceb3aaf24aae157b5d01b73e1b4b0`.
  `firmware.lock` records this exact image digest.
- `rombios/source.json` records the combined `rombios.c` SHA-256:
  `6d121d6843ca99669f8b41400495ab97bd1fa8b7ebe6d1bce45b2e30f6765f0c`.
- BIOS toolchain: dev86 `0b72e907f7a282aaa777a768b8b688e41c77fc93`
  (`v1.0.1`), as pinned in `firmware.lock`.
- VGA BIOS SHA-256: `69401dc856ec9e9377840123b9163e4a683733a13920eceaf72658a1b69b343c`
  (unchanged).
- Verilator 5.032, source revision
  `8ff77e9d47351b0a59114929880687839a51840b`; mtools 4.0.43.
- FreeDOS packages and NASM use the revisions in `cores/fes-pc386/sim/freedos.lock`
  and `test_fes_i386_test386.py`.

## Results

All six simulations used the BIOS image above. Each report has an empty
`failures` array.

| Suite | CPU | Limit | Result |
| --- | --- | ---: | --- |
| `c2xx` ([report](merged-main-bios-rerun/c2xx-report.json)) | oracle | 10 s | Pass; stale key, stale aux, masked key and typing checks; 41 resets while typing |
| `c2xx` ([report](merged-main-bios-rerun/c2xx-rtl-report.json)) | RTL | 10 s | Pass; same five checks; 42 resets while typing |
| `wait` ([report](merged-main-bios-rerun/wait-report.json)) | oracle | 8 s | Pass; all eight checks, including 300 ms |
| `wait` ([report](merged-main-bios-rerun/wait-rtl-report.json)) | RTL | 8 s | Pass; all eight checks, including 300 ms |
| `system` ([report](merged-main-bios-rerun/system-report.json)) | oracle | 12 s | Pass; all 17 record checks, including INPUT.COM mouse and keyboard checks |
| `boot` ([report](merged-main-bios-rerun/boot-report.json)) | oracle | 8 s | Pass; FreeDOS prompt at 3.75 simulated seconds; VGA replay matched 81,109 events with zero mismatches |

## Commands

From `sources/misteross`:

```sh
python3 scripts/build_pc386_firmware.py --download --jobs 8 --update-lock
FES_CI_JOBS=8 bash scripts/ci_verilator.sh "$PWD/../../out/tools/ci-verilator" "$PWD/../../out/tools/ci-verilator-build"
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite c2xx --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite c2xx --cpu rtl --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite wait --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite wait --cpu rtl --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite system --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 scripts/sim_fes_pc386.py --suite boot --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
```

The requested unit tests also passed:

```sh
# From sources/misteross
VERILATOR="$PWD/../../out/tools/ci-verilator/bin/verilator" PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" python3 -m unittest tests.test_pc386_machine
# From the repository root
python3 -m unittest tests.test_ci_gate tests.test_ci_workflow
```

`test_pc386_machine`: 51 tests passed in 293.064 seconds.
`test_ci_gate` and `test_ci_workflow`: 28 tests passed in 0.192 seconds.

This record contains host-side simulation evidence only. No kit test or new
Quartus seal was run. The earlier Quartus seal remains attached to its recorded
tree and is not claimed for this merged-main head.
