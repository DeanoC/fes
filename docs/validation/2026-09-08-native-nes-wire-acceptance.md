# Native NES narrow-wire acceptance, 2026-09-08

This record closes the grey-screen failure found when the native NES image was
launched through the FES host. The earlier [software integration record](2026-09-08-native-nes-software.md)
is retained as historical contract and provenance evidence; this record covers
the exact assembled image installed on the kit.

## Failure and correction

The NES core instantiates MiSTer's `hps_io` with its default `WIDE=0`. Its file
input consumes one byte from each 16-bit SPI transfer. The runtime had been
sending two cartridge bytes in every word, so the core discarded every high
byte and received a corrupted iNES header. The NES package also used filetype
index zero even though the core maps the first native NES item to `0x40`
(`type=1`, `slot=0`). Either error was enough to prevent a game from starting.

The selected component revisions now use a `little_endian_bytes` wire format
for NES, send each cartridge byte in its own 16-bit transfer with a zero high
byte, and emit the native filetype index `0x40`. SNES and Mega Drive retain
their wide pair transfer format.

## Exact inputs and reproducible image

| Item | Revision or SHA-256 |
| --- | --- |
| FES parent | `2734df5ccb3246f46fa9c5490c7d1637b4c27c02` |
| FogCast | `a8f2a2479a3b191922404c6633d1b6c754a16885` |
| libmister-runtime | `c6dc8611438e80783b7bacde0e3bb4ca7454f744` |
| mister-packages | `359a0171483e389ddbd68bcede8c51a77eec427c` |
| misteross | `b7e8e5ee30320050cae34d379e8a4f82160a56ae` |
| NES source | `9a63821173b6da4d6e95dcbe2e2a322ec8171144` |
| NES RBF | `2b8ea0d6e8d7d3b533e813477c011d4850013a768f1478a9281c8531f937c402` |
| cold rootfs, 64 MiB | `0c578202691be8dc8c5a12b4b7dc394c40cdb6867cdd2e7f922472b0ca40fbd8` |

`make build` produced the rootfs twice in independent Buildroot output trees;
both runs produced the rootfs hash above. `make verify` passed the native
structural checks and the vexpress-a9 QEMU packaging smoke. The verifier
accepted the four selected cores and their external selection records.

The cold manifest records these installed payloads:

| File | SHA-256 |
| --- | --- |
| `usr/sbin/mister-runtime` | `45d0a110427bcc4ac8d92da0d042b3eceaabb3b1b05b47cda1e33a3f63dfc484` |
| `usr/sbin/mister-agent` | `768fbd37b0bfa6a11fe2dec311132124a0ee4d26ce94d3378f9af34dae32a96d` |
| `usr/share/mister-runtime/cores/nes.rbf` | `2b8ea0d6e8d7d3b533e813477c011d4850013a768f1478a9281c8531f937c402` |
| `usr/share/mister-runtime/cores/snes.rbf` | `fdd6d3c51cf3662cb59c5250eee8d4aa48fdab14a272c756fb892677d5ff1226` |
| `usr/share/mister-runtime/cores/pong.rbf` | `1567e5ea4db1f18b9f23b48e7a4b7604024a998ddf1378bf77fe5968e00c64d1` |
| `usr/share/mister-runtime/cores/megadrive.rbf` | `195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e` |

## Exact-image kit check

The rootfs was staged under the existing kit lease, its remote hash was checked,
and the previous `/media/fat/linux/linux.img` was retained as
`linux.img.fes-nes-wire-baseline`. The target at `192.168.10.84` rebooted from
boot ID `64569669-a72c-4b69-95ab-6a2b438b5d94` to
`03f67c62-d178-42ad-88a9-410db88b3879`. The installed rootfs hash and all
runtime, agent and four RBF hashes matched the cold manifest. The lease was
free after the test.

Through the normal FogCast API:

1. `nes-10-yard-fight-u-1468f00fdd9d` launched as `fpga_native`; the input
   channel reported `attached` and `ready`, and the settled HDMI capture shows
   the full colour 10-Yard Fight title screen. Stop returned the session to
   `idle`.
2. `nes-tetris-u-270186b6002a` launched as `fpga_native`; after its startup
   blanking interval the settled HDMI capture shows the colour Tetris licence
   screen. Stop again returned the session to `idle`.

The retained captures and the installed-hash transcript are in the ignored
evidence directory `out/nes-native-wire-hw/` for local audit. This validates
the exact assembled NES video path, cartridge transfer and session lifecycle;
it does not claim separate audio measurement or a new controller-movement
test. Future image or core changes require a fresh exact-artifact check.
