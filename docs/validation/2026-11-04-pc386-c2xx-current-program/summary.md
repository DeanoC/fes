# pc386 C2xx current-program rerun (2026-11-04)

This rerun uses PR head `6c31a7e0388979bbed3d38399286bae554934cbf`, including the current `C2TEST.COM` ordering from `pc386: type before C2xx stale-byte checks`.

## Inputs

- System BIOS SHA-256: `26b37cca11e390175cc3c9ccb7755f806e5ceb3aaf24aae157b5d01b73e1b4b0`; VGA BIOS: `69401dc856ec9e9377840123b9163e4a683733a13920eceaf72658a1b69b343c`. The build matched `firmware.lock` without updating it.
- `cores/fes-pc386/sim/c2xx_test.asm` SHA-256: `9e61adb46cb1004928bb4463a803196bcbd77616e51e44e4517d4342587defce`.
- Assembled `C2TEST.COM` SHA-256: `6f4fcfbb546c9ebd8653a345f29be555c06ac49ad62bd327c81d632c33851823` (NASM 2.16.03).
- Verilator 5.032, source revision `8ff77e9d47351b0a59114929880687839a51840b`; mtools 4.0.43.

The program now runs the `typing` check first: it invokes C205h repeatedly while the bench types `a` through `q` (each held 8 ms), waits 10 ms for the final make/break to drain, and checks the keys arrived in order. It then runs `stale-key` (K make byte in the 8042 output buffer), `stale-aux` (aux byte), and `masked-key` (IRQ1 masked), and prints `DONE`.

## Results

| Suite | CPU | Result |
| --- | --- | --- |
| `c2xx` | oracle | Pass; 45 typing resets, keys `abcdefghijklmnopq` in order; stale-key, stale-aux, masked-key and `DONE` pass ([report](c2xx-oracle-report.json)) |
| `c2xx` | RTL | Pass; 45 typing resets, keys `abcdefghijklmnopq` in order; stale-key, stale-aux, masked-key and `DONE` pass; 0 oracle steps, 0 protected-mode steps, 3.80 simulated seconds ([report](c2xx-rtl-report.json)) |
| `wait` | oracle | Pass; all eight checks from 0 us through 300 ms ([report](wait-oracle-report.json)) |
| `wait` | RTL | Pass; all eight checks from 0 us through 300 ms; 0 oracle steps, 0 protected-mode steps, 4.30 simulated seconds ([report](wait-rtl-report.json)) |
| `system` | RTL | Pass; all 17 record checks; C2xx mouse ready, all ten mouse packets and keyboard codes match; Stop/save/restore and port-2 hold checks pass; 3.217 simulated seconds ([report](system-report.json)) |
| `boot` | oracle | Pass; FreeDOS prompt at 3.75 simulated seconds; VGA replay matched 81,109 events with zero mismatches ([report](boot-report.json)) |

These are host simulation results. No Quartus seal or kit test was run; pc386 has no kit HIL requirement.

## Commands

From `sources/misteross`:

```sh
python3 scripts/build_pc386_firmware.py --download --jobs 8
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" VERILATOR_ROOT="$PWD/../../out/tools/ci-verilator/share/verilator" python3 scripts/sim_fes_pc386.py --suite c2xx --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" VERILATOR_ROOT="$PWD/../../out/tools/ci-verilator/share/verilator" python3 scripts/sim_fes_pc386.py --suite c2xx --cpu rtl --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" VERILATOR_ROOT="$PWD/../../out/tools/ci-verilator/share/verilator" python3 scripts/sim_fes_pc386.py --suite wait --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" VERILATOR_ROOT="$PWD/../../out/tools/ci-verilator/share/verilator" python3 scripts/sim_fes_pc386.py --suite wait --cpu rtl --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" VERILATOR_ROOT="$PWD/../../out/tools/ci-verilator/share/verilator" python3 scripts/sim_fes_pc386.py --suite system --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
PATH="$PWD/../../out/tools/mtools/usr/bin:$PATH" VERILATOR_ROOT="$PWD/../../out/tools/ci-verilator/share/verilator" python3 scripts/sim_fes_pc386.py --suite boot --download --verilator "$PWD/../../out/tools/ci-verilator/bin/verilator" --jobs 12
```
