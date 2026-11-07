# Menu/computer load-role publication, 2026-11-07

The existing fixed per-load descriptor now has an explicitly selected version-three
role prototype. Trusted firmware setup enables this mode before the first capture;
requests cannot enable or downgrade it. The role and control ABI occupy the final
eight bytes after all nine chunk digest slots. The fixed 128-byte extension is
zero for menu and binds the privately staged 4/8 KiB ROM for the current computer
driver. The native arena rejects crossed role/ABI/ROM resources and retains failed
leases. Addresses, masks, master inventory and boot authority remain outside the
request.

Five mixed-role loads run through the actual C descriptor/configuration/held-bridge
engines with injected ownership, registers and digest responses. Twenty cases
reject unknown/crossed roles, downgrade/no-opt-in, stale owner/generation, changed
header or extension, unused-byte corruption, missing ROM and failed ownership or
read callbacks, then prove no replay. Menu capture clears preceding ROM state.
Native Go encoding and real arena publications pair with that C decoder for five
role changes, both ROM sizes and all nine digest slots. Stale callbacks cannot
alter the next lease; malformed resources cannot publish or release an arena.
This executes no mixed-role scanout reader or ROM FIFO.

The SDK cross-build and freestanding AArch64 module compilation passed, as did
all eleven affected-plan steps, including producer identity, board-control RTL,
firmware/RTL, generated consumers, native C/RTL/Go/real-Tenfoot integration,
the parent checks and unfiltered Python suite.
[result.json](result.json) identifies the exact source and evidence hashes.
An initial focused compile failed after moving the extension sampler because its
size check still named the former local variable; that reference was corrected
before the passing focused and complete checks.

Preparation also re-derived the retained real menu/computer FPGA source maps.
Their platform, device, HPS I/O hash, PFG identity, HPS/EMIF parameter digest and
complete consumed boot-configuration digest match. Their master inventories differ
and remain separately pinned. This is source compatibility, not live hardware
acceptance or permission to adopt another role's stop policy.

The current loader and cold platform do not enable mixed-role capture. Next
compile both role resource sets, select the captured role's active identity and
master-stop/ROM policy, then join the existing menu SD-return and ProgramService
with the real Tenfoot launch/return path. Native menu/core/menu, physical display,
input/media/networking, broader admission and timing remain open. No board was
programmed or card written during this step.

The parent runner reports 1,638 tests with three skips; the separate unfiltered
Python command reports 1,595 tests with 79 conditional skips. They are separate
runner scopes, not additive totals. The new role cases and native/C decoder
pairing ran and passed; skipped cases grant no additional acceptance.
