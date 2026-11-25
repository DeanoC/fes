# Embedded FAT32 storage

`Mount` reads FAT32 superfloppies and one FAT32 MBR partition using 512-byte
sectors. Files are bounded at 64 MiB and directories at 4 MiB. `WriteFile`
updates an existing preallocated regular file without reallocating its chain;
it flushes file data before updating and flushing the directory size.
`ReplaceFile(ctx, name, data)` updates any existing regular file (or creates it
if absent) via copy-on-write cluster allocation and a single-sector directory
entry commit before freeing the previous clusters, ensuring a power cut before
the directory commit leaves the previous file intact rather than half-written.
`RemoveFile(ctx, name)` and `RemoveDir(ctx, name)` delete an existing regular
file or empty directory by committing the `0xE5` deleted marker on the 8.3 short
entry first, invalidating open handles with `ErrReplaced`, wiping any preceding
LFN slots, and only then reclaiming the target's clusters across mirrored FAT
tables. `RemoveDir` rejects non-empty directories with `ErrNotEmpty`.
`Rename(ctx, oldName, newName)` renames or moves files and directories across
the volume without copying data clusters, commits the new directory entry (and
updates moved directory `..` links) before unlinking the old entry with `0xE5`,
and invalidates open handles with `ErrReplaced`.
`MkdirAll(ctx, name)` validates the full path, verifies free clusters up front,
and creates missing intermediate directories using the same fail-closed `Mkdir`
rules. `StatFS(ctx)` scans the active FAT, FSInfo sector, and root label to
report total/free/used/bad clusters, dirty-bit state, and FSInfo hints without
mutating media.

`CreateFile(ctx, name, data)` and `Mkdir(ctx, name)` explicitly allocate new
entries under an existing parent. They reject existing names and short aliases,
use long UTF-16 names with unique DOS aliases, grow directory chains when
needed, and append records after the directory's logical end. Deleted slots
are not reused. The FAT's active-table/mirroring flags select the tables to
update; reserved high bits remain intact. Affected entries in every selected
copy are checked before mutation and reread before each update.

Creation marks the volume dirty, invalidates the primary FSInfo free-space
hints, initializes and flushes new data/directory clusters, writes and flushes
FAT chains, then publishes and flushes the directory entry. Only then can an
originally clean volume regain its original clean flag. An originally dirty
volume stays dirty. No clock source is assumed by default (new entries use
1980-01-01 unless callers configure an optional UTC wall clock via `SetClock`).
Planning and mutation check cancellation and cooperate with native scheduling.

Success is a clean-restart durability boundary. Cancellation or an uncertain
read/write/flush after mutation retires the mount with `ErrMutation`: new opens,
existing-file reads and further writes refuse without retry or rollback.
Existing data, allocated clusters and DMA ownership are not reclaimed by that
error. Power loss can leave orphan allocation or incomplete metadata requiring
external repair. A known read-only refusal before mutation leaves the mount
usable. Overwrite-by-creation, overwrite-on-rename, formatting and repair are
absent.

These APIs supply neither a storage lease nor package/FPGA admission. The caller
must own and serialize storage access. Nano builds still select read-only SD;
the core installer and physical write qualification are not enabled implicitly.
Tests cover real FAT images, MBR offsets, empty/fragmented files, directory
growth, alias collisions, Unicode, mirrored/active tables, space/metadata
refusals, cancellation and lost replies at every tested write/flush boundary.
A canonical FES archive created on a fresh image passes independent readback,
normal package validation and RAM consumption after modeled SD containment.

The format reference is Microsoft's [FAT specification 1.03](https://www.cs.fsu.edu/~cop4610t/assignments/project3/spec/fatspec.pdf).
Independent readback uses GNU mtools. Version 4.0.48 does not handle UTF-16
surrogate pairs, including files it creates itself; supplementary characters
therefore have separate raw-pair, native-name and independent DOS-alias checks.
The source/control evidence is retained with this milestone's validation.
