# Launch composition

A library launch composes a selected core package with the title's required
ROMs, media and optional expansions. The package descriptor supplies their
names, sizes and contracts. The host resolves the selections and checks their
readiness before asking the target agent to start a session. The runtime owns
physical programming, media delivery and return to idle. The host never infers
requirements from a display name, file extension or raw RBF.

| Slot | Purpose | When selected |
| --- | --- | --- |
| Core | The sealed FPGA package | Before boot |
| Firmware | CPU boot ROM or BIOS | Before boot |
| Primary media | The cartridge, tape or disk the title starts with | Before boot when required |
| Expansion | An optional hardware asset linked to the shell | Before programming |
| Secondary media | Removable media that the running machine can change | At launch or during play |

The current execution paths and boundaries are in
[architecture](ARCHITECTURE.md); package selection and API examples are in
[the core package library](core-package-library.md).

## ROMs and media

A format-3 package declares one named ROM. A format-4 Coleco package declares
an 8192-byte BIOS and an exact-size cartridge as separate named requirements.
A title selects each object by its manifest name, and another title may choose
a different BIOS or cartridge. Media objects are content-addressed and checked
on read; private BIOS and cartridge bytes are not stored in Git. Import alone does not select an
object. The host checks required ROM bindings before launch; the target links
the source bytes and validates the result before programming.

Schema 23 preserves saved named ROM bindings and imported media, and removes
the old household BIOS selection. Titles that relied on that selection need an
explicit per-title BIOS choice. A missing, corrupt or stale selection blocks
the title until repaired.
The default image does not include a private BIOS.

Removable media is a separate selection. Some packages accept media at launch
and can change it during play; supported operations follow their declared
interfaces. See [ZX81 tape media](../../../docs/zx81-tape-media.md) and
[removable disks](ARCHITECTURE.md#removable-disks-fescomputer-media-units).

## Expansion and video parts

An expansion asset is linked to its compatible shell before programming, not
compiled from scratch on each launch. The package, socket, build and payload
identities constrain which assets may be linked. Apple II titles can select
multiple independently named slot cards. Video parts likewise belong to their
compatible shells and the selected output must be available before launch.
The standard ZX81 shell has 1 KiB internal RAM and a vacant expansion bus;
compatible bus carts, including the 16 KiB RAM cart, link at load time.
See [expansions and named ROMs](core-package-library.md) and the
[ZX81 expansion-bus guide](../../../docs/zx81-expansion-bus.md).

## Readiness and evidence

An installed package is not necessarily ready to play. Library and room
readiness includes its required ROMs and other selected assets as well as the
chosen target's availability. The UI offers a repair action for a missing
firmware requirement; a failed admission does not claim a successful session.
Installed FPGA library entries remain launchable as a platform even when a
raw cartridge from that system is browse-only; composition readiness is an
additional gate. When a mesh executor is selected, `ReadyHere` also requires
the selected executor, required slots and a free lease. Firmware import does
not itself claim the kit.
Physical acceptance applies only to the exact artifacts and operations recorded
in each validation. A simulation, a sealed package and a kit play test are
separate kinds of evidence.
