# Trusted writable FAT mount

`Plan` is compiled platform configuration, separate from a package or install
source. It binds CID, capacity, MBR and the sizes/hashes of `u-boot.itb` and
`FES/RECOVERY.JSON`. The strict bounded parser rejects unknown/duplicate fields.
It grants no firmware lease, card authentication or FPGA permission.

`Mount` checks the discovered card, sole FAT32 partition and identical primary
and backup boot sectors, then mounts through a block wrapper. Writes cannot
leave that partition or touch either boot sector. Protected files must match
before the mount is returned. `Check` rechecks metadata and protected files
under an active lease. `Remount` checks the same bindings after an explicit
same-card containment/resume. Errors issue no stop, repair, retry or rollback.

The package installer checks this mount before catalogue publication and after
record readback. An integrity error retires the store; a published record is
not presumed rolled back after a later error. The native FAT diagnostic reuses
this implementation. Its earlier physical evidence belongs to its archived
source, not this refactored or installer-enabled image.

The caller owns and serializes storage access. Ordinary startup remains
read-only unless a separately compiled writable plan and qualified 25 MHz
selection are provided. Physical package installation and power-loss recovery
require separate acceptance.
