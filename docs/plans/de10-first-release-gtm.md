# First DE10-Nano FES release: go-to-market and acceptance

**Status:** planning appendix. The [v1 first-boot and setup plan](v1-first-release-first-boot-setup.md) records the chosen systems, product decisions and milestone issues. The [support matrix](../support-matrix.md) describes current core standing; neither a matrix tier nor this plan is hardware acceptance for a particular artifact.

## Audience and positioning

The proposed first audience is technically confident DE10-Nano owners: existing MiSTer users interested in a focused set of cores, FPGA hobbyists interested in the open build lane, and retro-computing and homebrew users who can supply their own media. A broader plug-and-play promise depends on completing the first-boot, controller, storage and update paths in the v1 plan.

Describe FES as a focused DE10-Nano FPGA platform with independently installed core packages, a tenfoot interface and open build options. Be clear that its system coverage and package format differ from stock MiSTer, and label each published package's actual toolchain. Explain which systems include redistributable firmware and which need firmware or media supplied by the owner. The [firmware licensing record](v1-firmware-licensing.md) records the v1 decisions and their outstanding work.

## Public release and support

Keep developer build procedures in [getting started](../getting-started.md), [bootable media](../bootable-media.md) and [development](../development.md). The public quickstart and release page should cover the supported board, image and checksum, flashing, first boot, tested controllers, games folders, core installation, firmware and media, updates, recovery and known limitations ([#1356](https://github.com/DeanoC/fes/issues/1356)). Publish the actual supported and experimental scope and a support location alongside the release.

A small beta and announcements to relevant FPGA and retro-computing communities are options, not a committed schedule. Ask testers for reproducible reports identifying the image, bundle and core package; do not ask them to share private firmware, game files or credentials.

### Release checklist

The release-candidate acceptance issue is [#1355](https://github.com/DeanoC/fes/issues/1355). Freeze the artifacts and check the promised user paths before making release claims:

- Identify the promoted systems and their package variants, required firmware and media, supported features and actual toolchain labels. Keep experimental systems and utilities distinct.
- Build the named image and kit-software bundle from reviewed source; retain checksums, reproducibility and verification receipts, package IDs and firmware licence and provenance information. The default image supplies MENU and idle firmware; playable cores are installed separately.
- Verify the image and media receipts. Check that public media has no developer configuration or shared token, includes only firmware and artwork permitted for distribution, and has a documented recovery path.
- On a freshly provisioned DE10-Nano, test the exact release image, bundle and promoted packages: boot and HDMI, menu focus, documented wired and Bluetooth controllers, setup and library scan, installation, firmware and media loading, video/audio/input, Stop, and the save behavior promised for each system, including save failure and retry where supported. Test updates, rollback and reboot on the same named artifacts.
- Exercise a clean user profile with no preconfigured library and no network, including the supported offline first-use path. Record limitations instead of assuming all systems have bundled firmware or games.
- Tag the source and release artifacts together. Publish the quickstart, checksums, supported-controller list, toolchain labels, known issues, update compatibility and recovery guidance with the release.
- Record image and bundle hashes, package IDs, source revisions, controller models and results in dated evidence. Host tests, simulation, seals and tests of older artifacts do not establish acceptance of the release artifacts.

The proposed target is DE10-Nano/Cyclone V; DE25-Nano is a separate hardware and toolchain path. Release timing, website ownership and beta size remain planning decisions.
