# Native menu diagnostic runtime image — 2026-09-28

A runtime-only diagnostic image is prepared for the pending menu test. It is
not deployed, selected for boot or exported as an appliance release. The kit
lease was free during read-only status inspection; no kit state was changed.

## Artifact identities

The source image is the retained, verified native integration image
`0103b5f04cbe256ed84e97d589ec5cf108b1031e415820bd28c8bd3cb16b1a4d`.
Both retained clean and development copies match these bytes. Its existing
verification receipt reports structural, two-pass reproducibility and QEMU
packaging passes. This is also the installed root image observed during the
[presentation precondition check](2026-09-27-native-menu-presentation.md).

The separate derivative is 67,108,864 bytes, SHA-256
`33d6be81601d1b8db63d2407ae3853aba554c379aa86454603dc492f291bc692`.
It replaces `/usr/sbin/mister-runtime` with the tested ARM binary
`fe5d1f6d00371066abf96036b15cbb1205d959efc4ebff2c23a2b2c90441bf56`,
version `git-8907788dde54`, built from committed source
`8907788dde541788728778fdc5d2f2d2591b280b`.

Files are retained in the native-menu worktree under
`out/validation/native-menu-presentation/diagnostic-image/`:
`rootfs.img`, `diagnostic.json`, the injection commands, file comparison and
filesystem/loader logs. The original image remains untouched. The receipt is
explicitly diagnostic; it does not replace the source image's release evidence.

## Checks

`e2fsck -fn` passes all five filesystem checks. Extraction verifies the new
runtime's exact digest; its inode is a regular executable, mode 0755,
owner root:root and one link. Comparison of 590 extracted payload paths,
including regular-file bytes, modes and symlinks, finds exactly one change:
`usr/sbin/mister-runtime`. Source image hashes still match after inspection.

The ARM loader runs under QEMU using the derivative's own libraries and resolves
libstdc++, libm, libgcc_s, libpthread, libc and the ARM loader successfully.
This checks the executable's dynamic library closure; it does not run the
runtime lifecycle or emulate FPGA/DDR hardware. Existing software and sealed
FPGA qualification remain in the [presentation record](2026-09-27-native-menu-presentation.md)
and [package record](2026-09-27-native-menu-package.md).

## Next physical session

The operator deferred the power cycle and physical test. At that session,
claim the designated kit lease and stage the separate diagnostic boot candidate
through the appliance workflow, retaining factory, known-good, previous and
loop-referenced images. The updated runtime must be the first runtime to
program FPGA after boot. `LinuxFpgaManager::Program` captures the boot layout
before any programming and writes `/run/mister-runtime-boot-hps-ddr`.

After the coordinated power cycle, verify the actual boot image/runtime
identities, `hps_ddr.boot` capture and a genuine `latched` record before menu
configuration. A missing or `absent` result leaves the DDR test unqualified;
the earlier diagnostic's manually seeded record is not reused.

Then configure sealed menu package
`0d1ecd3328237fb4ba93e69c69dee45f48b4251a063e54cc86e6f7a8c96b1cc2`,
run the pattern client, measure latency and CPU, require ten minutes with no
underflows, and exercise menu/game/Stop and descriptor failure cases. Restore
the prior idle/image selection and release the lease afterward. This derivative
supports a diagnostic session; exact-image/appliance acceptance still requires
its own stabilized integration evidence.
