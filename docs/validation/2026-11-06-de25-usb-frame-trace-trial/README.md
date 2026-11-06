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

No hardware action has occurred while preparing this operator. Any subsequent
physical trace/result and cleanup evidence must be recorded separately; ordinary
core loading, native Ethernet, physical HDMI and complete launch/return
acceptance remain open.
