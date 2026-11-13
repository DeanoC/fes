# Sealed Nano ROM resources and cold firmware selection

Commit `b581998b2` connects the ROM producer's generated resources to native entry
and cold firmware validation. It derives the actual candidate's 128-byte CPU
region at `0x20010500`, 8 KiB ROM size, held computer conduit, 100 MHz clock,
source inventory and complete master-stop binding. The resource binds the
package/build, generated handoff, control map and computer map. It adds no
hardware permission or new master.

The producer remains `c60747191`, with diagnostic build
`cb63a0a04c6ca842320a760df7e3ef30` and package
`4bcf0e0a3b79335502afa8c2388284ec8180a6d3894d77bae3e7f97cdec6e31b`.
Fresh derivation from its retained sealed project passed. Native entry requires
the same complete ROM record and its presence in the native ELF; cold-chain
preparation rederives it. The cold header enables the existing secure ROM
provider only with that contract. Older profiles reject attached ROM resources
and emit the disabled selection. Missing resources cannot send a ROM producer
through the older boot path.

Seven focused tests cover both image sizes, unique/nonoverlapping CPU regions,
altered/missing fields, source inventories, package/build/ownership links,
complete master stop, fresh receipt equality and cold header selection. All
eight affected-check commands passed, including the aggregate parent suite
(1,483 cases, 2 skipped), native firmware/FPGA launch integration and embedded
producer/native-control integration. Existing focused computer, context and
cold-boot tests also passed.

The actual secure ARM64 provider translation unit compiled with the generated
resource header and pinned vendor headers. That fixture uses a synthetic entry
identity; it is not a matched boot chain. A fresh derivation from the committed
contract produced the exact header bytes used by that compile. The provider's
object and compile closure are retained. No board operation or Quartus rebuild
was performed for this change.

Evidence is retained at
`/home/deano/fes-retained/de25-rom-resources-20261105-r1/`. The source update was
restored with its explicitly recorded source, frozen-board and publication
prerequisite bundles and passed strict Git fsck. The preceding producer candidate
and its build evidence remain in `de25-rom-board-20261105-r1`.

The next integration is Nano ROM retention and publication. Current package
validation recognizes Nano only for format 2; formats 3/4 use the Cyclone V
bitstream ROM linker. This endpoint instead receives its ROM separately through
secure firmware. That path needs a package/build-bound retained ROM and receipt,
then the version-two program publication and matched native build. The selected
native frontend still rejects this ROM profile. Ordinary admission, a matched
ROM boot image and physical ROM loading remain unproven; full DE10-equivalent
core loading is not complete.
