# Catalog core media

Schema 8 stores immutable media bytes in SQLite, addressed by lowercase SHA-256.
The public comparable values expose IDs and metadata, never filesystem paths:

- CoreEntry adds MediaID and MediaRole, both omitted from JSON when empty.
- CoreMedia contains MediaID and Size.
- MaxCoreMediaBytes is protocol.MaxContentBytes (32 MiB), independent of target
  development-media limits. CoreMediaChunkBytes is 64 KiB.
- ImportCoreMediaStream(ctx, size, body) returns (CoreMedia, created, error).
  It validates an exact 1..32 MiB body into a private 0600 temporary file while
  hashing, before acquiring a database writer. Publication of metadata and
  chunks is atomic. Reimport verifies an existing object without overwriting it.
  Cleanup errors are returned even after commit; metadata and created still
  reflect the committed result.
- CoreMediaInfo(ctx, id) returns (CoreMedia, error), verifying all bytes into
  io.Discard under a read transaction.
- OpenCoreMedia(ctx, id) returns (CoreMedia, io.ReadCloser, error). It verifies
  a private file snapshot and releases the database transaction before returning
  the reader positioned at zero. Close removes the snapshot and is idempotent.
- These streaming operations use O(chunk) memory. ImportCoreMedia(ctx, data)
  and CoreMedia(ctx, id) remain allocating compatibility/test helpers.
- CreateCoreMediaEntry(ctx, title, coreID, packageID, mediaRole, mediaID)
  returns (CoreEntry, error). A selection is either ("blob", digest) or ("", "").
  Referenced bytes must exist and pass integrity validation in the transaction.
- SelectCoreEntryMedia(ctx, gameID, expectedPackageID, expectedMediaID,
  mediaRole, mediaID) returns (CoreEntry, error). Both expected IDs must match;
  an empty replacement pair clears media. The package and game identity remain.

CreateCoreEntry delegates with empty media. SelectCoreEntry changes only the
package, retaining media. Distinct titles for one core coexist; duplicate
core/title pairs conflict. Titles are trimmed as before. New game rows use their
GameID as relative_path. New IDs hash the core ID and exact trimmed title,
so titles with identical display slugs remain distinct. Existing rows and
GameIDs are not rewritten.

Malformed media, invalid role/digest pairs, and corrupt stored bytes return
ErrInvalidCoreMedia. Missing media returns ErrCoreMediaNotFound. Selection uses
the existing ErrCoreEntryNotFound and ErrCoreEntryConflict sentinels.

The schema 7 migration copies existing selections and imports the historical
Coleco diagnostic from seeds/coleco-controllers.hex with its adjacent MIT license.
Only entries already present with core ID fes.coleco receive that selection.
The seed is ordinary stored media after migration. New entries receive no
implicit media, and reopening never restores a cleared selection.

Schema 8 leaves legacy inline blobs intact and limits their actual length to
the historical 16 KiB contract. New rows have empty inline data and ordered
core_media_chunks rows. Reads reject mixed representations, missing or reordered
chunks, incorrect full-chunk/tail lengths, invalid total size, and digest
mismatch. SQL CASE expressions bound each blob before it reaches the driver.
Entry creation and media selection use the streaming verifier in their existing
transactions. Failed imports roll back all rows and close/remove temporary files.

Favorites and play history are owned by libraryuser and keyed by stable GameID.
Catalog migration and selection preserve that identity. Service, API, CLI, and
runtime media delivery are separate consumers of these catalog interfaces.
