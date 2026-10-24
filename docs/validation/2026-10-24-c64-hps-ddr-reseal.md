# 2026-10-24: C64 0.5.0 resealed after the shell took the HPS DDR bridge

#1001 moved the HPS DDR bridge out of the shared display plane and into each
shell: the plane is now a port-0 client of `fes_hps_ddr`. The C64 top gained
that bridge in the same PR, so the bitstream sealed for #1032
(`713b811056616b15…`, build `b7bf0ddbbf358a3b…`) is no longer what `main`
builds. This record seals the C64 from `main` after #1001. It is not a kit
check, an image or a release.

## Artifacts

Source FES `e93b15b2` (`sources/misteross`), producer
`scripts/build_fes_c64_oss.py`, lock `cores/fes-c64/toolchain.lock`
(nextpnr `5fe55f3d`, unchanged), HIP router, first-pass placement search in
the recipe's seed order. The toolchain slot was rebuilt, because the shared
toolchain cache had been cleared.

- `fes.c64` 0.5.0: package `3d5bfc914a91798fd56537ff7c4f0f11a672b870e57ca54e0bae2932fb8e54f7`, build `795666e322409e567db496b9b78c8c48`, RBF sha256 `b7807c1add373f71b407e77e2bb38cdee990284148cc240a37344290571fdd1e` (3,795,160 bytes)

| Clock | Constraint MHz | Achieved MHz |
| --- | --- | --- |
| System | 52.224 | 53.10 |
| Pixel | 74.25 | 78.09 |
| Audio | 12.288 | 148.50 |

The recipe tried seeds 5 and 4 first; neither finished routing in its
600-second lease. Seed 2 closed.

## Host checks

On the #1001 branch, with the same C64 sources, every C64 simulation passed
(`make sim-fes-c64`). The session-video simulation displays a frame read
through the shell's `fes_hps_ddr` port 0 and returns exact machine pixels
when the plane closes.

## Not established

- This package on a kit. Kit software built from `main` needs the kit SDK
  (#1062).
- Everything the [2026-10-23 session display record](2026-10-23-c64-session-display/README.md)
  established was established on the earlier package.
