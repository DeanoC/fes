# Native menu appliance release on the designated kit — 2026-09-28

Classification: **exact-artifact hardware pass** for boot menu presentation,
host launch of the selected Pong package, and Stop returning to the menu. This
does not establish acceptance of controller navigation, audio, or every core.

The FES build commit is `1280993f6f6d77fba24c5921751f1aae7a8d41ae`,
based on `4e709a35b2a2cbfe9637b22cebf1c2ecd308346b` (main after PR #271).
`make check`, the 587-test parent suite (39 skips), focused FogCast/runtime/image
tests, the two-pass cold `make build`, and `make verify` passed. The two builds
matched at rootfs SHA-256
`b239748a4feb360f56765ebf8d2f887e017cde049c401e071e4c7e1f394ed42c`;
the verifier passed structural and QEMU packaging checks. `make release
RELEASE_VERSION=0.2.0-native-menu.1` exported the immutable release under
`out/native-integration-dev/appliance/releases/<rootfs-sha>/<manifest-sha>/`,
with manifest SHA-256
`6934677fa16870088c48757bd0b44435164ee80091292fe40505e2a37e303580`.
The release command required a separately provisioned locked boot payload cache;
the pinned checkout and payload hashes were validated before export.

On the designated `192.168.10.84` kit, the lease was free before the appliance
updater staged and activated that exact release. The updater observed boot ID
`16a24990-5042-4fae-a3a8-55ca44962d52`, confirmed image SHA-256
`b239748a4feb360f56765ebf8d2f887e017cde049c401e071e4c7e1f394ed42c`,
`trial=false`, and `raw_idle_ready=true`. The previous confirmed image
`0103b5f04cbe256ed84e97d589ec5cf108b1031e415820bd28c8bd3cb16b1a4d`
remains available. Network update changed the system image only, not U-Boot,
kernel, FAT boot files, or credentials.

ShadowCast HDMI capture showed the native menu after boot while the host was
offline. A temporary matching host (`1280993f6f6d77fba24c5921751f1aae7a8d41ae`)
connected, reported target agent and runtime at that same revision, and showed
the live menu. The host installed the archive for the image-selected `fes.pong`
package `b7ca89754e4330f3a1ccc975ce36b466c8820564306e412bc234fd9bfef3b8a3`
and created library game `fpga-fes-pong-menu-release-a565c2b2b05e`. A host
library launch reported that exact package active with build ID
`11060a78e0e2d73a2ac968e011b800fb`; HDMI capture showed Pong. Host Stop
returned `idle`, detached input and restored the native menu on HDMI. The kit
lease was free afterward, the confirmed image was unchanged, and the temporary
host was shut down. Capture stills are retained locally at
`/tmp/native-menu-release.png`, `/tmp/native-menu-pong.png`, and
`/tmp/native-menu-return.png`; occasional ShadowCast MJPEG decode warnings did
not prevent these visual observations.

This release **does not** derive a per-kit Ethernet MAC address from SD-card
serial. Neither this build commit nor its main base contains that assignment.
That work needs a separate source change and validation before it can be claimed
for a multi-kit release.
