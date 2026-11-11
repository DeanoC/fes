# DE25 partial-reconfiguration control

`fes.fabric.de25-pr-control` is the diagnostic control contract for the single
slot in the DE25 base/persona build. The YAML definition generates matching
RTL and C++ constants. Its peripheral offset is relative to the generated
subsystem; it is not a CPU physical address. No production programming profile,
device binding or package admission is granted by this definition.

The slot and control use the preserved GHRD's 100 MHz clock. Cold configuration
starts frozen, with request issue disabled, responses isolated and the persona
held in reset. A 128-bit build identity binds the controller to the selected
base. Words 0 through 3 are the most- through least-significant eight hex
digits of that identity. A caller must verify all four words before control.

Every command stages one full-word argument, then submits one full-word command.
The high byte selects FREEZE (1) or RESUME (2); the low 24 bits contain a strictly
increasing sequence, beginning at 1. Sequence zero, reuse, gaps and wrap are
invalid. The argument is consumed on submission. Only one command may be in
flight; counter/status reads do not submit another command.

FREEZE requires argument zero. It stops new requests, drains accepted responses,
asserts local reset and isolation, and waits for the registered boundary before
publishing FROZEN and the completion sequence. RESUME requires the expected
persona ID. It releases reset and probes address zero while normal requests
remain gated. Only a matching returned ID completes RESUME and publishes
RUNNING. Addresses 1 through 255 carry the normal diagnostic ROM/counter traffic.

READY identifies a live control epoch; BUSY identifies a command in flight.
FROZEN is a positive completed drain/reset/isolation condition. RUNNING means
the expected persona has passed its identity probe. FAULT blocks both operations.
The three state bits BUSY, FROZEN and RUNNING are mutually exclusive. The
completed-sequence register advances only after successful completion; observing
an old sequence is not success for a new command.

Partial writes, writes while busy, malformed addresses/commands/sequences,
unexpected responses, a wrong persona ID, operation timeout or a later
peripheral reset produce a sticky fault. Reset/timeout is not drain proof.
There is no local CLEAR or retry that can invent a new epoch. An uncertain
operation requires external recovery and a new FPGA configuration. Software
polling deadlines cannot interrupt an actual blocked MMIO transaction; a future
physical backend must supply ordered, atomic, bounded accesses.

The runtime controller uses an exclusive injected register backend and an
injected programming operation. It admits programming only after matching the
selected base identity and positively completing FREEZE. An uncertain program
result poisons the software controller; it issues no RESUME and requires
external recovery. It cannot infer the hardware state after a failed callback.
Physical access and the production runtime constructor remain
unadmitted until the exact board/firmware/resource and lifecycle are qualified.
