# OSS place-and-route testing

Use this lane to prove that a small Cyclone V design synthesizes, places and
routes with the pinned Yosys, nextpnr-mistral and Mistral tools, and optionally
to compare that result with Quartus Prime Lite 17.0.2.

This is not how you change Coleco, SMS, ZX81, Pong or any other described
package. That job is [Cores](cores.md). The stitch rules and compiler identity
live in [the architecture](architecture.md). What each existing experiment
proved is the [experiment catalog](oss-experiments.md).

## What an experiment is

An experiment is one directory `experiments/NNN_name/` plus one entry in the
closed table in `scripts/experiment_policy.py`. The name must match
`NNN_name` (`[0-9][0-9][0-9]_[a-z0-9_]+`). `policy_for` rejects every other
name.

The three lanes share production RTL and do not share models:

```text
experiment RTL + constraints
  |-- sim ----> Verilator
  |-- oss ----> Yosys -> nextpnr-mistral/Mistral -> top.rbf
  `-- oracle -> Quartus Prime Lite 17.0.2 -------> top.rbf

oss manifest + oracle manifest -> compare report
```

`sim` checks logical behavior. Simulation-only models never enter synthesis.
`oss` does not use Quartus. `oracle` is an explicitly configured Quartus
17.0.2 install and is absent for most experiments. `compare` checks target,
sources, resources, timing and successful artifacts. The two RBFs are not
expected to be byte-identical.

Outputs:

```text
build/oss/<experiment>/top.rbf
build/oracle/<experiment>/top.rbf
build/compare/<experiment>/
```

These files are not format-2 packages and are not installed in an FES image.

## Run one

From `sources/misteross/`:

```sh
make toolchain-check
make toolchain
source scripts/env.sh
make sim EXP=010_blinky
make oss EXP=010_blinky
```

`make toolchain` builds the generic tools with the GPU router off.
`make toolchain-fes` is a different HIP build of the same `toolchain.lock`
into the same `build/toolchain` prefix. The last of those two commands wins.
Leave the HIP prefix for Pong and for freeze-scaffold pass 2. After
`make toolchain-fes`, run `make toolchain` again before a generic experiment
if you need the GPU-off tools back.

Coleco, SMS, SG-1000 and ZX81 HIP tools install under other prefixes. They
are not this lane. See [Cores](cores.md).

If Quartus 17.0.2 is installed, and the experiment's `expected.md` says the
oracle lane exists:

```sh
export QUARTUS_ROOTDIR=/path/to/17.0/quartus
make oracle EXP=010_blinky
make compare EXP=010_blinky
```

The explicit Quartus setup is [the oracle method](oracle-method.md). Many
experiments say "no Quartus comparison lane". Do not invent one.

`make sim` and `make oss` reject a set `FES_TOOLCHAIN_CACHE_ROOT`. The shared
compiler cache belongs to FES core producers, not to this lane. `make clean`
is not implemented. Delete `build/oss/<experiment>` or
`build/oracle/<experiment>` only. Do not delete `build/toolchain` unless you
mean to rebuild the compilers.

`make doctor` reports host, toolchain, oracle and hardware readiness.
`make doctor-strict` requires the host and OSS tools. Quartus and a board
stay optional.

## What exists

Families, not a second copy of every result:

| Range | Subject |
| --- | --- |
| `010`–`020` | LED counter; HPS mailbox (`OSS FPGA OK`) |
| `030`–`080` | M10K ROM, MLAB, LUT/DSP multiply, mixed memory |
| `090`–`400` | PLL: integer, fractional, duty, phase, multi-output, clock enable |
| `410`–`470` | DSP packing, MLAB power-up contents |
| `480`–`600`, `680`, `700`–`880` | M10K shapes: width, ports, clear, stall, async read |
| `610`–`670`, `690` | Fractional 74.25 MHz, DDR/SDR and IO buffers |
| `740`, `750`, `760`, `780`, `850` | Dual-PLL memory, 18×19 DSP, 52 MHz, Quartus SDC forms, HPS location |
| `890`–`893` | Reserved M10K column and ZX81 BASIC proof sites |
| `900`–`907` | Freeze-scaffold shells and carts |

The mailbox walk-through is [Linux mailbox development](linux-mailbox-development.md).
Read `experiments/<name>/expected.md` before building. The catalog records
the cross-experiment notes, including which designs have no Quartus lane and
which measurements are fabric GPI only.

A passing `make oss` is not kit acceptance. Where `expected.md` names a
`hardware/probe.sh`, that probe is a development-RBF diagnostic under the
existing kit lease. It does not seal a package.

## Add an experiment

1. Create `experiments/NNN_name/` with production RTL, a Verilator testbench,
   `expected.md`, and an `oracle/` project only when a Quartus lane is real.
2. Add one frozen `ExperimentPolicy` to `_POLICIES` in
   `scripts/experiment_policy.py`. Source lists, hard-block limits and
   simulation jobs come from that policy. An unknown name does not build.
3. Keep simulation-only models out of the production source list.
4. Say in `expected.md` whether `make oracle` exists. Do not claim it if the
   policy has no Quartus project.
5. Do not import `cores/fes-*` machine RTL, do not emit a format-2 manifest,
   and do not add a row to FES `config/core-recipes.toml`.

## Loading

Claim the designated kit with `scripts/kit.py` and stream the local RBF on
FogCast's development-RBF path. HDMI may stay powered down. Package ABI
negotiation is for described cores, not for these bitstreams. Stop restores
idle through the runtime. `make program` is the separate JTAG maintenance
path and does not write flash or the SD card.

Direct host `POST /api/v1/session/development-rbf` is the same physical path
when that host already holds the lease. Coordinate the lease. Do not take
over another owner.

## Freeze-scaffold cartridges

The DE10-Nano has no partial reconfiguration. A composed cartridge is one
full-chip RBF: a frozen empty socket plus an independent cart whose cells
occupy a reserved rectangle. The linker copies only that rectangle's CRAM
from the pass-2 bitstream onto the pass-1 shell.

This does not seal `fes.zx81`. The product ZX81 package and its library
expansion assets are a [core](cores.md). Image assembly does not install
900/901/903/904–907 artifacts.

### Roles

| Piece | Experiment | What it is |
| --- | --- | --- |
| Shell | `901_plugged_base` | Empty socket. Signature `0xD901`. Reserved rect `25 1 27 16`. |
| Cart A | `900_expansion_bus` | One BEL-locked `MISTRAL_M10K.26.1.0`. Synth-only. No HPS. |
| Cart B | `903_wide_cart` | Four slot M10Ks and a 2-bit decode. Synth-only. |
| Map | `experiments/901_plugged_base/link.toml` | `overlay_mode = "cram_rect"`, tile columns 21–33, `require_slot_only`. |
| ZX81 socket | `904_zx81_socket` | Empty ZX81 plug. Signature `0xD904`. Reserved rect `25 1 27 32`. |
| 16K validation cart | `905_zx81_ram16` | Sinclair `4000–7FFF`, sixteen column-26 M10Ks. Synth-only. |
| Zon X-81 | `906_zx81_zonx` | AY register file. Synth-only. |
| QS CHRS | `907_zx81_qs_chrs` | 1 KiB at `8400–87FF`. Synth-only. |

The 890/891/892 trio is the older INIT-only M10K overlay
(`overlay_mode = "m10k_ram"`). Use 901 when the cart is unknown at shell
place-and-route time. The mechanism, including `link_static_rbf.py init` and
the ZX81 BASIC splice, is in the architecture
[freeze-scaffold section](architecture.md#freeze-scaffold-cartridges).

### Build a composed RBF

Pass 2 uses `--router gpu`. Install HIP nextpnr first. That command replaces
the generic tools in `build/toolchain`:

```sh
make toolchain-fes
make oss EXP=901_plugged_base
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/901_plugged_base/routed.json \
  --shell-rbf build/oss/901_plugged_base/top.rbf \
  --cart 900_expansion_bus \
  --output build/oss/composed_901_plus_900.rbf
