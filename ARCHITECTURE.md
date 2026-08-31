# Architecture

## Current boundary

This repository has one runtime implementation split across three narrow
roles:

- `libmister-runtime.a` owns the public lifecycle API, state, validation, and
  profile model.
- `mister-runtime` owns the local Unix-socket protocol and delegates every
  hardware-changing request to that lifecycle API.
- `src/native` and `src/linux` contain the Linux hardware primitives and the
  production construction boundary. Construction intentionally returns
  `io_failed` today because the image-owned idle artifact, production profiles,
  and accepted device composition do not exist yet.

The library does not own a network API, catalogue, transfer cache, or host
session. A future target agent integration belongs outside this repository
and will call the daemon over `/run/mister-runtime.sock`.

## Lifecycle and ownership

The lifecycle states are `idle`, `starting`, `running_game`,
`running_development`, and `reboot_required`. Starting the runtime deliberately
asks the hardware boundary to establish idle. A game launch validates the
entire request against the one runtime-owned profile table before mutation; a
development launch validates its absolute RBF path without inventing a system
profile. Stop returns the hardware to idle.

At most one hardware-changing operation is admitted. A concurrent mutation is
rejected as `busy`; operations are not queued. After a mutation has begun, a
failed launch gets exactly one cleanup attempt. Cleanup success returns to
`idle` with the original error; cleanup failure returns `reboot_required`.
There is no retry loop, failover path, recovery coordinator, or second
ownership database.

Profiles own core identity, semantic media roles and indices, and allowed
settings. Callers own selection and staging of absolute paths. The production
profile table is currently empty, so no FPGA system is implemented or
supported. Test profiles are private fixtures and cannot be selected by the
production daemon. The canonical support record is the
[support matrix](docs/support-matrix.md).

## Protocol

Protocol 1 accepts exactly four operations over a local Unix socket, one JSON
request and one JSON response per connection:

- `status`
- `launch`
- `load_development_rbf`
- `stop`

Requests reject unknown fields. `launch` accepts a stable system ID, an
absolute RBF path, semantic media paths, and profile-declared settings.
Responses report `ok`, lifecycle state, execution type, system/core identity
when present, a direct error when present, and the runtime version.

`status` is the sole reconciliation mechanism for a response lost after
dispatch. The caller observes daemon state instead of guessing whether a
mutation happened or consulting another authority.

## Build and link closure

The canonical host build produces one production archive and one daemon. The
daemon links the whole archive so unresolved or accidentally omitted native
members fail at the final link. Acceptance guards audit the archive manifest,
exclude fake and historic symbols, exercise header dependency invalidation,
and compare two clean archive hashes for determinism.
