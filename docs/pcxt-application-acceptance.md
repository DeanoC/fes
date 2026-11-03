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
# Use a new private directory, never a previously populated Python cache.
PY_CACHE_DIR="$(mktemp -d "${TMPDIR:-/tmp}/pcxt-app-pycache.XXXXXXXX")"
python3 -I -B -X pycache_prefix="$PY_CACHE_DIR" scripts/pcxt_dos_application_acceptance.py com --execute \
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

Remove the private cache directory after the diagnostic exits. Each invocation
must use a freshly created directory. The isolated interpreter avoids stale
bytecode from a previous checkout, including in the nested package parser.

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

Each active-session guard now runs its read-only observer in a forked child with
a hard five-second deadline, including the synchronous `/api/v1/health` request
to the target. This behavior applies to both the COM and Digger diagnostics. A
temporarily absent or blank `selected` bundle value causes a fresh guard
observation to retry until the expected bundle appears or the deadline expires;
the active bundle and base identity are still checked on each pass, and a
nonempty wrong selection fails immediately. The Digger guard can fork while
its video recorder thread is active, so Python may emit a `DeprecationWarning`
about forking a multithreaded process.

## Writable floppy persistence diagnostic

`scripts/pcxt_floppy_persistence_acceptance.py` checks a writable disk in two
already owned sessions. It is an opt-in diagnostic; the caller still owns the
kit claim, queue lock, input attachment, launch, Stop, restart, restoration and
release. Use a dedicated library entry whose game ID matches
`fpga-pcxt-persist-test-<16 lowercase hex>-<12 lowercase hex>`; the tool refuses
other IDs and requires the base COM image and an initially absent saved revision.
The selected package must advertise both the PC/XT floppy and floppy-write
interfaces. The private loopback host process must be the supplied receipted
binary and own the selected listening socket.

Run `com --execute` with the same identity, package, host, kit and evidence
arguments shown above, substituting the persistence diagnostic script. Add:

```text
--phase write
--host-pid <private-host-pid>
--source-repository <owner/repository>
--source-pr <open-pr-number>
--source-base <expected-base-branch>
--phase-deadline-monotonic <absolute-monotonic-deadline>
```

The source commit supplied with `--source-commit` must be the full head of the
specified open PR, with the expected base repository and base branch; the PR
head may be in a fork. The diagnostic checkout, host, software and package
receipts must all identify that same head. The local diagnostic and its consumed
source inputs must match the bytes committed at that head.
`--phase-deadline-monotonic` is an absolute reading of the local monotonic clock,
not a wall-clock time; it is checked at source verification gates, not enforced
as an overall phase deadline. The Save request has its own bounded timeout.
Each read-only session identity observation has its own five-second deadline.

The write phase reads the known marker, copies it to `FESSAVE.TXT`, and sends
one Save with exact session, target, package, generation and media-unit headers.
On any failed or ambiguous write phase, do not replay Save or assume it did not
occur. The owner must retain the claim if it cannot prove identity and use its
guarded recovery procedure. A successful write receipt says **saved; restart
pending**, not that persistence passed.
Preserve `receipt.json` and its SHA-256, stop the owned session, and start a new
owned session with the same isolated library entry and saved revision. Use a new
private evidence directory and rerun with `--phase verify`, supplying
`--write-receipt <path>` and `--write-receipt-sha256 <sha256>`. The verify
phase requires the same target session coordinator with a different launch
flight and package generation, checks the saved revision, then reads the copied
file after the restart. A coordinator ID can persist across launches; it is not
a per-launch identifier.

The tool records sanitized host-health observations and bounded timing stages
without extra identity requests. A full timing pipe or an evidence write failure
stops the diagnostic. Exit status 74 means observation or evidence safety is
uncertain: the external owner must keep the kit claim and queue lock, keep its
lock-holding process alive if observer cleanup is unverified, and reconcile the
owned session before taking further target action. The stage trace describes the observed run, not the cause
of any earlier timeout. A completed receipt is physical diagnostic evidence,
not whole-board timing qualification or a claim of general DOS compatibility.
