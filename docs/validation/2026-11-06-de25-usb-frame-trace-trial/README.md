# One bounded Nano USB frame/command timing trial

Compiled native app/entry and matched firmware:
`3149fb25e7d6cb5920e124851cdf23fa16112aec`, under
`out/nano-usb-frame-trace-20261106`. The
[matched build record](../2026-11-06-de25-usb-frame-trace/README.md) binds four
changed compiled source files and 2,095 unchanged combined input hashes.
The FPGA payload, ROM/configuration contracts, firmware RAM exclusion and
initialized per-CPU DDR with its exact no-map reservation remain unchanged.

The diagnostic records the full frame used for original parity selection,
read-only frame observations before/after direct periodic channel writes and
clock windows. Step service gaps reuse the existing deadline clock. These
sequential observations add overhead; they cannot prove the exact bus commit,
wire frame or physical fault cause. Commands, PID/HCTSIZ/buffer/deadline,
queue/interval/ownership exclusions and terminal failure rules are preserved.
The preceding failed `c8ff50c29` input is consumed and cannot be replayed.

This separately frozen operator permits one attempt on TRWU8CBL, after exact
claim/USB/UART/source admission and fresh recovery/SD-card checks following the
user-reported power interruption. It requires all eight bounded Ethernet/RAM
uploads, SD writes and fresh readbacks before native programming. The planned
native sequence is ZX81 ROM, JP-loop ROM, ZX81 ROM, each with full launch/return,
activation receipts and shared USB pause/resume observations, then owned Quit.
No physical controller action is needed or claimed; idle reports cannot qualify
physical controls. Ordinary package admission remains closed.

Sixteen off-board fixture/proof groups pass, including actual source/recovery
admission, malformed timing-proof rejection, changed-device admission, upload
receipt failures, private JTAG/clock cleanup, all boot-proof/tail decoders,
per-CPU placement/reservation and fixed catalog binding. The operator sources
and completed fixture receipts are frozen in [operator-source.json](operator-source.json).
Publishing those checks does not establish a physical result.

Keep all existing bounds: prompt 50 s, startup 100 s, launch 100 s,
configuration 20 s, control 5 s, native 450 s, child 480 s, work 740 s,
host 850 s; passive tail 2 s and console 1 MiB; original ROM bounds;
USB drain 500 ms/4,096 steps. There is one fused physical attempt, with the
first failure terminal and no retry, fallback, fake NAK/completion, error
masking, deadline extension or release of uncertain ownership.

Hold the shared Quartus/JTAG lock as normal user. Use only the exact private
TRWU8CBL server, temporarily 6M, then restore and independently verify 15M,
exact USB permissions, private-server termination and free UART. Host cleanup
does not establish a CPU/USB/FPGA halt. Keep the continuing claim and spare
card; factory restoration remains waived. The factory card and bottom kit
remain untouched. There are 24 prior procedure stops, including one USB return
stop and two USB packet stops before launch; this input has zero attempts
before its fuse is consumed.

Preparation performed no hardware actions. The physical result below is
recorded separately from those off-board checks. Ordinary core loading, native
Ethernet, physical HDMI and complete launch/return acceptance remain open.


## Physical result

The one attempt on operator `2a543c869` completed in 136.89 s and stopped on
its first failure. Recovery after the power interruption, eight Ethernet/RAM
uploads, eight SD writes and eight fresh SD readbacks passed. The exact native
chain reached Tenfoot Ready in 5.12 s. Independent raw replay verifies all 31
runtime, two exit-cache, 26 loader and 48 firmware-call records, including all
eight SMC and cache-flush returns.

The first launch key failed before configuration, retained-ROM fetch, core
launch, master-stop return or Quit. USB packet 46 retained expected PID2, one
paired RX/IRQ toggle notification and one continuation. Its error occurred at
poll 250 with HCINT `0x200`, bit9 frame-overrun in the matched vendor header.
It reported no real payload, completion or NAK. The original 500 ms containment
wait failed; the channel remained enabled. The separate drain-exhaustion
snapshot was not reached.

The [cached timing audit](timing-observation.json) identifies the actual same-
packet commands: arm sequence228 and continue sequence229 both wrote
`0x805e8808`; containment sequence230 wrote `0xc05e8808`.

| Continuation observation | Full HFNUM | Frame | Remaining ticks |
| --- | --- | --- | --- |
| Original parity selection | `0x2d850413` | `0x413` | 11,653 |
| Before write | `0x2d190413` | `0x413` | 11,545 |
| After write | `0x2c980413` | `0x413` | 11,416 |

The CPU clock window from selection start to the pre-write sample is 1,808 ns;
the bracket around the write is 2,722 ns. Written parity is even, the next
parity from the sampled odd frame. No sampled frame number changes in that
continuation window. At the declared 60 MHz clock, the last frame sample has
about 190.27 microseconds remaining. The last step gap is 8,355 ns and the
maximum recorded gap between steps is 1,174,748 ns. The first step has no prior
step-gap sample; this is not a complete measurement of frontend polling cadence.
The original arm selected frame `0x409`; the first toggle snapshot sampled
`0x40b`, and the continuation sampled `0x413`.

These sequential CPU/HFNUM observations do not reproduce the synthetic model's
frame-crossing delayed-write scenario for the recorded continuation. They
cannot establish an exact peripheral commit, chosen wire frame or fault cause,
and observational overhead is present. The latest continuation decision had
already been overwritten by an interval wait; the raw channel/transfer admission
values at the continuation are not retained. Do not infer those observations
from the software command or from the earlier first-toggle snapshot.

The passive two-second failure tail and independent raw replay pass. Independent
host cleanup verifies 15M readback, exact USB permissions/device identity, owned
private JTAG server termination and free UART. This proves no CPU/USB/FPGA halt.
The continuing claim and spare card remain held; factory/bottom kit are untouched.
This input is consumed, with no replay or fallback. There are now 25 total
procedure stops; this is the third USB packet stop before first launch, distinct
from the earlier USB return stop, 11 pre-loader and three application-start stops.

The post-run parser independently checks duplicate cached records, missing
samples, frame-wrap/parity arithmetic and seven malformed/contradictory cases.
[physical-result.json](physical-result.json) and the timing audit bind their raw
console, source, decoder and cleanup evidence. Source snapshots and the complete
trace are retained in `/home/deano/fes-retained/de25-usb-frame-stop-20261106-r1`,
with the matched frame-trace build archive as prerequisite; member hashes and
imported source pack are verified independently.

Next: investigate the primary DWC2 periodic request/retirement semantics after
a data-toggle disable, the advertised interval versus continuation servicing,
and the current frontend-driven service path. Preserve the last issued command's
admission state instead of losing it to later interval checks. Choose a repair
from that evidence, rather than assuming a large CPU-side command delay. Never
mask frame-overrun, fabricate NAK/completion, extend a packet deadline or release
an unhalted source. No normal core-load or complete launch/return acceptance is
established by this failed diagnostic.
