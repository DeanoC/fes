# Native request-service hardware diagnostic

TRWU8CBL passes three native request-driven load/Run/Hold/return sessions in one
boot, ending with terminal Park. The service freshly retains each SD choice,
configures its FPGA payload, checks live identity internally, accepts computer
commands, stops its relevant masters and returns ownership before reusing SD.
The checked host client rejects stale/malformed requests and never retries an
uncertain command. No native command follows failure or Park.

All three choices use the same sealed ZX81 package. This qualifies the exact
contained service image and request flow; it does not qualify distinct physical
payloads, ordinary Nano runtime admission, native Tenfoot or physical USB/video/
native-Ethernet integration. Keyboard results are GP command acceptance, not
USB-device acceptance. The full core-load goal remains active.

## Exact source and evidence

- Current client/trace source: `4be141f96` (full commit in [summary](summary.json)).
  The implementation was introduced in `702a89de53e6e0fb5209f6e10ca7531dcb7d80c4`;
  the follow-up normalizes text fixtures while preserving raw-capture provenance.
- Base: `1ba994d591892cf6d5b89fcb61253c74a9d1d8c2`.
- Native service implementation/build: `af37a76aed47e049b0a70909026dd55cce567b1a`.
- FPGA producer: `c272cb2b320740220fc2b7bb1c8dfd92a0ffe6f6`.
- Native boot ID: `916b895d55afba6e`.
- FIT SHA-256: `432f06edbbedf6ee2cf89362be688f3720da907e9088908bd49af2b7b3eaa7fd`;
  6,823,844 bytes.
- SOF SHA-256: `f70325574150e873cdacb399479e736d4a28cb6eb129bc89f2182ef802685aa3`.
- FPGA build ID: `dda236646bb868e63c72bd0e777f3dd6`.
- Package ID: `b2353f540cc81b9896db86867248880aceffce8c81314042b02317f76f947f37`.

The prior [matched service candidate](../2026-11-03-de25-program-service/README.md)
is reused. All 121 native Git inputs and 1,298 pinned SDK/module inputs match
both its compiled source and the current checkout. No native cross-build, FPGA
compile or 3090 job was needed. Future 3090 Quartus work must hold its existing
shared lock throughout.

## Client and runner verification

`configuration_trace.extract` now supplies the same transfer grammar to the
fixed-probe decoder and the request-service validator. The existing 55
probe-decoder tests pass after extraction. The service client checks the actual
matched SPL/loaded-BL31/EL3/loader/EL1 prefix and a single complete Ready reply.
It then binds each reply to its boot owner, operation, local generation and
checked catalogue. Launch requires the complete bounded configuration, queue and
fabric-activation observations followed by the held-data control observations.
Return/Park requires the exact master stop. Running fetch progress, held stability
and resumed progress are independently checked from the counter observations.

All six current affected scopes pass. The exact parent selection has 1,355 tests:
1,353 pass and two explicit environment skips; the native candidate's eight
previously verified scopes are reused for unchanged inputs. The
[skip record](environment-skips.json) names the unrelated locked U-Boot cache
and the macOS-only statfs layout on Linux.

The actual exchange helper passes 12 fragmented-UART/failure cases. Its actual
outer controller passes 59 recovery/upload/native/cleanup cases, and the
independent replay function passes 12 success/damage cases. The actual uploader
passes 46 four-write/four-reread cases; 185 receipt mutations reject. Seven
transport/parser/helper checks pass. Rejected or failed phases never advance
and do not replay native commands. These failure cases are off-board evidence.

Initial off-board extraction/import/scope errors and a fixture mutation/deadline
assertion were corrected before hardware. The first committed-whitespace scope
rejected CR line endings in newly added text fixtures. The follow-up normalizes
those fixtures to LF, records that transformation, and explicitly tests actual
CRLF framing. The failed run and original byte-exact source capture remain
retained. No board attempt occurred before every final gate passed.

## Physical sequence

One qualified `phy-prepare` recovery boot precedes one owned Ethernet session.
It verifies four fixed uploads and freshly rereads all four after the final write:
`FES/UPLOAD.BIN`, `FES/PKGTEST.BIN`, `FES/PROGRAM.JSN` and `FES/CORESEL.JSN`.
Protected recovery FIT/manifest, card/prefix, stable MAC/PHY and scratch-memory
checks pass. The native image then reaches Ready and rejects four invalid launch
requests before the first valid launch.

| Session | Catalogue index | Application session | Launch seconds / bound | Configuration seconds / bound | Ending |
| --- | ---: | ---: | ---: | ---: | --- |
| 1 | 0 | 1 | 72.512 / 100 | 13.3468 / 20 | Ready generation 2 |
| 2 | 1 | 3 | 72.496 / 100 | 13.3469 / 20 | Ready generation 4 |
| 3 | 0 | 5 | 72.513 / 100 | 13.3485 / 20 | Parked generation 6 |

Each session checks running fetch progress, Hold and two stable observations,
nine-row keyboard updates/neutralization, Run and resumed fetch progress. The
first two returns reopen SD; stale return/launch requests reject before the next
fresh load. The final Park leaves SD contained and data held. Return/return/Park
take 0.263/0.232/0.220 seconds, each below the five-second control limit. Secure
configuration generations are 4/9/14, with master-stop observations at 5/10/15.
Application request generations are a separate counter. The matched Close code
verifies the corresponding reclosure before publishing Ready/Parked; there is
no separate public firmware-return-generation field in this interface.

The whole wrapper passes in 490.63 seconds under the 800-second inner experiment
and 900-second wrapper limits. No deadline was widened. The independent audit
replays all 44 exchanges from 31,080 saved bytes and rejects 58 damaged captures.
Raw SHA-256:
`c6abc7aa5372a1e991a613cecd79d38c7dee361980a8ce6a655d74a2f160638c`.

Serial program identity is the service publication after its source-checked live
opening, not a separate GP register dump. The board mapping checks remain in the
linked platform hooks around actual reservations/MMIO; this frontend emits no
separate live mapping report. SMMU/firewall observations and successful complete
master stop grant no general DDR-drain or ordinary-runtime qualification.

## Retained board and source state

JTAG 15 MHz was restored and read back before the owned private server stopped.
Exact USB permissions, stopped process identity, free UART and continuing claim
were independently checked. TRWU8CBL remains at terminal native Park, application
generation 6, final master-stop generation 15, with SD contained and data held.
The exclusive claim and updated spare remain retained; the factory card is safe,
with no requested restore. No card swap, recovery upgrade or native SD write ran.

Recovery must use
`out/nano-version-suffix-20261103/recovery-kit/network-recovery.sof` with the
spare's current matched `phy-prepare` FIT/manifest. Preserve `FES/R1103.ITB` and
`FES/R1103.JSN`. This consumed test directory cannot replay.

[Summary](summary.json) and [independent physical audit](hardware-evidence.json)
record exact sources and hashes. Full helpers, traces and logs are in
`out/nano-service-trial-20261103/`. Source restoration/fsck and member-verified
artifacts are retained in `/home/deano/fes-retained/de25-service-trial-20261103/`,
with explicit native-candidate/frozen-input dependencies. Ownership remains
`lane:deano`; no PR handoff or merge occurred.

Next: build and verify genuinely distinct eligible FPGA payloads under the shared
Quartus lease, then test native selection between them. Native Tenfoot, physical
input/video/native-network support and ordinary package/runtime integration
remain requirements of the full goal.
