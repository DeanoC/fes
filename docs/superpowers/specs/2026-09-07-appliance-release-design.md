# FES appliance release and automatic fallback

The user approved the release/update milestone and explicitly selected automatic
fallback, including a candidate that fails before networking. Implementation uses
isolated FES and FogCast worktrees under `out/dev/appliance-release`.

## Scope and ownership

FES selects compatible sources and builds the release/media artifacts. FogCast
owns the network updater and a small static bootstrap executable. The existing
runtime owns FPGA transitions; Stop must succeed before update activation. The
UI team consumes the API later; this work adds no UI. Preserve the existing card
identity, credentials, catalog cache and SNES saves.

First restore runtime bdf56ab's 120-second media deadline in FogCast's lock and
test fixtures, starting from merged FogCast 2c55c21 and FES f113a1d. Clear the
existing discovery cancellation vet warning at its retry boundary. Keep root
`sources/` and other workers' branches untouched. Local build checkpoint commits
are required because FES builders consume committed, clean component inputs.

## Boot choice

Keep the locked kernel and U-Boot. The kernel still mounts `/linux/linux.img` on
loop8; that file becomes a stable minimal bootstrap, excluded from online updates.
The bootstrap verifies and mounts a selected immutable system image on another
loop device, then uses pivot_root and exec to run that image's `/sbin/init` as
PID 1. It retains the old bootstrap mount for the independent watchdog guard.
BusyBox switch_root is unsuitable because the existing root is ext4.

An agent-only timer cannot recover pre-init failure. Changing the locked kernel
or U-Boot would expand the compatibility surface. A fixed bootstrap using the
present kernel's loop, pivot_root and enabled DesignWare watchdog is the selected
approach. A bootstrap/kernel update remains a separately installed media update.

## Release and state contract

A release consists of a closed JSON manifest and a raw ext4 image. Its manifest
binds format 1, board `de10-nano`, boot ABI `fes-bootstrap-v1`, display version,
kernel SHA-256, image SHA-256 and size, and exact FES/FogCast/runtime revisions.
The rootfs does not embed its own hash. The bootstrap provides the actual running
image identity to the target after selecting and hashing it.

Images are immutable content-addressed files beneath
`/media/fat/fogcast/releases/images/`. Installation never overwrites an image
referenced by the running loop device. The initial media contains a factory
manifest in the stable bootstrap and the corresponding factory system image.
The writable update state retains known-good, previous, pending candidate and
whether a trial has been consumed. State is versioned and checksummed; writes
use a temporary file, file sync, rename and directory sync under a local file
lock. Missing state selects factory. Invalid or torn state selects factory,
never an unverified candidate. Configurations and saves are outside every image.

Before a trial executes, bootstrap durably consumes its one attempt. On a later
boot an unconfirmed consumed trial selects known-good. Invalid image hashes,
mount errors or missing executable init select known-good during the same boot.
If known-good is invalid, try factory; never loop repeatedly through a failed
candidate. Missing/corrupt factory is a reported unrecoverable media failure.

## Watchdog and confirmation

A bootstrap-owned guard opens the hardware watchdog, configures its supported
timeout, and acknowledges that it is armed before candidate init executes. It
pings only until the bounded trial deadline (180 seconds) or durable confirmation.
It survives root switching independently of candidate services. Errors, signals
and deadline expiry never magic-close the device: stopping pings causes reset.
Only durable confirmation for this boot ID and image permits magic-close.
Failure to arm the watchdog rejects trial boot. Known-good boot does not depend
on the host being online.

The host reconnects using the existing target identity/discovery path, checks a
changed boot ID, the exact running image hash and raw native idle readiness, then
confirms using a newly claimed kit lease. Old pre-reboot lease tokens are invalid.
During an unconfirmed trial, normal launch/development/input/cast admission is
blocked; update status, lease claim and confirmation remain available. A host
that disappears cannot silently accept a trial: the watchdog returns to good.

## Update and rollback operations

The authenticated target API exposes update status, bounded upload, activate,
confirm and rollback. Mutations require the existing kit lease. Upload verifies
the closed manifest, board/kernel/boot compatibility, available space, declared
size, full image hash and ext4 identity before publication. Incomplete, canceled
or mismatched uploads cannot change selection. No archive paths are extracted.

Activation runs under the coordinator's existing exclusive transition and
requires verified idle after successful Stop/save persistence. Persist pending
selection before requesting reboot. A lost reply is resolved through update
status and boot identity; the host never blindly replays activation. Reboot uses
the existing OS reboot mechanism only after the idle transition is held.

Rollback selects the retained previous image as a bounded trial and follows the
same reboot/confirmation protocol. Ordinary updates never replace bootstrap,
kernel, U-Boot, credentials or saves. There are no destructive configuration
migrations in this format. One-time migration from the old direct-root image is
an explicit lease-owned provisioning step with the old file retained by name.

## Validation and completion

Use filesystem tests for truncated uploads, digest/ABI mismatch, full disk,
interrupted writes, consumed trial, malformed state, immutable-image retention
and exact confirmation. Exercise HTTP authentication, lease fencing, admission
and lost-response behavior through production handlers. Test Linux root switch
and watchdog primitives in disposable execution environments before kit use.

Run existing parent tests, component race tests and vet, consistency, static
ARM builds, then one clean assembled-image verification at stabilization. Reuse
the unchanged compilers and FPGA bundles during iteration. Hardware acceptance
under the kit lease covers prepared-card boot, good update, unconfirmed trial,
unbootable image fallback, explicit rollback, controller reconnect, host restart,
and SNES progress through clean Stop/reboot. Record artifact identities and actual
results; simulated tests do not establish watchdog or FPGA hardware acceptance.

Automatic recovery assumes intact stable bootstrap, kernel, factory image and
functioning card/watchdog hardware. Online updates do not modify those boot files.
