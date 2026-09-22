# ZX81 expansion bus

The registered expansion bus is part of the standard ZX81 shell. Expansion
carts are launch-time composition (linked before programming). Mid-session
`.p` tape select/load is a separate product path — see
[ZX81 tape media](zx81-tape-media.md). Do not conflate bus carts with tape.

A separately synthesized RAM cart is retained only as a validation consumer
for this bus; the shell interface is the bus itself. The socketed `fes.zx81`
1.2 package has 1 KiB of mirrored RAM when the edge is
vacant, and a registered Z80-like expansion bus (A, D, /MREQ /IORQ /RD /WR
/M1 /RFSH in; D, ROMCS, WAIT, RAM_PRESENT, DSEL out). The validation cart decodes the physical `4000–7FFF` window on that bus. Zon X-81 and QS Character
Board RTL uses the same plugs; they are not library assets yet. Cart cells
keep a distinct `FPGA_CLK1_50` clock port so `--fes-slot-clock clk_sys` can
splice the inferred IB onto the shell 52 MHz net. Zon X returns a digital channel-A square on `peek_d`
and the shell mixes it into HDMI I2S0 when the validation cart is absent. During ULA
`/RFSH` the shell presents `{6'h21, char[6:0], row[2:0]}` on the edge so QS
`8400–87FF` can supply glyphs. That window power-up copies Sinclair glyphs
0–63 from ROM `1E00–1FFF` so the board boots with readable text; `POKE`
still replaces rows. The shifter keeps the first socket `ROMCS`
byte in `rfsh_chr`. `/RFSH` is not muxed into `cpu_din`. The normal library launch composes a frozen shell and a selected bus cart;
it does no synthesis, placement or routing. An unset selection loads the original sealed
shell.

The CRAM map is `fes.zx81-bus.socket/1` (slot `fes.expansion.zx81-bus`
1.0). This names the bus contract and does not constrain future cart types. The plug packing itself is
the Z80-like edge, not the earlier pre-decoded 14-bit RAM port. Old RAM-port
carts cannot overlay a bus shell: shell hashes differ. The standard `fes.zx81`
producer now builds this socketed shell with the scoped
`sources/misteross/toolchains/zx81-expansion.lock`; `--legacy` is reserved for
diagnostic builds of the pre-expansion shell.

## Producer

Build the scoped compiler with `make -C sources/misteross toolchain-fes-zx81`.
Build the standard shell with `python3 scripts/build_fes_zx81_oss.py --gpu-devices N`
from `sources/misteross`. Keep its sealed package and `build/fes-zx81-oss`
frozen routing evidence together. A shell is built once for any number of
independently built compatible carts.

Build the RAM validation cart with:

```
python3 scripts/build_zx81_bus_validation_cart.py \
  --shell build/fes-zx81-oss --package build/packages/SHELL_PACKAGE_ID --gpu N
```

The result is a two-member validation archive: canonical `manifest.json` and `cart.rbf`.
The manifest binds the exact shell package, base BUILD_ID and RBF hash, cart
hash and size, source revision, recipe hash, device and fixed socket geometry.
The cart producer records and uses placement seed 2 explicitly; retries use
the same deterministic recipe. It rejects logged compiler errors even when
the process exits zero, and clears old outputs before each attempt.
It checks timing and rejects any non-CRC change outside the
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
A missing or incompatible selected bus cart blocks admission before any recovery
Stop or programming; it never silently falls back to the empty machine.
Changing the base package does not erase the selection: the title remains
unready until a compatible bus cart is selected or the choice is explicitly cleared.

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

The [2026-09-21 exact-artifact record](validation/2026-09-21-zx81-ram-composition.md)
records five normal-library launches, visible 1 KiB/16 KiB validation-cart sizing,
selection retention/clearing, negative admission and original-kit restoration.