```

Replace `--cart 900_expansion_bus` with `903_wide_cart` for cart B. Cart A
and cart B are synth-only: `make oss EXP=900_expansion_bus` and
`make oss EXP=903_wide_cart`. Verilator is separate:
`make sim EXP=900_expansion_bus`.

ZX81 carts use the 904 shell, map and QSF:

```sh
make oss EXP=904_zx81_socket
python3 scripts/build_fes_slot.py \
  --shell-json build/oss/904_zx81_socket/routed.json \
  --shell-rbf build/oss/904_zx81_socket/top.rbf \
  --cart 905_zx81_ram16 \
  --map experiments/904_zx81_socket/link.toml \
  --qsf experiments/904_zx81_socket/pins.qsf \
  --output build/oss/composed_904_plus_905.rbf
```

Replace `--cart 905_zx81_ram16` with `906_zx81_zonx` or `907_zx81_qs_chrs`.

`scripts/build_fes_slot.py` synthesizes the cart, merges it into the routed
shell with `--fes-scaffold --fes-cart`, and runs
`scripts/link_static_rbf.py overlay`. Locked nextpnr `30ac6f47` provides those
flags (it inherits the earlier freeze-scaffold support from `d672fade`).
`NEXTPNR_MISTRAL` still overrides the binary. A nextpnr without the flags
fails closed. The linker writes a `.receipt.json` beside the output.

To overlay two already-built RBFs without resynthesizing:

```sh
python3 scripts/link_static_rbf.py overlay \
  --base build/oss/901_plugged_base/top.rbf \
  --cart path/to/pass2.rbf \
  --map experiments/901_plugged_base/link.toml \
  --output build/oss/composed.rbf
