# PC/XT DOS application kit diagnostic

This opt-in diagnostic exercises a DOS COM program or Digger Remastered in an
**already owned, active** PC/XT kit session. It does not install a bundle, claim
or release a kit, launch or stop a session, or restore the baseline. The kit
owner must carry out those steps using the repository's shared-kit rules and
normal queue procedure, including cleanup after any failure or interruption.
The ordinary `fes.pcxt-dos` prompt-capture gate is unchanged.

Before running, the owner must hold the kit's status-file claim and queue lock.
The diagnostic requires the actual open file descriptor that holds the exclusive
flock, inherited from the owner process. It verifies that descriptor against
Linux `/proc/self/fdinfo` and the kit queue lock file before every input and
capture. A different process holding the lock does not qualify. For example,
when opening a new queue claim in the same shell that launches the diagnostic:

```sh
exec {KIT_LOCK_FD}</home/deano/kit-status/kitA.lock
flock -n "$KIT_LOCK_FD" || exit 1
# Record the kit claim and complete the caller-owned preparation here.
# Keep this shell and descriptor open until all restoration and release finish.
```

For kit B the queue lock is `/home/deano/tmp/lander-kitB.lock`. Follow the
shared-kit queue and claim rules; the example does not itself create a claim.
The latest `KIT A IN USE: <owner>` or `KIT B IN USE: <owner>` claim line must
also include `target_id=<exact-target-id>` so the selected target is explicitly
bound to the claimed kit. The owner must then
activate the matching software bundle, import the sealed package and select the
floppy in a library entry, then launch the package and wait for an attached HID
input session and a DOS prompt. Use a private loopback FogCast host configured
with remote input and only that target enabled. Save the owning active
`GET /api/v1/session` response, including its attached input session, in a
private file. The diagnostic checks this identity against the live foreground
and named-target sessions, boot ID, package, media, software, current claim and
queue lock before each input and capture. It will not attach input itself.

The COM image can be built from the pinned FreeDOS base. The game image needs
the separately obtained executable described in the
[PC/XT core README](../sources/misteross/cores/fes-pcxt/README.md#dos-application-acceptance);
the game is not downloaded or redistributed by FES. From `sources/misteross`:

```sh
python3 scripts/pcxt_acceptance_media.py com --output /private/pcxt-com.img
python3 scripts/pcxt_acceptance_media.py game --digger-exe /private/DIGGER.EXE --output /private/pcxt-game.img
```

From the FES root, run one scenario at a time, substituting the **exact**
identities from the prepared package, software receipt and owned session:

```sh
python3 scripts/pcxt_dos_application_acceptance.py com --execute \
  --kit A --owner "$KIT_OWNER" --queue-lock-fd "$KIT_LOCK_FD" \
  --host-receipt /private/host.json --host-receipt-sha256 "$HOST_RECEIPT_SHA256" \
  --host-binary /private/fogcast-api \
  --software-receipt /private/software-receipt.json --software-receipt-sha256 "$SOFTWARE_RECEIPT_SHA256" \
  --host-api http://127.0.0.1:8787 \
  --config /private/host.toml --owned-session /private/active-session.json \
  --archive /private/core.fcore --archive-sha256 "$ARCHIVE_SHA256" \
  --rbf-sha256 "$RBF_SHA256" --package-id "$PACKAGE_ID" --source-commit "$SOURCE_COMMIT" \
  --bundle-id "$BUNDLE_ID" --image-sha256 "$BASE_IMAGE_SHA256" \
  --target-id "$TARGET_ID" --expected-boot "$BOOT_ID" \
  --disk /private/pcxt-com.img --capture-device /dev/v4l/by-id/fes-shadowcast-hil \
  --evidence /private/new-com-evidence
```

For the game, replace `com` with `game`, select the separately built game
floppy in a **new owned session**, and provide its path and a new evidence
directory. Kit B uses `/dev/v4l/by-id/fes-4kpro-hil`. The evidence parent must
already exist and have at least 30 GiB free; the named evidence directory must
not exist. The caller must retain the package, software and launch receipts,
boot captures and its final Stop/restoration evidence alongside the diagnostic
output. The host and software build receipts are copied into the evidence with their
hashes and distinct source commits. The supplied host binary must match its
receipt, and the software receipt must name the selected bundle and base image.
The operator must ensure that binary is the one actually serving the isolated
host API; the API does not attest its executable. The diagnostic also records
hashes of its scripts, parser, BIOS locks, ROM and extracted font. These identify
the diagnostic inputs separately from the package source commit. The local
floppy digest and library selection are checked, but media
ready does not independently hash target SDRAM.

The COM check requires a fresh `FES COM READY (PRESS K)`, sends `k`, and
requires `FES COM PASS` followed by a later `A:\>` prompt using the locked BIOS
font. The game check records a continuous lossless video and host-side input
and capture timings. A completed video is **review pending**: an independent
reviewer must identify the title, playfield and response of the player to input,
not merely animation. Timestamps do not prove when the target applied a key.
The game runs in quiet mode and the floppy is read-only, so the diagnostic does
not establish audio, disk writes or score persistence. Neither diagnostic is
whole-board timing qualification or a claim of general XT compatibility.
