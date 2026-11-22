# Coleco CPU Expansion Integration Plan

**Prerequisite:** The development-only CPU socket and diagnostic module passed
the authenticated host feasibility gate in FES snapshot `4cfd8381777addc224e0ac18727b712f21d9ae3c`.
The shell used placement seed 1, socket rectangle `24 1 28 11`, and CRAM map
`fes.coleco-bus.socket/1` at `(1769, 32, 2806, 1034)` (half-open). Its RBF
SHA-256 was `f4e29bbe7fbbdff8b3b000f1ff550d5e4671089812459a55987c69712f6e6438`;
the diagnostic archive SHA-256 was
`dd9e41a1637639e0456d0cc3514f132264c2d4f0a12db7900b86bba6d712e330`.
Both builds met system, pixel and audio clock gates. The diagnostic changed
2,763 CRAM bits inside the socket and zero outside. These are development
evidence identities, not selected factory assets or hardware acceptance.

The review found that this first diagnostic's WAIT mask expired before a CPU
could issue a later read. The corrected module in development snapshot
`37d76a39af68cdea0c9dae77e675ccffee32e9a8` holds the delay until the
read, then advances it across CPU enables. Its separately routed archive is
`56f5c4f80914fbc6e5da5b960a37f7dc9d39422de886e01af77121cb6011b506`
(SHA-256 `967ec97e817c1653dd5afe47c7268a9773a38980e1727d2b1304752a724d1405`),
and its linked RBF SHA-256 is
`b47b3ef3ab5a5ab1471e70811e248505e603f65da13dea166893add93b170264`.
It changes 3,388 CRAM bits inside the socket, zero outside, and passes all
three timing gates. CPU/socket simulation observed 96 WAIT ticks. These remain
development-only artifacts; product selection and kit acceptance are pending.

## 1. Admit the Coleco slot in the Go linker

Implemented in this worktree. The Go composition matches the independent
builder's `linked.rbf` byte-for-byte; the ZX81 linker suite remains green.

Own this in `sources/misteross`. Parameterize the existing ZX81 expansion
admission by a closed slot/map policy for optional `fes.expansion.coleco-bus`
1.0 and `fes.coleco-bus.socket/1`. Require the shell package ID, BUILD_ID,
RBF digest, device, archive digest, map, geometry and changed-CRAM bounds to
match. Keep ZX81's accepted bytes and failure behavior unchanged. Test a valid
Coleco link and rejection of a ZX81 asset, wrong shell, wrong version, corrupt
archive and out-of-socket CRAM. Reconstruct the full RBF and compare its digest
with the builder's `linked.rbf` before exposing the policy to consumers.

## 2. Carry an explicit library choice to the target

Implemented in this worktree for the described package contract. The host
imports and binds the exact diagnostic archive to an explicit Coleco library
entry, target staging recomposes it, and runtime admission checks the matching
slot/map and ABI. The private host test launches the selected composition
through a target-shaped staging client, checks the exact routed linked RBF and
media delivery, rejects a wrong-bus choice and a stale clear, then clears the
choice. A wrong-bus selection is not Ready. No kit operation is claimed from
these host tests.

Own library selection and network/session coordination in FogCast, physical
programming in libmister-runtime, and policy assembly in FES. Keep the optional
Coleco expansion selection separate from cartridge ROM and BIOS selection.
The host and target should independently verify the selected module against
the exact shell and recompose the same full RBF bytes; a restart must restore
only an explicit library binding. The runtime programs only the verified
composed RBF under its existing lease. Add tests for stale selections,
wrong-shell payloads, altered relink bytes, clearing a selection and unchanged
ZX81 behavior. No new mister-packages mailbox contract is needed for this
CPU-bus slice.

## 3. Promote and accept an exact product artifact

After the linker and consumers pass, replace the development-only Coleco
producer with a selected FES package that advertises the optional slot. Keep
the original factory package selectable until the new artifact passes the
normal parent image checks. The current `config/core-recipes.toml` has exactly
one producer row for `fes.coleco`; do not point that factory row at the socket
candidate before exact-artifact acceptance. Seal the candidate separately from
a reviewed committed FES tree and import it into a private host catalog. Use
that catalog and the designated kit
lease to launch the exact empty shell, the same title with the diagnostic
module, a restart/relaunch, and a cleared selection. Record shell, module,
composed and programmed RBF digests; confirm visible CPU-bus diagnostic
results, Stop, and lease release. Register the new product assets only after
that exact-artifact acceptance. Cartridge and BIOS ROM linking remain a
separate follow-up because they are independent inputs.