python3 scripts/link_static_rbf.py diff \
  --a build/oss/901_plugged_base/top.rbf \
  --b build/oss/composed.rbf
```

`require_slot_only` refuses any CRAM bit outside the map rectangle.

### Write another cart

1. Independent experiment with `top = "cart"` and `synth_only`.
2. Ports `plug_addr[15:0]` and `plug_rdata[9:0]` matching the shell. ZX81
   carts also take `plug_wdata[7:0]`, `plug_mem_we`, `plug_io_we` and
   `plug_io_rd`.
3. BEL-lock every slot cell inside the shell reserved rect (901:
   `25 1 27 16`; 904: `25 1 27 32`, M10K column 26).
4. `setattr -set FES_SLOT 1 c:*` after synth so nextpnr treats those cells
   as the cart.
5. No HPS, LED, GPIO or signature. The shell keeps `0xD901` or `0xD904`.
6. Primitive `MISTRAL_FF` `BEL` attributes survive Yosys. An inferred `reg`
   `BEL` does not.
7. Compose with `--cart <experiment>` onto the matching shell. Do not rebuild
   the shell for a new cart.

### Kit probes

Claim the kit with `scripts/kit.py session`. Load the RBF through the
development-RBF path. Vacant 901:
`experiments/901_plugged_base/hardware/probe.sh`. Composed cart A:
`probe_cart.sh`. Cart B: `probe_cart_b.sh`. Vacant 904:
`experiments/904_zx81_socket/hardware/probe.sh`. 16K pack: `probe_ram16.sh`.
Zon X-81: `probe_zonx.sh`. QS CHRS: `probe_qs_chrs.sh`.

GPI is `{SIGNATURE, plug_addr[5:0], plug_rdata}`. GPO for 904 is
`{io_rd, io_we, mem_we, wdata[7:0], addr[15:0]}`. 904 probes settle addr/data
with strobes low, then pulse. Do not apply `0x13579BDF` (it is an I/O write
to xxDF). This is a development-RBF diagnostic, not image acceptance, and it
does not seal `fes.zx81`.
