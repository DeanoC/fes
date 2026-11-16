# Native FAT allocation and new package files (software only)

`fat.CreateFile` and `fat.Mkdir` create new files/directories under an existing
parent, allocate free clusters and extend directory chains. The path supports
long UTF-16 filenames with unique DOS aliases, mirrored or explicitly active
FATs, reserved high-bit preservation and primary FSInfo hint invalidation.
Mutation first marks the volume dirty. New data and FAT links flush before
publishing the directory entry; an originally clean flag returns only after
that directory flush. No implicit overwrite, rename, removal or repair is added.

Cancellation and IO uncertainty after mutation retire the mount without replay,
rollback or reclaim. Known pre-mutation refusals leave it usable. This is
clean-restart persistence, not power-loss atomicity. Physical SD writes remain
explicitly disabled in Nano startup; no hardware, card or kit state is touched.

Tests cover real mtools-formatted images and independent file readback, MBR
offsets, empty/fragmented files, growing directories, name/alias collisions,
Unicode, selected FAT copies, stale FSInfo hints, no-space and damaged metadata,
cancellation, and lost replies at every tested write/flush boundary. A freshly
created canonical FES archive passes the existing validator/store and is consumed
from private RAM after modeled SD containment. This is content/storage evidence,
not live FPGA programming or ordinary appliance admission.

The initial Unicode interoperability failure is retained. A separate control
shows mtools 4.0.48 cannot read an emoji filename created by mcopy itself; its
matching GNU source truncates wide characters and does not combine surrogate
pairs. BMP Unicode retains independent full-name readback. Supplementary names
have exact independent UTF-16-pair assertions, native-name remount checks and
independent data readback by the standard short alias. The driver is not changed
to imitate the tool's truncation. The original control image, source archive,
source hashes and Microsoft format reference are retained locally.

All seven selected checks pass, together with the additional actual C/RTL native
launch/return integration suite. The final test-only follow-up uses a real BPB
reserved-flag mutation and eight-sector clusters with directory growth; the
SD/SDHCI/FAT/store race check passes again on those final test bytes. Production
sources remain unchanged across these checks. The changed-source digest record,
all original failures, independent mtools control and passing results remain in
`out/nano-fat-create-20261109/validation.json` and adjacent files.

The full native ARM64 launcher cross-build passes from committed source
`24d5c40f07356f13f23d20834a237e6295ef5dcf`, based on `afc842810dbd1848c706464a524165635c9aecde`. All
2115 recorded compiler input hashes, including `fat/create.go`, still
match. ELF SHA-256: `a1d80a661abb484697a6b92826c76ae63da2047359181e4cca5985453958ccbb`.
The board still explicitly selects read-only SD and volatile preference changes.
No new entry/BL31/FIT/SOF was assembled or deployed. Cross-compilation does not
establish native execution or hardware acceptance of the new allocation path.
The owned native installer/upload integration, hardware write/readback and
power-loss behavior remain open. The overall DE10-equivalent core-loading goal
is incomplete. Physical keyboard/HDMI testing remains deferred until the
operator returns.
