# Durable kit core library

The target agent can install described core packages on existing FESDATA3 storage
without rebuilding the appliance image, restarting services, or activating the
FPGA. Installation retains immutable versions. Explicit selection controls the
next kit-local launch; a running core keeps its package and execution lease.

The library requires the existing writable ext4 FESDATA3 volume. Startup
resolves that volume the same way appliance-data preparation does:
`/dev/disk/by-label/FESDATA3` when the label is present, including a labelled
USB partition, and otherwise a labelled `/dev/mmcblk0p3`. It lives at
`/run/fogcast/fesdata3/core-library` inside that mounted volume, not on tmpfs.
Startup positively checks the mount, writable ext4, exact mount identity, and
that selected device before creating anything. Absent storage or an appliance
trial, pending update, or corrupt update record leaves the API unavailable.
The agent does not format, repartition, or fall back to FAT.
After confirming a trial image, restart the agent under owned maintenance to
open a library that was unavailable at startup.

## Install and select

All routes use the target agent's bearer credential. Package identities are the
existing sealed package IDs; ABI, board/profile and required-interface support
remain the native runtime's authority. No execution lease is needed for these
storage operations. Appliance update admission excludes writes during a trial
or image transition.

| Route | Effect |
| --- | --- |
| `GET /v1/core-packages` | Durable package descriptors and writable selections |
| `POST /v1/core-packages` | Install a bounded `application/octet-stream` package archive |
| `GET /v1/core-selections` | Read writable selections |
| `PUT /v1/core-selections/{core_id}` | Select an installed compatible package using an expected prior ID |

The PUT body is
`{"expected_package_id":"<prior writable selection>","package_id":"<installed ID>"}`.
Use an empty expected ID when there is no writable selection, including when the
factory version is still selected. A competing selection returns HTTP 409
`CORE_SELECTION_CONFLICT`; refresh rather than overwrite the other agent's
choice. The package must identify the requested core and pass read-only native
runtime inspection. That inspection does not activate hardware.

For example, with `KIT_API` and `KIT_TOKEN` supplied from the authorized target
configuration:

```sh
curl --fail-with-body -H "Authorization: Bearer $KIT_TOKEN" "$KIT_API/v1/core-packages"
curl --fail-with-body -H "Authorization: Bearer $KIT_TOKEN" \
  -H 'Content-Type: application/octet-stream' --data-binary @core.fcore \
  "$KIT_API/v1/core-packages"
curl --fail-with-body -X PUT -H "Authorization: Bearer $KIT_TOKEN" \
  -H 'Content-Type: application/json' \
  --data '{"expected_package_id":"","package_id":"<installed ID>"}' \
  "$KIT_API/v1/core-selections/fes.pong"
```

Installation returns the exact descriptor and ID, plus `created`. Reinstalling
identical bytes retains the same ID and does not select them. The Go target
client exposes `InstalledCorePackages`, `InstallCorePackage`, `CoreSelections`
and `SelectCorePackage`; mutations are not automatically replayed after a lost
reply. Reconcile through inventory first. An interrupted unselected install is
completed from its verified retained archive when the store is reopened.

## Launch and recovery

The kit-local inventory combines factory selections with writable selections,
overriding only the same core ID. Unselected installs do not add tiles. The
factory inventory and earlier installed versions are retained. There is no
package deletion API and no implicit rollback. Downgrade by selecting a retained
compatible version with the expected current selection ID.

Launch requirements come from the verified manifest. Firmware, multiple ROMs,
native-pixel shells requiring video parts and unsupported local media paths
remain blocked. The current local cartridge path supports one format-3
cartridge map. When the durable store is open, `GET /v1/local/cores` overlays
writable selections on the factory catalog. The factory pass reads manifests,
streams payload and ROM-map digests, and structurally validates each ROM map
while streaming it. A map that full inspection would reject is omitted. The
factory catalog is cached until its stat fingerprint changes: selection and
member size, mtime, inode, and directory entries. Opening a selection file
does not change that key. A disconnected client ends the handler wait. One
scan runs at a time; if the tree changes during that scan, the next request
waits and then scans once more.

A selected durable package is listed from the verification recorded at
install. Listing keeps that result while the sealed archive and member
fingerprint still matches: size, mtime, inode, mode, and directory entries.
Repeated lists do not read payload or ROM-map bytes. A changed fingerprint
verifies the bytes again. Launch and path lookup still inspect the retained
bytes in full. Missing or corrupt selected bytes then block local launch,
including after restart, and do not silently substitute a factory version.
Native runtime inspection checks the exact canonical bytes before activation
through its existing trusted staging path. Every physical launch and stop
still uses the execution lease; local launches also participate in appliance
update admission and draining.

Bearer routes report a broken library as HTTP 503
`CORE_PACKAGE_STORE_UNAVAILABLE`. Kit-local clients call `GET /v1/local/cores`
on the root-only control socket. That route returns HTTP 200 and an empty list
when the durable library cannot be read, including when FESDATA3 is absent, and
it does not read factory package bytes. Local launch stays on that empty
inventory. An empty local list is that socket's response for this failure.
Mesh inventory advertises durable packages from the same stat-keyed
verification. A changed archive or member stat verifies the bytes again.

Package persistence is separate from settings and progress persistence. Kit-local
execution still uses its existing volatile core-data mode. Persistent settings,
ROMs, expansion/video parts and title-specific media follow their own contracts.
Software tests cover concurrent publication, selection conflicts, process
restart, interrupted installation, update exclusion and activation boundaries.
They do not establish card power-loss behavior or exact-artifact hardware
acceptance. See [core persistence](core-persistence.md),
[core packages](core-packages.md) and [kit sharing](kit-sharing.md).
