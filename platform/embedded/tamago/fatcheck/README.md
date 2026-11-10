# Native FAT file diagnostic

`fatcheck.Run` joins FAT allocation and existing-file updates to an explicitly
owned writable SD session. The separate Nano `sdfile` command selects it with
`--sd-write-plan FILE` and the closed SD-only profile. QEMU rejects this command;
host tests inject the actual C firmware dispatcher and Go PIO/card model.
Ordinary native storage and core-loading images remain read-only.

The compiled plan binds the expected CID, sector count, MBR, recovery manifest
and root recovery FIT, plus a unique `FES/SDP-<16 lower-case hex digits>.JSN`
target. The target must be absent. Before mutation, the diagnostic verifies the
protected files and the identical primary/backup FAT boot sectors. Writes are
confined to the sole FAT32 partition and cannot touch either boot sector.
This requires an independently reviewed spare-card layout and exclusive lease;
the plan is not card authentication or general installation authority.

The command creates 4096 deterministic JSON bytes, reads them, updates the file
to 1024 bytes and reads it again. It contains and resumes SD through the same
firmware owner, freshly mounts the volume, rechecks file contents and protected
files/boot metadata, then ends contained. The original allocated chain remains
available after the shrinking update. Each of six trace records binds card,
path, size, digest, owner and generation. The explicitly selected native trace
decoder requires the complete cold-boot proof and exact phase order.

An error stops the diagnostic without retry, rollback, file deletion or an
automatic firmware stop. The caller retains unresolved ownership. Success
consumes the absent-target precondition; a future trial needs a fresh reviewed
target. Independent recovery-boot file readback is a separate gate.

Tests include real FAT images and independent mtools readback, card/MBR/file
refusals, existing targets, cancellation, uncertain create/update writes,
firmware stop failures, changed cards on resume, lost resume replies and final
stop failures. Decoder interoperability consumes actual model records and uses
an explicitly synthetic cold prefix. These are software checks, not physical
FAT persistence, power-loss qualification or ordinary package installation.

From the TamaGo module, run:

```sh
go test -race -count=1 -timeout 90s ./sdcontrol ./fatcheck
```
