# Linux-free FES protocol experiment

This is an isolated feasibility prototype for the
[DE25-Nano study](../../docs/design/de25-embedded-os.md). It is not a production
runtime, DE25 board port or deployable card image. It never opens a hardware
device, programs an FPGA or changes boot media.

One allocation-free C++14 protocol probe runs in three environments:

| Mode | Execution | FPGA endpoint |
| --- | --- | --- |
| `host` | Normal host executable, deterministic virtual clock | In-memory fault model |
| `zephyr` | Real Zephyr kernel, AArch64 QEMU `virt`, hardware-cycle clock | Same model |
| `baremetal` | Freestanding AArch64 image, QEMU `virt`, architectural counter | Same model |

The shared code uses FES's checked-in generated `fes.simple-game` constants.
It tests full identity comparison before release, input/hold messages,
ACK toggling and stable sampling, timeout/rejection handling and refusal to
replay an ambiguous transaction. It does not execute the production runtime
or implement package verification, real MMIO, networking, storage, USB, DMA
or display. A simulated success does not qualify the FPGA protocol on a board.

## Host check

From the FES worktree root, with Python 3.11+ and a C++14 compiler:

```sh
python3 examples/de25-embedded-os/run.py host
```

The runner creates `out/de25-embedded-os/host/`. Choose a fresh `--output`
under this worktree's `out/` to repeat after edits; an existing directory is
refused to preserve earlier results. It records commands, compiler identity,
source hashes, artifact hash and console log in `result.json` and adjacent
logs. `--cxx` selects the host compiler.

## AArch64 prerequisites

The tested inputs are recorded in [toolchain.lock.toml](toolchain.lock.toml).
Use Zephyr **v4.2.0**, SDK **0.17.4** with the **aarch64-zephyr-elf** toolchain,
CMake 3.20+, Ninja and an isolated Python environment with `pyelftools`,
`PyYAML`, `pykwalify` and `packaging`. Neither mode needs `west update` or
third-party Zephyr modules. Download the official SDK's minimal archive and
the AArch64 toolchain archive, verify their SHA-256 values from the lock, then
extract the toolchain inside the SDK directory. In the SDK directory run:

```sh
./setup.sh -t aarch64-zephyr-elf -h
```

This installs SDK host tools locally; no global CMake registration is needed.
Example checkout and Python setup (the SDK download links are in the lock):

```sh
mkdir -p out/de25-os-tools
git clone --depth 1 --branch v4.2.0 https://github.com/zephyrproject-rtos/zephyr.git out/de25-os-tools/zephyr
python3 -m venv out/de25-os-tools/venv
out/de25-os-tools/venv/bin/pip install pyelftools PyYAML pykwalify packaging
```

The SDK path below assumes its archive was extracted as
`out/de25-os-tools/zephyr-sdk-0.17.4`.

## Zephyr kernel check

```sh
python3 examples/de25-embedded-os/run.py zephyr \
  --zephyr-base out/de25-os-tools/zephyr \
  --sdk out/de25-os-tools/zephyr-sdk-0.17.4 \
  --python out/de25-os-tools/venv/bin/python
```

This builds `qemu_cortex_a53` and boots the resulting ELF directly in the
SDK's system emulator. The guest is Zephyr, with no guest Linux kernel or
root filesystem. It exercises real kernel initialization, interrupt/timer
support and console output; the FPGA remains simulated. This Cortex-A53
machine is not an Agilex 5 or DE25 hardware model.

## Pure bare-metal check

```sh
python3 examples/de25-embedded-os/run.py baremetal \
  --sdk out/de25-os-tools/zephyr-sdk-0.17.4
```

Only the SDK compiler and QEMU are used here; no Zephyr source or kernel is
linked. The image has a small assembly entry, linker script, byte-copy/fill
routines, polling PL011 console and architectural-counter adapter. It masks
interrupts, parks secondary CPUs, clears BSS and uses a static 16 KiB stack.
It assumes the selected QEMU entry permits counter access and has its MMU
off. It does not initialize DDR, caches, a GIC or board firmware.

`baremetal/linker.ld` places the image at QEMU RAM address `0x40080000`;
the UART is QEMU PL011 at `0x09000000`. **Neither is a DE25 address.** The
compiler uses `-mstrict-align` because an MMU-off image must not generate
unaligned structure accesses. Small local memory routines satisfy compiler
emissions; there is no libc, dynamic loader, allocator or OS syscall layer.

Both AArch64 images park after printing their final result. The runner observes
ten seconds of output, stops its own emulator, and requires the final PASS
marker with no FAIL/fatal output. This timeout is not a clean guest shutdown.
`--qemu` can select another emulator. No board command is provided: a real
Nano requires an independently checked firmware/DDR/entry contract first.

## Limits for reuse

The `Port` callbacks have one owner, must return, and supply monotonic
microseconds. This is a synchronous diagnostic, not a scheduler. Identity
expectations are fixtures, not authenticated package receipts. A fresh link
requires a reset responder; it must not infer authority over an already
running core. The implementation's review and tests belong to this isolated
experiment and do not change production support or any shared definition.
