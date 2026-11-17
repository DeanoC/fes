# FPGA cartridge expansion

FPGA expansion production and linking live in `sources/misteross`. The FES
commit selects that source; the host and target consume its Go linker through
the normal library lifecycle.

The DE10-Nano has no partial reconfiguration. A composed cartridge is one
full-chip RBF: a frozen empty socket plus an independent cart in a reserved
CRAM rectangle. Each shell and cart is built independently; launch does no
synthesis, placement or routing.

## ZX81 bus carts (advisory)

The registered ZX81-family packages are built by the Quartus Lite producer
(`cores/fes-zx81/quartus.lock`) and have no expansion socket, so no library
entry can select a ZX81 bus cart for them. DeanoC/fes#1875 builds the 16K RAM
and Zon X carts into that shell, selected at launch.

The [ZX81 expansion-bus guide](zx81-expansion-bus.md) describes the Z80-like
edge as the advisory nextpnr shell (`build_fes_zx81_oss.py`,
`toolchain.lock`) still builds it, with its validation cart. On that shell an
entry may select an immutable bus asset against the exact sealed shell, and
clearing the selection loads the original shell. Host and target
independently verify composition bytes; the runtime programs the admitted
payload and retains the base package identity. The cart is a separate
expansion archive bound to that exact shell. This is a diagnostic path:
build, software verification and exact-kit acceptance remain separate
evidence, and the factory package set never contains it.

## Earlier development experiments

From the FES repository root, create one FES worktree and enter its
misteross module. Do not create nested component worktrees:

```sh
mkdir -p out/dev
git worktree add -b fpga-expansion \
  "$PWD/out/dev/fpga-expansion" HEAD
cd out/dev/fpga-expansion/sources/misteross
```

Follow [OSS place-and-route testing](../sources/misteross/docs/oss-pnr.md#freeze-scaffold-cartridges)
there. The module README has the short entry point. The compose entry point is
`scripts/build_fes_slot.py`. The linker is `scripts/link_static_rbf.py`.

The compose support originated in [misteross #79](https://github.com/DeanoC/misteross/pull/79);
that historical component revision is not the current source selection.
Use the selected experiment's recipe and lock for these developer probes.
The advisory nextpnr ZX81 shell selects the shared `toolchain.lock`, not an
ambient compiler override. Other qualified core compiler selections remain
separate.

## Development-RBF diagnostics

Claim the designated kit with the existing misteross `scripts/kit.py`
lease. Load the composed RBF through FogCast's development-RBF path.
Never take over `fogcast@ai-dev-mac` or another owner's session.

## What FES does not do here

- Image assembly does not install composed 900/901/903/904–907 artifacts.
- Parent `make build` / `make verify` do not exercise freeze-scaffold
  compose.
- Experimental 900/901/903/904–907 loads remain separate from the described ZX81
  package and expansion-asset library path above.

ZX81 diagnostic carts (`904_zx81_socket` plus `905_zx81_ram16`,
`906_zx81_zonx`, `907_zx81_qs_chrs`) remain an HPS-driven freeze-scaffold
bench. They are not the sealed `fes.zx81` package. Library 16K / Zon X / QS
RTL under `cores/fes-zx81/expansions/` targets the ZX81 CPU edge instead.
