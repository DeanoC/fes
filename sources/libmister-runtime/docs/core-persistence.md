# Described-core persistence

Library activation can persist the exact `fes.persistence.words` 1.0 and
`fes.pong.progress` 1.0 contracts. Both interfaces must be required together.
The runtime registry admits one known layout: two 16-bit words containing
paddle speed (0 slow, 1 normal, 2 fast) and best rally (0–65535). Missing data
starts with `[1,0]`. Existing volatile packages remain supported.

This is software-tested persistence of settings and progress, not a game save
state. Exact-image physical acceptance is pending. Ordinary SNES battery SRAM
keeps its existing `.srm` identity, sizes, transport, and file format.

## Local protocol 2

The target agent derives and creates the trusted absolute `data_root`; the
runtime traverses it without following symlinks. Network callers never supply
this path. All package requests retain the existing `package_path` and
`package_id` spellings and require admission of the exact staged package bytes.
The local requests are:

```json
{"protocol":2,"operation":"load_library_core","package_path":"/tmp/fogcast-development/core-packages/example","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","data_root":"/media/fat/fogcast/core-data"}
{"protocol":2,"operation":"inspect_core_data","package_path":"/tmp/fogcast-development/core-packages/example","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","data_root":"/media/fat/fogcast/core-data"}
{"protocol":2,"operation":"update_core_settings","package_path":"/tmp/fogcast-development/core-packages/example","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","data_root":"/media/fat/fogcast/core-data","expected_revision":"absent","paddle_speed":2}
```

The package IDs above illustrate the syntax; they are not installable artifacts.
`Runtime::LoadLibraryCore`, `InspectCoreData` and `UpdateCoreSettings` expose the
same operations to native callers. Data operations serialize with lifecycle
transitions and reject pending recovery. Inspection does not program the FPGA
or open input. Settings updates reject an active package with the same core ID,
including a volatile development session, and preserve current best rally.

Successful data inspection and settings update add this object to the ordinary
response envelope:

```json
{"core_data":{"package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","core_id":"fes.pong","layout":{"id":"fes.pong.progress","major":1,"minor":0},"mode":"persistent","revision":"absent","paddle_speed":1,"best_rally":0}}
```

`revision` is `absent` or the lowercase SHA-256 of the complete canonical record.
GET-style inspection returns durable data, never a sampled live score. Updating
requires an exact `expected_revision`; absent and existing records participate
in the same compare-and-swap boundary. A successful write returns its new
revision. Status responses do not add `core_data`; an active package instead
has `persistence_mode: "persistent"` or `"volatile"`.

`inspect_core` also returns derived `inspected_package.persistence_layout`,
either `{id,major,minor}` or null. `inspect_core_data` accepts a compatible
volatile package when its namespace has no record and no active persistent
generation: mode is volatile, layout is null, revision is absent. Existing data
or an active persistent generation makes a persistence-removing candidate
incompatible, even before the first save. A volatile package has no settings
update operation.

Development `load_core` is always volatile, even for a persistence-capable
package. It neither restores nor writes library records. Explicit library
loading repeats authoritative compatibility and record validation at launch.

## Record and lifecycle boundary

The runtime creates a directory named SHA-256 of `core.id` below `data_root` and
retains its directory descriptor. The fixed filename is `record.bin`, independent
of package identity or layout version. The codec implements the generated
shared `FESDATA1` fixtures: core-ID digest, layout ID/version, bounded payload,
and SHA-256 checksum, little-endian integers, no padding or trailing bytes,
maximum 688 bytes. Layout and versions must match exactly. Corrupt or
incompatible records are never replaced by defaults.

Library preflight validates record bytes and probes write/sync access with a
private temporary file before retiring input. Read-only inspection skips the
write probe. Replacement flushes the outgoing generation, then reopens and
validates the incoming record under runtime serialization. Thus two versions
of one core restore the newly flushed data rather than the preflight snapshot.
After programming, live ABI/build/capability identity and data-info fields must
match before restore. Restore stages both words and commits them atomically
while reset remains held, before gameplay release and input start.

Stop retires input, freezes gameplay without reset, reads a complete snapshot,
and publishes only complete canonical bytes using private temporary creation,
complete writes, file sync, atomic rename and directory sync. Namespace locks
serialize file operations and compare-and-swap. A failure before rename leaves
the old record; a directory-sync failure after rename reports uncertain
durability and permits only an old or complete new record. Retry rereads and
validates the current file. Successful Stop is the durability boundary; startup,
fault cleanup and failed activation do not save.

When publication fails, explicit GP resume must succeed before the same input
generation is reopened. Only after input has started does the runtime discard
the old snapshot, so retry captures fresh progress. A complete snapshot remains
retained when resume is unsafe. An ambiguous GP timeout poisons the exchange;
there is no blind commit/resume retry or reset to hide failure.

## Errors and recovery status

| Code | Meaning |
| --- | --- |
| `unsupported_interface` | Missing, unsupported, or multiple persistence layouts; volatile settings update |
| `invalid_request` | Invalid path, package-ID syntax, revision syntax, or noninteger/out-of-range speed |
| `invalid_package` | Exact admitted bytes or expected package identity are invalid |
| `corrupt_data` | Invalid envelope, length, checksum, enum, or nonregular record |
| `incompatible_data` | Existing identity/layout/version mismatch, or persistence-removing candidate |
| `stale_revision` | Compare-and-swap revision changed |
| `busy` | Lifecycle operation, active namespace write, or recovery prevents access |
| `save_failed` | Storage access/publication failure; successful Stop was not reached |
| `idle_failed`, phase `recovery` | Gameplay/input cannot safely resume after a save failure |

Storage diagnostics do not disclose server paths. Package/identity errors retain
the existing safe expected/observed evidence.

A safely resumed failed Stop/replacement has `save_failed`, phase `save`, the
same running state, package, and generation, and restored native input. Unsafe
persistence recovery reports `reboot_required` with phase `recovery`, retaining
execution `development`, core, active package, active interfaces, generation,
and persistence mode for honest ownership. It does not report idle, program
Menu, or allow a settings write. Other existing recovery shapes are unchanged.
The target must retain its lease/session and require explicit recovery.

The byte-exact response corpus is
`tests/fixtures/protocol-v2-persistence-responses.jsonl`: absent durable
inspection, running persistent, safe failed-save resume, retained unsafe
recovery, and corrupt-data/stale-revision errors. Protocol 1 retains its existing request and response shape; retained persistent
recovery is projected to its legacy reboot-required execution-none/null-core
shape, while protocol 2 preserves ownership metadata.
