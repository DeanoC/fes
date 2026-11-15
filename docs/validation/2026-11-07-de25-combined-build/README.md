# Paired menu/computer matched build, 2026-11-07

Implementation `ecd8042861bb1a9813da5509dd07ec157d2594a8` adds explicit native `--menu-project` selection
beside the original selected-computer HPS-first project. It keeps the original
boot context and verifies all six shared HPS/DDR/board fingerprints while each
role retains its own complete inventory and resources. The native image links
a complete canonical companion and constructs NewProgramLoaderWithMenu. Its
source/configuration parser rejects missing or crossed fields, and entry
validation requires exactly one matching linker assignment.

The original computer reader descriptor remains 8,192 bytes with its 2 MiB source
arena; the combined CPU reservation is 8 MiB. SDK board mapping, entry staging,
cold firmware and SPL use that full union. Both source projects are rechecked,
and the cold header enables the existing role-aware ROM/stop/SD-return policy
only for this explicit selection. The HPS image composer also validates the
companion and rejects a smaller CPU reservation before invoking conversion.
Its new unit case uses a synthetic converter and grants no hardware permission.

The native SDK, final entry, BL31 and U-Boot/SPL all built. Independent proof
checks their exact hashes, unchanged anchor, companion, mapping, generated
header and both 8 MiB SPL reservations. The loaded OCRAM interval ends at
`0x5de00`, within the unchanged `0x60000` limit. FIT size
is 8,272,432 bytes, within 8 MiB with 116,176 bytes spare.
Initial chain builds are retained separately from the final entry/firmware/SPL
builds containing strict linker-assignment validation. SDK source hashes were
reverified and unchanged; the later host composer update is not an SDK/entry
or firmware build input.

All 10 selected checks passed. Final parent regressions:
1,647 tests / 3 skips. Separate overlapping misteross:
1,595 tests / 79 skips; counts are not additive.
Native C/RTL/Go/Tenfoot and producer/firmware checks passed. The real menu,
ZX81 and ZX80 packages preflight through the linked companion without I/O.
[result.json](result.json) records exact inputs, results and limitations.

The current frontend still uses the serial selected-program service. Actual
Tenfoot menu display ownership, shared USB execution during active menu DDR,
matched menu asset delivery and combined physical acceptance remain open. No
FPGA synthesis, Quartus conversion, board programming or card writing occurred.
Ethernet recovery stays a separate loader. This is a matched software build,
not native boot, HDMI/input/media/network or full appliance acceptance.
