# Bounded non-clearing status probe with corrected host enumeration

One fresh procedure for firmware f7e915eb2 and byte-exact native application
and entry 31d716957. The matched chain, 39 fixed reads and register-map review
are unchanged from the non-clearing fault-status validation. No firmware rebuild.

The preceding operator28fae stopped before programming or SD writes: its error
search matched the word "error" in its own configuration path printed by Quartus.
That stopped procedure remains fused and retained separately. This operator
removes only the exact expected environment notice once, then requires exactly
the cable and device records, the correct chip ID and no real error. The saved
physical listing passes offline through the actual corrected admission method.
Seventeen admission cases reject foreign/repeated notices, extra records, real
errors, wrong/missing IDs, extra cables/devices, wrong clock and missing claim.
Five operator fixture groups and 24 source/provenance rejections pass; no device
is opened by those checks. The source freeze binds all 29 operator files.

Campaign history now records12 procedure stops: one programmer failure, two
host setup stops, four ROM deadlines and five pre-native startup stops. The
current f7e firmware has one host-only operator stop and zero native executions.
The fresh procedure permits one attempt, without retry or fallback.

Read-only live preflight must validate the continuing top-kit claim, source and
artifact identities, exact adapter, free UART and disk space. The normal-user
shared Quartus lock encloses the operation. Chip admission at6M precedes
recovery programming. Eight fixed owned-file uploads and fresh SD readbacks
precede any native attempt. Bounds stay prompt50/startup100/launch100/config20/
control5/native450/child480/work740/host850 seconds; ROM16-byte/1-second/64-poll
limits are unchanged. Startup requires guard entry/pass, all15 vendor markers,
no exceptions and an empty fault-status inventory. Any original exception stays
fatal regardless of probe completion. Passive UART drain adds no commands.

If startup passes, three serial Tenfoot ROM launch/return/Quit cycles, matching
activation receipts, USB report and pause/resume evidence remain required.
Restore and read back15M, exact USB permissions, terminate private JTAG and
verify free UART independently afterwards. Cleanup is not CPU/FPGA halt proof.
Retain claim and spare card; factory card and bottom kit remain untouched.
Factory restore is waived by the user. No physical controller action is needed
or claimed; earlier user input cannot qualify this run. No ordinary admission,
HDMI, native Ethernet or full DE10-equivalent acceptance is claimed here.


## Physical result

One attempt on operator53892975d / firmwaref7e915eb2 completed recovery,
eight fixed uploads and eight fresh SD readbacks, JTAG programming and SPL
FIT verification. Early firmware ENTRY/PASS and eight ordered vendor markers
were followed by the same EL3 SError: ESRbe000011, savedPC8001b1f8 at linked
DC IVAC,x0, r0=50240/end5dec0/line40hex. The asynchronous saved PC is not the
proven originating instruction. The complete 39-read inventory was captured,
independently replayed and remains terminal. No fesboot command was sent.

The sampled CPU is Cortex-A55 r2p0 (MIDR412fd050), PFR011112222 exposes RAS1;
SCTLR00c5183a has MMU/data cache off and instruction cache on. The DSU-port
and OCRAM-interface uncorrectable statuses are0 before/after, so these samples
do not establish the proposed CCU decode-error cause. OCRAM control3 enables
ECC and its bus-error suppression; single-port configuration83090 has port-A
initialization complete1, pending/mode/decoder flags0. Only firewall region0
is enabled, covering0..7ffff with secure/nonsecure access allowed. Boot region
attributesc0700600 map the valid low region to DII. These configuration/status
reads neither prove successful RAM data access nor exclude CPU/cluster cache
errors or all other error sources. The offline status-diagnosis record derives
MMIO field values from the pinned primary register map.

The first failure marker stopped active commands; passive UART drain captured
PROBE END. No native startup, native CPU exclusion, ROM completion, CPU Run or
return was reached. No retry. Independent cleanup verified recorded15M readback,
exact USB permissions, owned private JTAG gone and UART free. Claim/test card
retained, factory card and other kit untouched; CPU/FPGA halt remains unproved.
There are now13 campaign procedure stops; current firmware has one native
failure plus its earlier host-only stop. This image/procedure will not be rerun.

Next inspect the architectural RAS records now that CPU and feature identity are
known, using documented feature/selector bounds and retaining failure markers
before every access. Preserve the original exception, park-only fallback and
all time/byte/ROM limits. Do not treat clear CCU/OCRAM samples as permission to
bypass vendor cache maintenance, clear status, disable ECC or scrub live OCRAM.

Primary references: [Altera CS/R1 register map](https://docs.altera.com/v/u/resources/864455/agilex-5-cs-and-r1-suffix-opn-hps-register-map),
[Arm Cortex-A55 TRM](https://documentation-service.arm.com/static/5e7e09f6a3736a0d2e862d2f),
sections A8.6 and B2.40-B2.50 distinguish architectural core/DSU RAS records
from the CCU-port registers already read. The physical result remains diagnostic.
