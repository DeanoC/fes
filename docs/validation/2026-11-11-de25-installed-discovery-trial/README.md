# Independent installed-package discovery: physical pass

The separate TRWU8CBL experiment passes after the
[first physical installation](../2026-11-11-de25-native-first-install-trial/README.md).
A fresh recovery/native reboot discovers ZX81, launches its verified package,
returns to the launcher and reaches final Park. Both boot traces have distinct
native owners. Boot ID stays `b3ed4389ad43d91b`, compiled at `da33a94cc`;
the second operator ran at `6473efc71`.

The actual first-install capture is hash-bound and replayed before this run.
The recovery phase freshly reads/hashes all ten original staged files and
protected recovery media. CORESEL.JSN and UI.JSON still describe an empty
bootstrap. The controller runs no network uploader or staged SD-write command.
Native startup nevertheless advertises ZX81 from its owned installed store;
`initially_discovered=true`, `installed_during_test=false`. No native Install
command is sent. The core's FPGA/ROM identity and fetch activity pass, followed
by return and final Park. This joins real installation, unchanged bootstrap,
independent reboot and discovered package activation as SD persistence evidence.
It does not prove interruption/power-loss safety.

Three configurations, two menu sessions and one computer cycle complete.
Final logical generation is 3; firmware menu drain is generation 18, ResetMask
15 and MastersContained true. All 24 App exchanges replay through the strict
discovery observer with 322 saved polling offers. Source-controlled per-cycle
receipts keep `persistence_verified=false`; the outer experiment publishes the
joined persistence verdict only after the independently bound prerequisite,
fresh readback, native discovery/launch/return and transport cleanup pass.

Independent cleanup verifies both private-service process identities absent,
service archive hashes, 15 MHz/automatic-clock readbacks, exact USB permissions
and a complete free top-UART inventory. Four actual image programmings across
the two experiments succeed; the historical incomplete-programming count stays
three. The test card remains in TRWU8CBL and the native runtime is parked. The
factory card and bottom kit are untouched; no power cycle or restoration swap
is requested.

Complete wire/receipts/helpers and source are privately retained at
`/home/deano/fes-retained/de25-native-reboot-discovery-passed-20261111-r1`.
Archive hash/mode readback and independent source restore/fsck/pack verification
pass. The first-install and native-candidate archives are declared dependencies.
`hardware-result.json` and `hardware-retention-proof.json` pin the verdict and
retention identities. The earlier frozen/offline records remain preparation,
never reusable live authority. No push, PR, merge or old-output cleanup occurs.

This is one bounded ZX81 installation/reboot/launch qualification. Current-image
physical HDMI/key-action observation, broader core/package/device compatibility,
native Ethernet, USB hotplug, power-loss behavior and full DE10-equivalent
appliance acceptance remain open.
