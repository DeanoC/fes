# Fresh matched halt-first periodic USB boot chain

Compilation source: `0d7556334156bc70450a90c4e6434d2c7ff80695`.
Driver implementation and software-tested source:
`33d40069011175f265d08e668c61db8c8cf3edc6`.
The intervening change is the prototype's documentation/evidence publication;
all selected compiled source bytes match both revisions.

This candidate rebuilds the native app/entry, BL31, U-Boot and HPS-first file
composition for the [halt-first driver](../2026-11-06-de25-usb-periodic-retire/README.md).
The previous `3149fb25e` physical input remains consumed. Its historical ELF or
boot-chain evidence cannot substitute for this candidate.

Direct periodic IN has one queued request per service. Paired toggle recovery
requires matching recipe/HCTSIZ, PIO/link/RX/status/queue admission, then actual
CHHLTD with both channel command bits clear. A new attempt is armed only on the
observed idle channel after the advertised interval, retaining the public packet,
PID, buffer and original deadline. Pause may explicitly discard an observed
halted attempt without a fake report/completion/NAK or PID advance. Resume retains
its next permitted poll time. Original drain, containment and failed ownership
rules remain unchanged. Nonperiodic continuation and split behavior are preserved.

Fresh compiled evidence:

- The native SDK contains 2,078 selected source files. `periodic.go` is new;
  channel history, continuation, host state/step, polling and arm code are the
  five changed prior inputs. Of 2,100 combined app/entry input hashes, 2,094
  remain unchanged. All forty entry-producer records match the prior candidate.
  Complete selected source bytes are archived/read back, checked against the
  actual compilation/implementation Git bytes and bound to the new ELF/entry.
- The actual stripped ARM64 image links continuation to periodic retirement,
  retirement to the halt write and idle retry handler, and idle retry to a fresh
  arm. Step dispatches a retained idle retry separately. Periodic retirement
  does not directly arm or call the periodic write wrapper. Source/lifecycle
  evidence separately establishes the halt, ownership and deadline gates; this
  call graph alone is not physical branch coverage or a halt proof.
- The timing wrapper's four indirect read/clock calls and one basic write are
  linked. The original drain snapshot still precedes failure containment. CPU
  mapping, firmware exclusion and FPGA/ROM/configuration contracts remain exact.
- Twenty secure device-driver objects and both vendor C startup objects are
  byte-identical. Fresh RAM, entry, loader/runtime, exception/status, decode,
  cache and final-branch audits pass. Initialized per-CPU DDR is still at
  `0x80036000`, private BSS ends at `0x5ddc0`, and both compiled DTBs carry the
  exact 2 MiB no-map firmware reservation at `0x80000000`. Original PSCI/cache
  operations are retained. Five fresh ARM firmware model cases pass with their
  existing modeled cache/UART limits; they do not execute the new USB driver.
- The prior nine lifecycle methods/49 scenarios, exact-predecessor red overlay,
  full DWC2/USB ownership/native runtime race suites, seven frame-model methods
  and all six affected checks are reused only after exact source comparison.
  Their 1,568 parent tests/three skips passed in 320.19 s. No unchanged software
  suite is presented as a new execution on the later documentation revision.
- The first new validator expected a short no-fake-completion field name, while
  the prototype receipt uses its explicit cancellation name. The corrected
  validator also binds the independent no-fake-completion policy audit. The
  failed script/log and wrapper result are retained. No driver or compiled
  input changed for this off-board validator correction.

Only final outputs under `out/nano-usb-periodic-retire-build-20261106` are
selected. Native ELF, entry image and FIT identities differ from the previous
candidate. FIT size is 8198704 bytes, within the unchanged 8 MiB limit.
Matched chain identity: `6526d2f02c575f03684b227f5b6200b934f369f8932ef982431200c6d1374e8b`. File composition used the shared Quartus
lock as normal user and performed no board programming.

There are zero physical attempts on this compiled input. This milestone changes
no firmware/FPGA API, memory reservation or normal package-admission permission.
It does not establish Nano halt acknowledgement, physical fault cause, native
network, HDMI or complete core-load/return acceptance. The continuing TRWU8CBL
claim and spare card remain held; factory/bottom kit are untouched. Host cleanup
from the preceding failed trial does not prove a CPU/USB/FPGA halt.

Next: freeze a separate one-attempt operator for this exact candidate, preserving
all prior recovery/identity, eight upload/readback, boot, USB/drain, launch/return
and host-cleanup gates. Use fresh claim/device/UART and live recovery/card
admission, stop first failure, and do not reuse the consumed image or relax a
halt, deadline, timing or ownership check.

Detailed hashes and checks are in [result.json](result.json). Complete source,
compiled artifacts and logs are retained at
`/home/deano/fes-retained/de25-usb-periodic-retire-build-20261106-r1`, with the
prototype archive as prerequisite. Every member is read back and its hash
checked, and the imported source pack is verified independently.
