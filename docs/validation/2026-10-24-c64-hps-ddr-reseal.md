# 2026-10-24: C64 0.5.0 resealed after the shell took the HPS DDR bridge, and a kit A check

#1001 moved the HPS DDR bridge out of the shared display plane and into each
shell: the plane is now a port-0 client of `fes_hps_ddr`. The C64 top gained
that bridge in the same PR, so the bitstream sealed for #1032
(`713b811056616b15…`, build `b7bf0ddbbf358a3b…`) is no longer what `main`
builds. This record seals the C64 from `main` after #1001 and runs the
sealed archive on kit A with kit software built from `main`. It is not an
image or a release.

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

## Kit A check

Run `kswA1` (05:22 to 05:36 EET, 2026-10-25) ran this archive on kit A. Its
running image `4db74864` was not changed. The kit SDK was published with
`make kit-sdk` (#1063) from `main` `0f3b35c9`, and kit software built from
that commit (bundle archive `0f1f757a…`, installed as `115494f3…`) was
installed and activated over the baseline `35d58489`. The private host was
built from the same commit. The archive was imported, checked for
compatibility, linked to a 36 KiB firmware image composed in memory from
locally held ROMs, launched, seen running on three status samples and
stopped. The kit reported build `795666e322409e567db496b9b78c8c48`. HDMI showed the
BASIC V2 start screen with 38911 bytes free, and a `PRINT 6` typed through
the host's input path printed 6. A 20-second capture had no HDMI relock, and
the ADV7513 `0xBA` delay read `0x20` (read only). The software was restored
to `35d58489`, the image fingerprints matched before and after, and kit A
was released at 05:36.

## Not established

- Disk loading and the live disk selector on this package.
- Everything the [2026-10-23 session display record](2026-10-23-c64-session-display/README.md)
  established was established on the earlier package.
