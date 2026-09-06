# SNES cartridge saves

Native SNES saves preserve ordinary battery-backed cartridge RAM. Use the game's
own save feature, then Stop in FogCast before switching systems or rebooting.
The next launch of the same game and ROM restores that RAM before play starts.
This is cartridge saving, not a save-state feature. Continuous power-loss
protection and enhancement-chip cartridges are outside this milestone.

## Where saves live

The target agent selects files beneath `/media/fat/fogcast/saves/snes/`, separate
from the ROM cache. Each game gets a directory named by the SHA-256 of its game
ID; each raw ROM gets a `.srm` file named by its SHA-256. Cache relocation and
agent restart preserve this identity. Different games and changed ROM bytes get
different saves, including versions that differ only by a copier header.

Save files contain raw cartridge RAM, not an FES-specific envelope. Ordinary
battery cartridge type 2 with RAM exponents 1–7 uses 2–128 KiB. Cartridges with
no battery RAM do not create a save file. An existing file with the wrong size
is rejected before the core is programmed; it is never silently truncated.
Back up this save directory separately from the replaceable Linux root image.
Host synchronization and a save-management UI are not included.

## Stop and failure behavior

A successful Stop captures complete SRAM, writes and syncs a temporary file,
atomically replaces the previous save, then loads idle. Startup, failed launch,
and generic input-fault cleanup do not write potentially incomplete game RAM.

If writing fails, FogCast reports an error and retains the kit lease. The game
may be frozen while its snapshot is retained. Correct the storage problem and
retry Stop with the same owner; no takeover or unlock is required. A complete
captured snapshot is retained for a file-write retry, and partial transfers do
not replace the previous save. A successful Stop is the persistence boundary.

## Component responsibilities

FogCast passes the validated game identity through the existing launch path and
selects the persistent save filename. Its native adapter adds the optional
local `save_path` field only for SNES. Main, Mega Drive and Pong keep their
existing launch behavior. The production target sets the save root; standalone
native adapter callers can explicitly omit it for volatile cartridge RAM.

libmister-runtime derives battery RAM size from the admitted cartridge. It
mounts the save while download is asserted, restores through the existing SNES
virtual-SD transport before starting input, and snapshots before ordinary Stop.
All SPI save transactions run after input has stopped. The existing runtime
mutation boundary also prevents a late input callback from discarding a failed
save's retained session.

The existing source-built SNES RBF already implements this transport; no FPGA
source changes or Quartus rebuild are required. The implementation design and
remaining integration work are tracked in the
[save plan](superpowers/plans/2026-09-06-snes-saves.md). The dated evidence below
identifies the candidate revisions and separates diagnostic hardware tests from
normal image verification.


## Diagnostic hardware validation (2026-09-06)

The designated kit ran runtime `93b369f7bf56757697cc5e59332545f5b4ee62f3`
and FogCast `5fc0b4ad8cac63be8fc6e9d0b9e82c8aede8baac` in derived image
`036915d57c700d8369659af922a7d5771e736b828395f5dd2ae8ff6fc92ba353`.
The existing three core RBFs and verified base image were retained unchanged.

- Zelda: A Link to the Past created player slot `G`; Stop wrote its 8 KiB SRAM.
  Pong then launched and accepted input. After a full kit reboot, Zelda restored
  slot `G` with its three hearts visible in the player selection screen.
- A controlled replacement failure returned the save error while ownership
  remained held. Repairing the path and retrying Stop with the same owner
  succeeded; the complete previous save bytes were preserved.
- Super Mario World wrote a separate 2 KiB save and relaunched with restore.
  Its lifecycle did not change Zelda's saved bytes.
- Fievel Goes West, a non-battery HiROM cartridge, launched and stopped without
  creating a save file.
- Mega Drive Sonic 2 ran with visible gameplay after the SNES checks. Final
  Stop succeeded, and the kit lease was released with the agent idle.

Local evidence is retained in `out/snes-save-diagnostic/`, including source and
binary identities, image assembly/verification, launch/Stop logs, before/after
screenshots, save bytes and failure/retry records. No prior saves existed in the
new save root. Raw saves, screenshots and ROMs are not committed.

This is physical validation of a derived diagnostic image; it does not relabel
the earlier verified image as having save support.

## Normal image validation (2026-09-06)

With the same selected component revisions, `make build` produced identical
images in two independent passes:
`88d5a2505b7348ea934692e2c43fa06d0fef651bbc71c3f01c4f3fba934308b6`.
`make verify` passed reproducibility, structural checks and the QEMU boot smoke
test. QEMU validates packaging and boot, not FPGA operation. The three existing
source-built FPGA bundles were reused.

The normal image is available under `out/native-integration-dev/`; its records
are also preserved in `out/snes-save-image-evidence/`. The selected changes are
submitted for review in
[libmister-runtime #15](https://github.com/DeanoC/libmister-runtime/pull/15) and
[FogCast #149](https://github.com/DeanoC/FogCast/pull/149). Integrate the runtime,
then FogCast, then this parent selection, preserving the tested commits.

## Normal-image hardware acceptance (2026-09-06)

The designated kit booted the normal image above. Installed runtime, agent and
all three RBF hashes matched its manifest. Existing saves were backed up before
deployment, and the preceding diagnostic image was retained.

- Zelda restored the existing player slot `G`. A new slot `H` was created on the
  normal image; Stop wrote 8 KiB of SRAM, then Pong ran with paddle input.
- A further full reboot changed the boot ID. Both save files were byte-for-byte
  unchanged across reboot, and Zelda visibly restored both `G` and `H`.
- A controlled save-replacement failure returned the expected error and retained
  the same lease. The previous file remained intact; repairing the path and
  retrying Stop with that owner succeeded.
- Super Mario World's separate 2 KiB save completed Stop and restore without
  changing Zelda's file. This checks the smaller SRAM transfer; no completed
  Super Mario World level/progress save is claimed.
- Non-battery HiROM Fievel Goes West launched with its title visible and created
  no save file. Mega Drive Sonic 2 also launched with its title visible.

Detailed logs, screenshots, save hashes, deployment and reboot records are under
`out/snes-save-image-evidence/hardware/`. These checks apply to the exact normal
image and the ordinary-cartridge save milestone. They do not expand cartridge
support or establish new FPGA timing or audio-quality results.

Final Stop succeeded with no agent error, and the lease was released. The kit
remains on the accepted normal image with both saves retained.
