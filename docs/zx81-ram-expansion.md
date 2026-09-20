# ZX81 RAM composition

The optional RAM pack is a separately synthesized and routed FPGA component.
The base `fes.zx81` 1.1 package has 1 KiB of mirrored RAM and one fixed RAM socket.
A selected pack provides the full 16 KiB address window. The normal library
launch composes the frozen shell and the selected pack; it does no synthesis,
placement or routing. An unset selection loads the original sealed shell.

This feature uses the scoped `sources/misteross/toolchains/zx81-expansion.lock`.
It does not change the factory ZX81 producer or its compiler selection.

## Producer

Build the scoped compiler with `make -C sources/misteross toolchain-zx81-expansion`.
Build the shell with `python3 scripts/build_fes_zx81_oss.py --socket --gpu-devices N`
from `sources/misteross`. Keep its sealed package and `build/fes-zx81-socket`
frozen routing evidence together. A shell is built once for any number of
independently built compatible carts.

Build a cart with:

```
python3 scripts/build_zx81_ram_expansion.py \
  --shell build/fes-zx81-socket --package build/packages/SHELL_PACKAGE_ID --gpu N
```

The result is a two-member archive: canonical `manifest.json` and `cart.rbf`.
The manifest binds the exact shell package, base BUILD_ID and RBF hash, cart
hash and size, source revision, recipe hash, device and fixed socket geometry.
The cart producer checks timing and rejects any non-CRC change outside the
fixed socket. The host and target use the Go linker rather than trusting a
producer-generated replacement RBF. Compiler and Python are build tools only.

## Library API

Import the shell through the existing `POST /api/v1/core-packages` API and create
its ordinary core library entry. Import the expansion archive as an
`application/octet-stream` body to `POST /api/v1/core-expansions`. List installed
assets with `GET /api/v1/core-expansions`.

Read or update a title's choice at
`/api/v1/library/core-entries/{game_id}/expansion`. The PUT body is:

```json
{"package_id":"<exact shell package>","expected_expansion_id":"","expansion_id":"<imported expansion>"}
```

The expected value implements compare-and-swap. Set `expansion_id` to an empty
string to return to the 1 KiB machine. Selection persists in the host catalog.
A missing or incompatible selected pack blocks admission before any recovery
Stop or programming; it never silently falls back to the empty machine.
Changing the base package does not erase the selection: the title remains
unready until a compatible pack is selected or the choice is explicitly cleared.

The existing normal library launch API is unchanged. Host launch links the
immutable components before target mutation. `/v1/library/core/compose` is the
internal target transport and uses the same kit lease and update/lifecycle
fences as other programming operations. Its closed archive contains the
original package, expansion asset, canonical composition tuple and linked
bytes. Target staging independently recomputes the bytes and identity.

## Identity and ownership

The package ID and observed on-FPGA BUILD_ID always identify the sealed base
shell. `core_package.composition` separately reports the expansion ID, shell
hash, programmed payload hash/size and composition ID. The composition ID is
SHA256 of `fes-composition-v1`, a NUL, package ID, a NUL, expansion ID, a NUL,
and linked-payload SHA256. The expansion ID is SHA256 of
`fes-expansion-v1`, a NUL, and the exact canonical manifest bytes.

The target keeps the original package sealed. Cart and linked payload occupy
separate private sibling directories beneath the existing trusted package root.
All directories share a private publication token and retain root/inode cleanup
ownership. Restart adoption independently re-links before accepting the stored
composition. Runtime opens no-follow files, checks exact hashes and socket
admission, retains file descriptors and rechecks them before programming.
A lost response can reconcile only the exact composition and new generation.
Stop and ordinary loads clear composition status. ZX81 composition is volatile;
no new settings/save-data policy is inferred from the asset or title name.

## Validation

`make -C sources/misteross sim-fes-zx81-expansion` boots the actual ZX81 machine
with and without the pack and checks RAMTOP and visible output. The standalone
Go module under `sources/misteross/expansion` tests exact Python golden bytes,
framing/CRC failures, outside-slot changes and canonical asset admission.
FogCast uses a compact retained synthetic RBF golden for always-run staging
and restart tests. It also tests catalog persistence/CAS, lease enforcement, composed response
identity, lost-response handling and staging ownership. Set
`FES_EXPANSION_SHELL` to a produced sealed shell package when running
`go test ./corepackage -run TestComposition -v` to exercise complete staging,
restart adoption, and cleanup against real RBF bytes without hardware.

These checks are host evidence. Exact empty-shell and composed-pack hardware
acceptance must separately verify the selected package, composition tuple,
RAMTOP, keyboard/tape behavior and clean Stop/reload on the designated kit.
