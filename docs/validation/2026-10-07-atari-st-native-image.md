# Atari ST native image acceptance, 2026-10-07

The default native profile includes Atari ST after RAM Tester in its ordered nine-package set. The image carries the sealed ST firmware map and independently sealed Direct and Scanlines parts alongside ColecoVision’s parts. Firmware is separately selected by the user; no ROM is bundled.

This record accepts the exact image below on designated Kit A for diskless GEM, the two ST video selections, automatic guest disk execution, Save/Stop and cold-host saved-disk restore. It does not qualify every included core, audio or physical expansion cards. The [machine-readable evidence](2026-10-07-atari-st-native-image/evidence.json) binds installed files, sources, artifacts, executable hashes, comparisons and restoration.

## Selected artifacts

The committed image/software producer is FES `0cee5866db0e5d2d5ec426d5f3034a28b230a1bc`, incorporating main `a7617debc74b2c5b2abf008af3a2e047aacc97e3`. Later evidence/documentation commits do not relabel the tested image or executables.

| Artifact | SHA-256 or identity |
| --- | --- |
| Two-pass image, 167,772,160 bytes | `efe04797f2682c5c76372999a0628dc4f62742905e253dfc557311c0e0453075` |
| Immutable release manifest, version `0.2.0-st.1` | `749ad1ee327cd0d2581efd3ec8061dfd29ba5928a078fabaf28302f431f2b105` |
| ST package | `eba89c8b44c1650ca2ef77985ecf34f46ac27fce3e3cc33e84059b2a7bfb4e0d` |
| ST build ID | `356a0401737f24f0513103714148c944` |
| ST payload | `781d29ec5de7f9069ef76988d5eb4bf6601cb7fabbb1e8a946ed031ec0212333` |
| ST firmware map | `e49128b61df9cbcc5116aabb9811f6cd1a1b8618ac7700c62c2a1aef0d052e7d` |
| ST Direct part | `305000b8bc66aef0328832e8cff141756e1466f499ee359382116aba4f194c38` |
| ST Scanlines part | `0c4f1342b9ca1670c741b26c57120f5ab99b9ae276172f8ecdd1a6a5a1af61d1` |
| Closed four-part index | `5fd0df83a01079bb6f50396925dd80567e1556d99943102989d94e0260b71967` |

The authenticated cached FPGA producer remains original FES `a97433c1ef280683f6095a38cf785371404414e9`. Accepted ST seed 2 timing is 80.08 MHz pixel, 58.44 MHz system and 213.81 MHz audio against 74.25, 52.224 and 12.288 MHz constraints. Earlier candidates failed timing and were rejected. ST retains its own toolchain pin; the main shared compiler update did not replace it. Independent exact-shell part inspection passed source/tool provenance, clocks, timing and CRAM containment, including rejection of RAM in raster parts.

## Build and installed-image checks

The first complete assembly exceeded the old 128 MiB image’s populated-size guard. The native rootfs is now 160 MiB, retaining the 15% required free-space margin. Structural verification derives its limits from the trusted native defconfig. The accepted image contains 132,506,624 populated bytes, or 78.98%, below the 142,606,336-byte guard. Failed earlier assemblies are diagnostics, not acceptance.

Both cold image passes produced the image hash above. Structural/package-only checks and QEMU packaging passed. QEMU requires power-of-two SD devices: the smoke recipe pads only a disposable copy to 256 MiB and removes it on exit or interruption. Regressions prove unchanged original bytes and zero-only padding for 128 and 160 MiB inputs. The released 160 MiB image is unchanged. QEMU reached its ready sentinel with a read-only root and writable volatile tmpfs; this is packaging verification, not FPGA emulation.

