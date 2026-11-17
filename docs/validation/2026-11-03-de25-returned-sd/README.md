# Owned SD reopening after a core return

**The firmware, native storage handoff and fresh package reselection pass their
software checks. The hardware sequence stopped in recovery Ethernet before
any SD write or native boot attempt.** Returned-core SD access and ordinary
Nano package admission remain unqualified.

The implementation and tested source are
`6e75ec57f3447cbe3e805ba6f6d9b1884046c00b`, based on
`0e5769420976670e2e252f6a606df3f7cac02e81`. The unchanged sealed FPGA producer is
`c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`. The matched candidate boot ID is
`1d884a401e393b85`; its FIT SHA-256 is
`1ea63a3ccdd94be214f9143627fdaca81b1ee03ba33bb684af06408f667d03cd`.
That candidate was built and checked, but was neither written to SD nor booted.

## Implemented behavior

The [bridge service](../../../platform/embedded/firmware/nano_bridges.c) saves a
private closure only after producer stop and exact reset readback. Opening,
failure or a completed new configuration invalidates it. Warm SD access requires
that successful control-only return with data continuously held, the same secure
owner and the live CPU/reset/clock/PHY scope. Active SD blocks FPGA operations.
Secure rearm completion separately verifies the stopped SD provenance.

The [SD client](../../../platform/embedded/tamago/sdcontrol/README.md) requires
an explicitly expected newer return, queries its exact live generation and
requests the grant once. The actual PIO driver rediscovers the original card's
CID/capacity. The [package store](../../../platform/embedded/tamago/packagestore/README.md)
invalidates previous selections/handoffs and records the return in the newly
contained handoff. The native verifier consumes its own successful computer
Close before requesting SD access; copying a public observation grants nothing.
Failed or uncertain ownership cannot replay.

The opt-in `--computer-storage-return` selection requires `--computer-reload`
and is bound through SDK, entry, link-command and secure cold-profile checks.
The diagnostic runs a fixed core, returns, reopens SD, retains the compiled
package again, contains SD and transfers the fresh retained bytes. Expected
activation generations are 4/9, master stops 5/10 and final returns 6/11. This
still selects the compiled sealed package. No general Nano profile, concurrent
core/SD access, native SD writes or ordinary runtime admission was added.

## Software evidence

All [11 affected scopes](software-summary.json) pass on the exact implementation:
1,326 selected parent tests in three intact-module partitions capped at 300
seconds each, plus 1,577 FPGA software tests. Two parent environment skips are
explicit: macOS `statfs` and an absent locked upstream U-Boot cache. Generated
consistency and `make check` pass.

Joined tests exercise three configuration/Run/return cycles and two SD
reopens/reselections. Separate tests drive three warm cycles through the real C
bridge/SD service, Go PIO driver and stateful card model. Strong cold-provider
tests cover success, wrong ownership, active SD and corrupted return provenance;
the last rejects secure activation even after hardware transfer completion.
Native tests reject failed Hold/Close, malformed closure and changed source
before SD can be granted.

Serial checks require three ordered SD-return records strictly between verified
core cycles and reject missing, duplicate, malformed, reordered or stale records.
The actual outer controller passes 132 injected cases, the real upload session
passes 18 stateful card cases, and 11 helper tests preserve the separate 45-second
core / 100-second SD / 45-second core phases. Four actual SDK/entry selection
mismatches reject before compilation. Prompt 50 seconds, initial SD 100 seconds
and each configuration 20 seconds remain unchanged.

The matched SDK/entry/BL31/SPL/SOF builds pass. The
[selected-input audit](selected-inputs.json) verifies all 115 selected Git blobs,
1,298 pinned module/SDK files and the actual native CPU/translation instructions.
Vendor startup assembly, linked relocation targets and contained-console checks
pass. A complete source bundle independently restores and passes `git fsck`
without shared object storage. No FPGA compile ran; future 3090 Quartus jobs must
hold the existing `/tmp/lander-quartus-3090.lock` throughout.

## Bounded hardware result

The [outer controller](experiment-result.json) failed during uploads at
2026-11-03T00:33:12 UTC. Qualified recovery boot passed. Before DHCP, the uploader
verified the original card/protected files, KSZ9031 ID `00221622` and two link/
negotiation-ready BMSR samples `796d`. The one DHCP command then reported PHY
autonegotiation timeout and `phy_startup() failed: -110`. The saved
[raw console](upload-console.log) and [upload receipt](uploads-result.json)
retain that failure. No download or SD write followed, and no native command or
candidate programming attempt occurred.

An [independent failure audit](failed-hardware-evidence.json) verifies zero SD
writes/native attempts, 15M JTAG readback before stopping only this session's
private service, exact USB permissions restored and UART free. The original
factory card remains safe; the spare, exclusive claim and parked recovery
prompt remain on TRWU8CBL under Deano's instruction. No factory restore ran.
Exactly one full hardware sequence was attempted for this implementation.

One subsequent bounded [read-only PHY inventory](phy-registers.json) found
BMCR `1140`, BMSR `796d`, gigabit control `0300` and status `3c00` in both samples.
It sent no DHCP, reset, SD or native command and closed its UART. A wrapper
filename collision happened after the successful readout had been saved; the
[wrapper error](phy-read-wrapper-error.json), original readout and
[raw inventory](phy-read-console.log) are retained, and the readout was not repeated.

The pinned recovery driver connects/configures its PHY lazily in the first
XGMAC start, after the uploader's readiness samples. Its internal negotiation
timeout is 4,000 ms. This identifies an ordering gap; it does not prove precisely
why this negotiation exceeded that window. The next repair is to prepare the
PHY before accepting readiness, while preserving the existing phase bounds.
There was no retry of the failed upload or native test.

Full matched artifacts, exploratory/failed checks and raw traces are under
`out/nano-return-sd-20261103/`; exact affected checks are under
`out/nano-return-sd-checks-20261103/`. External source retention is under
`/home/deano/fes-retained/de25-returned-sd-20261103/`. Earlier fixed-core hardware
success qualifies only its earlier exact artifacts. Physical warm return,
arbitrary compatible package loading and Tenfoot/video/input integration remain
open; the full base-system goal is active.