`make -C sources/misteross sim-fes-zx81-expansion` boots the actual ZX81 machine
with and without the validation cart and checks RAMTOP and visible output. The standalone
Go module under `sources/misteross/expansion` tests exact Python golden bytes,
framing/CRC failures, outside-slot changes and canonical asset admission.
FogCast uses a compact retained synthetic RBF golden for always-run staging
and restart tests. It also tests catalog persistence/CAS, lease enforcement, composed response
identity, lost-response handling and staging ownership. Set
`FES_EXPANSION_SHELL` to a produced sealed shell package when running
`go test ./corepackage -run TestComposition -v` to exercise complete staging,
restart adoption, and cleanup against real RBF bytes without hardware.

These checks are host evidence. Exact empty-shell and composed-cart hardware
acceptance must separately verify the selected validation cart, composition tuple,
RAMTOP, keyboard/tape behavior and clean Stop/reload on the designated kit.

## Designated-kit acceptance recipe

Use the integrated runtime/agent and one private host catalog. Root is the kit
operator; claim its ordinary lease before programming. Retain the initial
system/active-package state and restore it after the run. Record all package,
asset, composition and image identities in the acceptance evidence.

1. Import the exact sealed socket package. Create one entry using
   `POST /api/v1/library/core-entries` with `title` and `package_id`. Keep the
   returned `game_id`; its media and expansion selections are initially empty.
2. Launch with the ordinary `POST /api/v1/session/launch` and that `game_id`.
   Wait for BASIC, attach the existing session keyboard input, and capture the
   display. Require the base package/BUILD_ID and no composition in status.
3. Enter `PRINT PEEK 16389` using the matrix sequence below. The display must
   show **68**. This is the high byte of RAMTOP at 16388/16389, hence 0x4400
   and 1 KiB available from 0x4000. The RAMTOP system-variable address is from
   [the original ZX81 manual, chapter 28](https://worldofspectrum.net/ZX81BasicProgramming/chap28.html).
4. Stop through the host session API. Import the independently built cart at
   `POST /api/v1/core-expansions`. PUT the entry's expansion selection with
   exact `package_id`, empty `expected_expansion_id`, and returned `expansion_id`.
   Launch the same `game_id` normally. Require the same base package/BUILD_ID,
   the exact expected composition tuple, and a new generation. Repeat the
   keyboard expression: the display must now show **128**, RAMTOP 0x8000.
5. Stop, restart the private host with its existing catalog, and launch the
   same entry again. Confirm the selected expansion survived and repeat the
   identity/display checks. Stop, explicitly clear the selection using its
   current ID as `expected_expansion_id`, and relaunch: require no composition
   and **68** again.
6. With a valid owner retained, attempt selection of a missing expansion ID
   and import of an asset bound to an unavailable shell. Both must reject before programming;
   retain the same active tuple/generation and running display. A host unit
   regression separately injects a missing/mismatched already-selected asset
   while recovery is pending and requires zero Stop/load calls. Do not edit
   the kit's catalog or immutable package files to manufacture these failures.
7. Stop/relaunch once more and restore the original kit setup. Save request
   outcomes, identity snapshots and display frames; successful simulation or
   a direct runtime composed load is not a substitute for this library test.

Keyboard sequence, verified against the actual BASIC/ULA simulation with both
memory sizes: `P`, `Shift+Enter`, `O`, `1`, `6`, `3`, `8`, `9`, `Enter`.
`P` inserts PRINT in keyword mode. Shift+Enter enters function mode and O inserts
PEEK. Send each press/release through the existing session input attachment;
start with roughly 200 ms down and 200 ms released per key and inspect the
resulting line before Enter. Hold both Shift and Enter during their chord and
release both before O. Allow BASIC's initial RAM/display setup to finish before
typing (the 16 KiB simulation needed a longer initial wait than 1 KiB).

FogCast matrix event codes are P=275, O=274, Shift=256, Enter=257 and digit n=
286+n. They are already handled by `internal/zx81keys`; this test adds no
hardware diagnostic command or private RAM peek interface.
