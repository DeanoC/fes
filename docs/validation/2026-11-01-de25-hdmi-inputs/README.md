# DE25 HDMI input and startup compiler proof

The scanout board now synchronizes external HDMI SDA and interrupt inputs in
CLOCK0_50 before the initializer samples them. A fresh canonical MENU candidate
compiles and passes the actual derived-controller protocol tests. It remains
unprogrammed and unsealed: four of 85 signoff rules still fail.

Producer: `d8b23267cc8301361bdc5c2ef7db89593462f4a6`. BUILD_ID:
`4db01378ee1bdf8877a83e162fff93ce`. The 1,736,704-byte RBF has SHA256
`ee0cac2f36644d1ff1cd39203a2fdfecd0a3f5440ce44766e06daa918064391f`.

| Check | Result |
| --- | --- |
| Quartus 26.3 Pro compile-only | PASS, 617.3 s; [receipt](compile.json) |
| Generated interconnect with modeled M20K storage | PASS, ten cases in 70.5 s; [receipt](generated-transport.json) |
| Actual HDMI initializer and controller | PASS, four protocol cases and 40 equivalence cells in 5.6 s; [receipt](hdmi-protocol.json) |
| Independent identity, source, artifact and provenance checks | PASS, 0.8 s; [checks](checks.json), [receipt](verification.json) |
| Complete affected software suite | PASS, 1385.4 s; [receipt](software.json) |
| Parent generated/source consistency | PASS, 5.6 s; [receipt](parent-check.json) |

The identity check verifies all 370 canonical inputs, 175 compiled-model HDL
sources and 17 QIPs, final generation and Linux phase artifacts, and the actual
RBF. The unchanged generic exporter validates an in-memory manifest's provenance;
no manifest, package or SD image is exported by this candidate.

SDA and interrupt each enter two preserved 50 MHz flops. SDC cuts only each
external input to its first D pin; subsequent stages, reset and fanout remain
timed. This is an asynchronous input architecture, not a numerical external
IO-delay budget. The raw vendor Config and Controller are retained outside the
compiler project; the actual scanout project uses named derived cells. Hashes
and the stage-one bindings are checked after generation and compilation.

All three controller ACK samples use synchronized SDA. The controller's 53
sequential blocking assignments become nonblocking. The initializer also waits
for END to become low after GO before accepting completion, so stale reset END
cannot skip the first LUT entry. The 33 vendor LUT values and divider are
unchanged; SCL has a 5,002-source-edge period.

The real derived cells and board wrapper execute all 33 three-byte writes with
every byte checked. Interrupt reconfiguration repeats all 33 writes (66 total).
A NACK at address, subaddress or data retries exactly once (34 writes per case).
The slave drives open-drain ACK 3.51 microseconds into the clock-low interval;
the test also checks the synchronizer latency and actual divider period.
Digital PLL/configuration models used by other board tests remain separately
identified and do not substitute for this protocol test.

Yosys proves all 40 controller equivalence cells with `async2sync` reset
modeling. It compares ACK-retargeted vendor control before and after sequential
assignment normalization; it does not claim the intentionally changed
initializer is equivalent. Earlier direct and raw-reset induction attempts
remain failed/unproven in development retention. This proof establishes no
physical asynchronous-reset timing, metastability/MTBF, PLL or electrical
behavior.

All 74 reported timing checks have finite nonnegative slack. Signoff still
reports TMC-20012 (30 High), TMC-20011 (two High), RDC-50003 (two Medium) and
RES-50101 (three Low), with zero waived findings. Adding input synchronization
has not removed the missing-input-delay rule. The vendor archive supplies no
external HDMI SDC input/output delays or PCB routing files to derive a board
delay budget. Required clocks and the strict production seal remain unchanged;
no board delays are fabricated. Package export and the independent successful
signoff rebuild remain blocked by these findings.

Full compiler project, raw reports, model/protocol binaries and logs are
retained in the independently verified 311,388,040-byte archive, SHA256
`8dbb7b919092718347e3d27ab255b12ddf75f6a5b1890d824eb1749ec91fbeeb`. Source-bundle restoration
and extracted RBF/receipt hashes pass. Local retention is
`out/artifacts/de25-hdmi-inputs/d8b23267c/retention.json`; original development
failures are preserved separately in its development archive.

[Source equality](source-equivalence.json) keeps existing runtime, agent,
contracts, kernel and headless-image evidence at their original producers.
The board remains on the earlier owner-ABI-2 tracing card, with no kernel,
FPGA, card, bundle or QSPI write in this phase. No new physical or normal-kit
acceptance is claimed. The [normal-kit gates](../../design/de25-linux-platform.md),
managed image/bootstrap, native MENU/catalog lifecycle, restart, physical SAVE,
security/reset and whole-call bounds remain open.
