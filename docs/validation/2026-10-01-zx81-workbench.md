# Zx81 workbench software validation

Issue [#366](https://github.com/DeanoC/fes/issues/366) adds installed-package
setup, compatible expansion import, progress presentation and cassettes chosen
before launch. Base: `750254a8b1ebdd408a3d32a1d0518c9c4866e7af`.
Implementation: `a2495d1f37c991859362ed709fd2f03880c2ef7b`, with a subsequent
presentation adjustment keeping progress marks visible on both shelf and
inspection. The [workbench guide](../hardware-rooms.md) describes operation.

## Checked behavior

- Installed Zx81 setup reads the sealed ROM requirement through local APIs,
  creates the library entry and binds its explicitly selected ROM without a
  published catalogue. Retrying finishes an empty binding; it rejects a
  different existing ROM or conflicting title selections.
- Opening the cassette shelf imports nothing. Selecting a default or custom
  cassette uses the existing media store and compares the exact package and
  previous media ID before saving the next launch. Selection does not launch,
  stop or mutate a running session.
- RAM, Zon X and QS are supported import categories. Zon X and QS carry
  presentation progress marks. Immutable archive identity and existing shell
  admission remain authoritative; import does not fit the card.
- Catalogue migration 15 → 16 retains existing expansion selections and
  descriptions. The progress flag defaults to false, survives restart and is
  preserved by older presentation edits that omit it.
- The kit's menu-display disables live room/tape routes. The separate host
  display retains its existing live session path. HDMI overlay/second-display
  work and tape swapping while the kit core runs are deferred.

The starter shelf includes Guess the Number (original MIT homebrew, 1 KiB),
Aritm (GPL-3.0-or-later, 16 KiB recommended) and Character Display (MIT, 16 KiB
recommended). Complete source, licence and checksum records are in
[`internal/zx81tapes`](../../sources/FogCast/internal/zx81tapes/README.md).

## Software evidence

Focused catalog, hostclient, host API, service, room and tenfoot tests passed.
Race checks passed for the affected Go packages. The migration fixture
reconstructs schema 15 with saved records before opening it under schema 16.
The parent regression suite passed 605 tests with 39 documented/environmental
skips. `go vet`, producer unit tests, tape checksums, whitespace checks and
committed-source `make check` passed. A read-only agent review found no blocking
correctness defects in the setup, media, expansion or asynchronous overlay paths.

`make host` built the committed implementation for linux/amd64 and emitted
`out/native-integration-dev/host.json`. Its API SHA-256 is
`d23fbb72c0ba2bd5a3fd27d0d21be25937f7cd585064ac38e5014302a03f21b8`.
The ARMv7 tenfoot launcher also compiled without SDL/CGo. Local logs and binaries
are retained under ignored `out/zx81-room/`. These are software build results,
not hardware acceptance.

## Hardware classification and next integration

No hardware programming, shared-service restart, deployment or installed-image
change was performed. Read-only inspection found kit 1 available and the normal
Powerboat `fogcast-api.service` active. Its published-core catalogue was disabled,
which motivated the installed-package setup path. Coordination to stop that
service temporarily for an isolated diagnostic was requested; it remains
pending. The kit-sharing guide prohibits a duplicate host against the same kit.

Next, coordinate the shared host, claim kit 1 through the existing target lease,
and validate the workbench with the exact selected package, private BASIC ROM
and starter cassette. Observe LOAD/RUN and controller navigation on HDMI, then
Stop, release and restore the host. Expanded-RAM tapes require a matching RAM
archive. Zon X is being developed separately. This change modifies only the
producer's visible package name to `Zx81`; renamed package acceptance requires
a freshly sealed artifact. Historical shell or image acceptance does not
qualify that artifact. No runtime, FPGA behavior or shared ABI definition changed.
