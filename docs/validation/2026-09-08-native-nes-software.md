# Native NES software integration, 2026-09-08

This record covers the native NES vertical slice in the isolated FES integration
worktree. It establishes a software-supported, hardware-pending state. No exact
assembled image was installed on the kit and no NES hardware acceptance is
claimed.

## Selected revisions

The parent starts from FES `a501f6420d8484b6f9bf83bb0055dac2a6a6be10` and selects
these component commits:

| Component | Selected commit |
| --- | --- |
| FogCast | `47d55bf6c2fe5b095d67898de13ab51d950fca7f` |
| libmister-runtime | `c19d13909f062e5ec05139e9a6d3571accb99fd6` |
| mister-packages | `c7091785510f6b383c646eff3f86d0b3d4bf0620` |
| misteross | `b7e8e5ee30320050cae34d379e8a4f82160a56ae` |

FogCast's runtime lock points to libmister-runtime `c19d139`. The integration
profile selects the ordered set `megadrive pong snes nes`.

## NES source and contract

The package and core lock agree on the official MiSTer NES source:

```text
repository: https://github.com/MiSTer-devel/NES_MiSTer
commit:     9a63821173b6da4d6e95dcbe2e2a322ec8171144
project:    NES.qpf
artifact:   releases/NES_20260823.rbf
sha256:     a4c023defa4f7856585e5dba429a3b61aee3e01eb3de2c731bb0036c12f11701
size:       3282472
```

The checked-in native contract admits `.nes` files only, with a 32 MiB maximum,
one cartridge at native index 0, and raw little-endian byte-pair transfer.
Runtime preflight accepts iNES 1.0 and NES2 headers and rejects missing magic,
trainers, zero PRG, impossible sizes and truncated payloads before FPGA
programming. FDS, UNIF/UNF, NSF, saves, cheats and accessory peripherals remain
outside this slice.

The upstream checkout was fetched at the locked commit. Its release artifact was
verified locally as SHA-256
`a4c023defa4f7856585e5dba429a3b61aee3e01eb3de2c731bb0036c12f11701` and 3,282,472
bytes. Quartus Prime Lite `17.0.2 Build 602 07/19/2017 SJ Lite Edition` was then
run against that checkout. The source rebuild completed with zero errors and
produced a sealed bundle containing a 3,304,192-byte RBF with SHA-256
`2b8ea0d6e8d7d3b533e813477c011d4850013a768f1478a9281c8531f937c402`. The rebuilt
RBF is intentionally distinct from the locked upstream bytes; both remain
available for their respective provenance paths.

TimeQuest reported zero TNS with worst-case setup slack `0.468 ns`, hold slack
`0.245 ns`, recovery slack `3.445 ns`, removal slack `0.910 ns`, and minimum
pulse-width slack `1.122 ns`. The exact upstream RBF was then loaded through
the existing leased development path on the designated kit. The target
returned `state: active`, and its authenticated status reported
`observed_core: NES` after an eight-second stability window. Stop returned the
kit to idle and the lease was released; the public lease status subsequently
returned `free`. This validates the upstream RBF programming/identity path
only; it is not acceptance of the assembled four-system image or NES game
video/input behavior.

## Software checks

The following checks passed:

- `make check`: package YAML, nine generated consumers, and four copied source
  pins agree with the selected gitlinks.
- `make test`: 173 parent tests passed; 36 tests were skipped because they are
  explicitly delegated to a pinned media container.
- `python3 -m unittest tests.test_core_build tests.test_consistency tests.test_native_dev tests.test_bundle tests.test_media -v`: 81 passed.
- In FogCast, `go test ./...` and `go vet ./...` passed.
- FogCast `scripts/tests/target-image_test.sh` and
  `scripts/tests/native-extra-cores_test.sh` passed, including the four-system
  selector, sealed sidecars, five-RBF count, missing-core rejection and NES
  bundle environment routing using synthetic sealed fixtures.
- The selected mister-packages and libmister-runtime worktrees passed their
  focused/full package and runtime suites; runtime `make all` also passed.
- `python3 scripts/core_lock.py validate` and the focused misteross lock/export
  tests passed. The full misteross unittest discovery retains existing errors in
  a fresh worktree because ignored `build/toolchain/install/bin/yosys` is absent;
  no cold Linux/compiler/toolchain rebuild was performed.
- `make fetch-core CORE=nes`, `QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0
  make rebuild-core CORE=nes`, and `make export-core-bundle CORE=nes` passed;
  the exported bundle contains exactly `nes.rbf` and `nes-rbf.toml`.

These checks validate contracts, provenance, Quartus timing and image assembly
policy, and the standalone upstream RBF now has a bounded programming/identity
check. They do not establish assembled-image video, audio, controller input or
exact-image NES acceptance. The next hardware gate is an assembled four-system
image containing the locked upstream NES RBF, followed by a bounded mapper-0
launch/input, Stop, and relaunch test on the designated kit.
