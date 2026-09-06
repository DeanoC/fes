# Dual-PLL native diagnostic, 2026-09-06

This record covers FES `f34c84c` on `main`, which selects misteross `cb89517`.
It is a diagnostic `make dev` image and bounded kit check of that selection. It
does not replace [integration validation](integration-validation.md) or
[multi-system development](multi-system-development.md) evidence for earlier
pins and artifacts.

| Component | Selected revision |
| --- | --- |
| FogCast | `5fc0b4ad8cac63be8fc6e9d0b9e82c8aede8baac` |
| libmister-runtime | `93b369f7bf56757697cc5e59332545f5b4ee62f3` |
| misteross | `cb895176d229f095d23dbc005566dda6ace78a6e` |
| mister-packages | `b5a92e511a111c428f1f39057e3c6386e9d19c81` |

misteross `cb89517` pins nextpnr `56b64126` for compatible dual PLL outputs and
adds `170_pll_dual`. Mega Drive, Pong and SNES recipe files were unchanged from
the previous three-system bundles.

## Software checks

- At pin time, `make check` passed and `make test` passed all 29 tests.
- `make doctor` reported the revisions above.
- `QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0 make dev` published
  `out/native-integration-dev/development/linux.img` after structural checks.
  Mega Drive and SNES source-built bundles were reused. Pong was rebuilt and
  exported at misteross `cb89517`; its RBF bytes remained
  `1567e5ea4db1f18b9f23b48e7a4b7604024a998ddf1378bf77fe5968e00c64d1`.

Published development image SHA-256
`899001ade0f3e63c150947b9fa78945f0c4b2ca632ba500ab9e30b4da6902ea0`.
Installed core hashes match the earlier three-system diagnostic RBFs:

| Core | SHA-256 |
| --- | --- |
| Mega Drive | `195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e` |
| Pong | `1567e5ea4db1f18b9f23b48e7a4b7604024a998ddf1378bf77fe5968e00c64d1` |
| SNES | `fdd6d3c51cf3662cb59c5250eee8d4aa48fdab14a272c756fb892677d5ff1226` |

This is not a two-pass `make build` / `make verify` run. QEMU packaging was not
repeated.

## Bounded hardware

The exact development image was deployed to the designated kit at
`192.168.10.239`. Installed image, agent, runtime and core hashes matched the
published files. The target reported ready, with no Main process and no
`/dev/MiSTer_cmd` FIFO. Pong's installed selection records misteross `cb89517`.

A temporary native host API listened on `127.0.0.1:8788` using the selected
FogCast `fogcast-api`. The pre-existing host on port 8787 was left running.

On boot `675d052a-fd5c-45b8-ae41-1adcb70fc68c`:

1. ROM-less Pong launched as `fpga_native`. HDMI showed paddles, ball and 0–0.
   Stop returned idle.
2. Sonic The Hedgehog 2 (World, Rev A) launched as Mega Drive. HDMI showed
   Emerald Hill Zone 1. Stop returned idle.
3. Super Mario World (U) launched as SNES. HDMI showed the title screen. Stop
   returned idle.
4. An explicit reboot returned ready idle on boot
   `817f9c5f-aa75-40e1-ac75-abe5261f5083` without Main. The kit lease was left
   free.

`scripts/native-runtime-smoke.sh` failed on idle Stop:
`MISTER_UNAVAILABLE`, because idle Stop without a kit lease is rejected by the
selected host. Launch/Stop of the three games and the reboot idle check above
were used instead. Remote input was not enabled in the host configuration, so
Start/movement were not sent.

The temporary 8788 host was stopped afterward. Evidence is under
`out/pll-dual-native-hw/`: image SHA, `installed.txt`, launch/session JSON,
HDMI captures, deploy log and reboot health.

This is exact-artifact diagnostic evidence for the `make dev` image of FES
`f34c84c`. It is not cold two-pass reproducibility, native-game acceptance of a
clean image, audio characterisation, or complete bootable-media assembly.
