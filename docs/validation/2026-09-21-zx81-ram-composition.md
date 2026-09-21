# ZX81 optional RAM through the normal library

The designated MiSTer Pi passed the exact-artifact library diagnostic with one
sealed 1 KiB shell and an independently built 16 KiB RAM cart. Selecting or
clearing the pack did not rebuild the shell or change its package/BUILD_ID.
The operator restored the original kit software and configuration afterward.

## Selected artifacts

| Artifact | Identity |
| --- | --- |
| Shell package | `01395c26800e58e33811fb74bf25aa697d1b5047933c65905e00526ab331fadb` |
| Base BUILD_ID | `bbf8e204088ea23557a193fd880b6d2c` |
| Shell RBF SHA256 | `28ec0bf42c555874da72745452e0bdb2ac22319a109b29a1bda25cb254bff529` |
| Expansion asset | `33450bc8e90e791784bec865d682b0eaf2fd1c0a64314978b0f6cb36e2e6bc9a` |
| Composition | `c6ee49901236459e6a082c8a40790d045c92ec91b760dbcb8287052fc74c4064` |
| Linked payload SHA256 | `2166f1171e77031fd3f4f2a4f9edb21d1ebf8f4bb2a2f4f33292e661902e697f` |
| Linked payload size | 2,572,142 bytes |
| Cart recipe | `911e539546f117e38c53f5c28f87002a84adcf42546f9a16b84eda6b9b2bb58b` |
| Host/agent/Kit software | `549ce1c703ae9e9eb75057902b92bcc297d6a357` |

The shell retains producer revision `b7d896a3`, nextpnr `b5a0ea71` and Mistral
`b28e30a3`. The cart was produced at `d49f43c3` with authenticated nextpnr
`74f26cc1` and Mistral `18db2489`; its functional inputs match the integrated
source. These are independently recorded provenances, not a relabelled shell.
The scoped compiler changes are published in nextpnr PR73 and Mistral PR3;
default and registered-memory compiler selections remain unchanged.

The unchanged runtime binary was retained from the `22142395` software build,
SHA256 `37bb086233d6988897ed729f955e24e6dc3ade4c51888672125e8c1a5f19637a`.
Its source tree is unchanged in the tested selection. Frozen software records
identify each executable's build revision, hash and size separately.

## Build and host verification

The authenticated cart producer passed HIP routing with no overuse or
architecture failures. System Fmax was 54.4929 MHz against 52.0021 MHz;
pixel Fmax was 101.7605 MHz against 74.2501 MHz. Header bytes were identical
to the sealed shell; 179,289 configuration bits changed inside the socket and
zero non-CRC bits changed outside it. The final producer reproduced the
reviewed diagnostic cart bytes exactly.

Independent Python and Go composition produced identical linked bytes.
Real-asset staging, restart adoption, cleanup and frozen identity checks
passed. Independent review verified the final source closure, tool identities,
artifact hashes, timing, physical boundary and provenance distinction.

The first kit attempt exposed a software performance problem before expanded
programming: ARM composition staging took 59.699 seconds and exhausted the
request budget. The valid artifact also passed standalone adoption in 60.236
seconds. Direct canonical-frame validation and overlay reduced those measured
times to 7.936 and 7.428 seconds, without changing linked bytes or deadlines.
CRC/EDCRC, padding, outside-socket and input-ownership checks remain enforced.
The linker race suite, Python golden/equivalence and malformed-frame tests,
affected FogCast packages, cross-builds and parent consistency passed.

CPU router2 previously encountered an undriven RAM-padding assertion during
compiler diagnostics. It is not counted as a passing route; the selected
product producer and this artifact use HIP routing.

## Hardware observations

Target: `73dc9f5f-1a12-4a95-a820-a9b4e600769a`, designated MiSTer Pi.
The private host used the ordinary package import, expansion import/selection,
library launch and session keyboard APIs. No direct RAM-peek command was added.

| Same library entry | Visible `PRINT PEEK 16389` | Generation |
| --- | --- | --- |
| Empty shell | 68 (RAMTOP 0x4400) | 1 |
| Selected RAM pack | 128 (RAMTOP 0x8000) | 2 |
| Pack retained after host restart | 128 | 3 |
| Selection explicitly cleared | 68 | 4 |
| Empty shell reloaded | 68 | 5 |

All five HDMI captures were manually reviewed. Every launch retained the exact
base package and BUILD_ID; only the two expanded launches reported the expected
composition tuple. The same entry and expansion selection survived private-host
restart. Each Stop reached idle.

While the expanded machine was active, wrong-shell asset import returned 404
and missing-asset selection returned 400. Active identity, generation and
catalog choice remained unchanged, and both retained display captures showed
128. These negatives did not interrupt the running machine.

The restoring operator exited zero: original on-disk and live executable hashes
matched, boot identity and agent configuration were unchanged, and the target
was ready/idle with a free lease. Private container, target staging and private
host configuration were removed. No permanent image/card update occurred.

## Evidence and scope

Retained Powerboat evidence:

- `out/hardware/zx81-expansion-20260921-02/`: frozen candidate and software,
  request/identity records, display captures, `operator-result.json`, and
  `review.json` binding the manually inspected frames by hash.
- `out/hardware/zx81-expansion-20260921/`: failed initial attempt and standalone
  before/after ARM timing measurements; retained rather than overwritten.
- `out/dev/expansion-delivery/fes/sources/misteross/build/zx81-ram-expansion/911e539546f117e38c53f5c28f87002a84adcf42546f9a16b84eda6b9b2bb58b/`:
  authenticated producer logs, timing, recipe, archive and linked payload.

This establishes exact-artifact normal-library RAM composition, BASIC keyboard
input, visible memory sizing, rejection continuity and lifecycle behavior.
It does not qualify physical USB input, tape loading, all expansion combinations
or a complete appliance release. Operator instructions are in the
[ZX81 RAM guide](../zx81-ram-expansion.md).