[All 46 CI jobs](https://github.com/DeanoC/fes/actions/runs/37541541750) passed on the tested source. Focused parent, selector/CLI race, host API race, rootfs installation/verification, sealed-map, package-only and factory-video checks passed. Parent consistency verifies 18 generated consumers and 34 fixtures. The recovery-test change arms its stop chord after external-session adoption; production UI behavior is unchanged.

The normal leased appliance-update client installed, booted and durably confirmed this release. Read-only physical checks matched the complete FAT image hash, all nine installed packages and their maps/selections, the closed four-part inventory, and runtime/agent/kit/tenfoot executable hashes from both cold passes. The live root was read-only ext4 on the loop device backed by that exact release image. There were no runtime/agent overlays.

## ST boots and durable disk proof

The private host and CLI were built at the image producer commit. Its actual configuration used a 60-second upload timeout. Each boot used normal library Play with no guest input or manual program launch.

| Boot | Video | Launch time | Result |
| --- | --- | ---: | --- |
| Diskless | Direct | 54.269 s | GEM; drive A empty and volatile; normal Stop |
| Fresh disk | Scanlines | 60.728 s | AUTO guest completed; PASS/seed records; Save and Stop |
| Saved disk | Scanlines | 61.225 s | Cold private-host restart; exact saved seed bound; prior markers checked before RESTORED write; Save and Stop |

The AUTO guest returns to GEM after execution. HDMI captures show the resulting desktop, while saved disk bytes prove execution. [Diskless GEM](2026-10-07-atari-st-native-image/diskless-GEM.png), [fresh disk](2026-10-07-atari-st-native-image/seed-automatic-boot.png) and [saved disk](2026-10-07-atari-st-native-image/restore-automatic-boot.png) captures are bound by hashes in the evidence.

The fixture uses EmuTOS 1.4 US 192 KiB, SHA-256 `8fbbf8b44fc3e34281eaf8cda5265510e9af9ccda0e3e409111648060d244cfc`, and the original AUTO diagnostic program `5751778793d56cc58ea582b5322b0b47672ab3ea3b93701fe745a751aa9d35ac`. Original disk SHA-256 is `ecf5c24d56236cb2d688ab09a618c0fb1703f0a63ea09dd7317dc2fff2fa65aa`. The guest tests an odd 1,537-byte create/write/close/reopen/read/rename/delete sequence. Its second boot validates saved seed and PASS markers, including EOF, before its first write.

Save and post-Stop records matched in each phase. All 737,280 payload bytes exactly matched the independent guest models: seed `a9d2a28156ffbef7989f42d99272b844eb1af519754e4486e72bd550fc99b53c`, restore `c5a68818e6ac53d11e1c2f3596e61b0ad55eb7b11924fa79c3cb65d9e9a1744b`. No timestamp variation was needed. The parser’s `hardware_acceptance: false` identifies its offline marker inspection; physical acceptance comes from the complete image/session/guest/byte proof here.

Cold restore reused the same container, private HOME and configuration bytes/inode, with a distinct host start time. It read the saved Scanlines preference without a setter. An earlier seed preflight stopped before hardware mutation because the normal settings API atomically rewrote the private config; the helper was corrected to freeze identity after the authorized setter. Runtime and agent executable hashes, PIDs and process start ticks were unchanged across all six observations surrounding the three final probes.

## Restoration and boundaries

Managed rollback durably confirmed the actual predeployment image `5032c2d2282da26e79e6f7efc47f9f2a1aa9abca4aad4ca6f19817e82d18ce4a`. The normal host and original autostart setting are active/enabled; all 4,253 library entries were confirmed. The [populated HDMI menu](2026-10-07-atari-st-native-image/final-populated-menu.png) was inspected with the target ready/idle and lease free. The task-owned private host and exact credential copy were removed; the owner configuration is unchanged. Protected and loop-mounted images, saves and unrelated work were preserved.

The change introduces no FPGA RTL or shared schema/wire-contract changes. Spectrum’s legacy route-search timeout follow-up remains [#603](https://github.com/DeanoC/fes/issues/603), after the successful original-producer seed 4 seal. The next integration step is review and merge of PR #601; agents do not merge it themselves.
