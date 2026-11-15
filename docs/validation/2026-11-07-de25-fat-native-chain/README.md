# Fresh native chain for per-walk FAT metadata reuse

Implementation/SDK/entry/firmware source: `e2bd5808abae8990b737c25a520a72cdf98ee09a`.
The [preceding physical baseline](../2026-11-07-de25-timer-launch-trial/README.md)
passed all eight uploads and three serial native launch/return cycles. Launches
took about 72.5 seconds; roughly 60.6 seconds lay outside the traced EL3 transfer
window. Those observations did not localize a sole cause.

The targeted [FAT change](../2026-11-07-de25-fat-chain/README.md) removes repeated
table-sector reads within a single locked chain walk. Its SDK has exactly one
changed compiled source, `fat.go`, and no added compiled inputs. All ordinary
package/ROM maps, seven catalogue payloads, sealed FPGA bytes and existing time
and ownership bounds remain intact. No verification is removed or cached across
walks. This build does not add a broad launch observer or alter firmware tracing.

Fresh SDK, entry, secure firmware, native U-Boot FIT/SPL and matched HPS-first
SOF builds pass their existing gates and bounds. Composition used the shared
Quartus lock and did not rebuild FPGA source. The unchanged normal native
checker passes the fresh chain and rejects the old SDK/chain after the FAT
input change: `native selected source changed: platform/embedded/tamago/fat/fat.go`. Actual Go preflight accepts both retained-ROM choices
without firmware I/O. Twenty-four low-level driver/PSCI objects remain exact;
the initialized aligned CPU array and both 2-MiB firmware DDR reservations pass.

FIT: 8,219,184 bytes, `fb2ad0ed261cd0502cf8395dab113bb4d766bcbbb7737a7a3eb74fa2b2bdae4e`.
SPL: `b8f98a972e90962e4034622328bf0a4e0c1a1fee722dbb953ea0dd21d328cd76`.
SOF: `6ef984759cdff21d4ee825f1d4841cd56824415fe779b0ec5924777637f90541`.
Eight fixed launch files are listed in `upload-plan.json`.

These are fresh file artifacts and off-board checks. They inherit no physical
boot, performance or core acceptance. The next step is a newly frozen bounded
eight-upload/three-launch-return test against the retained baseline. The board
was last verified after native Quit at ready generation 6; it requires fresh
recovery/card/source/USB checks before writes. The continuing top-kit claim is
held, factory restoration remains waived, its original card stays safe aside,
and the bottom kit is untouched. General package acceptance, display/input/media/
native network behavior and four FPGA signoff findings remain open.

Raw evidence: `out/nano-fat-native-chain-20261107`.
Retention: `/home/deano/fes-retained/de25-fat-chain-20261107-r1`.
