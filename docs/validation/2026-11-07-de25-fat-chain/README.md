# Read each FAT metadata sector once per consecutive chain span

The existing reader fetched the same 512-byte FAT sector for each cluster
entry, although one sector contains 128 entries. The change reuses its existing
sector buffer only while a locked chain walk remains within that sector.
Every new walk reads metadata anew. No archive, digest, identity, size, cycle,
bounds, ownership, containment or device-error check is removed.

On the same private regular FAT image containing the actual eight launch files,
the real archive retainer validates the identical 1,659,392-byte package before
and after the change. FAT reads fall from 1,623 to 15; data/other reads remain
3,245. Total calls fall from 4,868 to 3,260 (33.0%). The image checksum is
unchanged and the counted device receives no writes. This is a file-only
reference, not a replica of the live card or a physical speed measurement.

Race-enabled FAT, retention, SD lifecycle and controller tests pass, as do native
lifecycle/admission tests and affected parent/planner/generated checks. New
regressions exercise sequential and fragmented chains, reduced I/O counts,
fresh metadata between walks, cycles/reserved/bad/free/outside clusters, size
limits and failures on a later FAT sector. Errors still return no partial chain.

The [fresh matched chain](../2026-11-07-de25-fat-native-chain/README.md) is prepared
for a new bounded hardware comparison. Physical speedup and full DE10-style
core-loading readiness remain unproven. No board operation occurred here.

Raw probe/tests: `out/nano-fat-chain-20261107`.
