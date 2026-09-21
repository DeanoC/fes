# FPGA cartridge expansion

FPGA expansion production and linking live in `sources/misteross`. The FES
commit selects that source; the host and target consume its Go linker through
the normal library lifecycle.

The DE10-Nano has no partial reconfiguration. A composed cartridge is one
full-chip RBF: a frozen empty socket plus an independent cart in a reserved
CRAM rectangle. Each shell and cart is built independently; launch does no
synthesis, placement or routing.

## Normal library use

The [ZX81 RAM composition guide](zx81-ram-expansion.md) describes the implemented
optional 16 KiB RAM pack. An ordinary entry selects an immutable expansion
asset against an exact sealed 1 KiB shell. Clearing the selection loads that
same original shell. Host and target independently verify composition bytes;
the runtime programs the admitted payload and retains the base package identity.

This uses the scoped `toolchains/zx81-expansion.lock` and a separate expansion
archive; it does not change format-2 package seals or the factory package set.
Build, software verification and exact-kit acceptance remain separate evidence.

## Earlier development experiments

From the FES repository root, create an isolated FES worktree and enter its
misteross module:

```sh
mkdir -p out/dev/expansion
git worktree add -b fpga-expansion \
  "$PWD/out/dev/expansion/fes" HEAD
cd out/dev/expansion/fes/sources/misteross
```

Follow the misteross README
[Freeze-scaffold cartridges](../sources/misteross/README.md#freeze-scaffold-cartridges)
section there. The compose entry point is
`scripts/build_fes_slot.py`. The linker is `scripts/link_static_rbf.py`.

The compose support originated in [misteross #79](https://github.com/DeanoC/misteross/pull/79);
that historical component revision is not the current source selection.
Use the selected experiment's recipe and lock for these developer probes.
The normal ZX81 library producer uses its own scoped lock, not an ambient
compiler override. Other qualified core compiler selections remain separate.

## Development-RBF diagnostics

Claim the designated kit with the existing misteross `scripts/kit.py`
lease. Load the composed RBF through FogCast's development-RBF path.
Never take over `fogcast@ai-dev-mac` or another owner's session.

## What FES does not do here

- Image assembly does not install composed 900/901/903 artifacts.
- Parent `make build` / `make verify` do not exercise freeze-scaffold
  compose.
- Experimental 900/901/903 loads remain separate from the described ZX81
  package and expansion-asset library path above.
