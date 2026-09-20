# FPGA cartridge expansion

Freeze-scaffold compose lives in misteross. FES only selects a misteross
revision; it does not overlay CRAM or merge cart JSON.

The DE10-Nano has no partial reconfiguration. A composed cartridge is one
full-chip development RBF: a frozen empty socket plus an independent cart
in a reserved CRAM rectangle. This path is a kit diagnostic. It is not a
factory package, not FogCast format-3, and it does not seal `fes.zx81`.

## Work in misteross

Keep `sources/misteross` on the selected pin. Create a worktree:

```sh
mkdir -p out/dev/expansion
git -C sources/misteross worktree add \
  "$PWD/out/dev/expansion/misteross" HEAD
cd out/dev/expansion/misteross
```

Follow the misteross README
[Freeze-scaffold cartridges](../sources/misteross/README.md#freeze-scaffold-cartridges)
section there. The compose entry point is
`scripts/build_fes_slot.py`. The linker is `scripts/link_static_rbf.py`.

The selected misteross pin is `744906e` ([misteross #79](https://github.com/DeanoC/misteross/pull/79)).
Repository `toolchain.lock` is nextpnr `d672fade`, so
`make toolchain-fes` then `scripts/build_fes_slot.py` compose without a
sidecar. Coleco, SG-1000 and SMS keep their core-local `0fad53a7` HIP slot.
`NEXTPNR_MISTRAL` still overrides the binary.

## Kit

Claim the designated kit with the existing misteross `scripts/kit.py`
lease. Load the composed RBF through FogCast's development-RBF path.
Never take over `fogcast@ai-dev-mac` or another owner's session.

## What FES does not do here

- Image assembly does not install composed 900/901/903 artifacts.
- Parent `make build` / `make verify` do not exercise freeze-scaffold
  compose.
- Host library format-3 overlay remains later work.
