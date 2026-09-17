# Catalog core media

Schema 7 stores immutable media bytes in SQLite, addressed by lowercase SHA-256.
The public comparable values expose IDs and metadata, never filesystem paths:

- CoreEntry adds MediaID and MediaRole, both omitted from JSON when empty.
- CoreMedia contains MediaID and Size.
- ImportCoreMedia(ctx, data) returns (CoreMedia, created, error); sizes are
  1..protocol.MaxDevelopmentMediaBytes. Reimport verifies an existing object.
- CoreMedia(ctx, id) returns (CoreMedia, copiedBytes, error), rechecking size
  and digest on every read.
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

Favorites and play history are owned by libraryuser and keyed by stable GameID.
Catalog migration and selection preserve that identity. Service, API, CLI, and
runtime media delivery are separate consumers of these catalog interfaces.
