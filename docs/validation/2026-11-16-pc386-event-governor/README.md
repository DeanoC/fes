# pc386 diagnostic: deferred bytes across divide error

This record supports a **default-off experiment**, not a physical 286 model,
a finished governor, or tier-1 promotion. The source baseline is FES commit
`16b10bd948929107a4e19652084a95c9b2729495`. This milestone carries the
diagnostic fetch queue and committed deferred bytes across a real-mode #DE
that is not raised while a delivery is active. That condition does **not**
exclude a later #DE in a handler. Nested-handler timing against 86Box remains
unmeasured; `nested-control/` checks CPU architectural state only.

The separate segment-stack timing experiment applies to **PUSH ES, PUSH CS,
PUSH DS, POP ES and POP DS** only, with an unprefixed 16-bit operand, real
mode, a 16-bit stack and wholly ordinary-RAM accesses. PUSH/POP SS, FS/GS,
prefixed operations, other widths, non-RAM and protected mode retain the
previous diagnostic path. Preserving that path does not establish its accuracy.

## Method and measured boundaries

The guest loops in each retained `reference.asm` and `rtl.asm` were
byte-matched and assembled with NASM. The 86Box v6.0 reference ran a private
286 at 12.5 MHz, one wait state. Each reference value counts completed
1,000-iteration blocks in 64 BIOS ticks, except `hires-repeat`, which uses
256 ticks. The FES column is charged clocks in a CPU simulation using a
separately frozen diagnostic-enabled binary. Each DOS result includes
`EVENT_CONTROL_VERIFIED`: the fault probes check entry count, exact saved
IP/CS/flags, canary and stack restoration; ordinary segment-stack probes
check segment values, canary and stack restoration. The archived three output
files per experiment are byte-identical.

The table compares **whole-loop immediate-zero minus CL-zero differences**,
not absolute #DE instruction costs. Entries are reference estimate / FES
diagnostic clocks per iteration. The original high-resolution Powerboat run
is inconclusive because retained frames show 99%; `inconclusive/` preserves
its audit, sample frame and output. The authorized repeat used an isolated
3090-host VM with the identical pre-run disk SHA-256, same guest inputs and
settings, and a separately recorded host loader/Vulkan runtime identity.
The original-handler row uses the earlier v2 prototype CPU SHA-256
`a4cdcb3cdd4a6bc1dcc9a1c783e7582602734bfcf4328be37a345e184a4db537`;
the other rows use v4 CPU SHA-256
`2e5b6c136d63318bc31ac2dc68d3047407fa07688f204cfa67071bd39e1fd8d6`.

| Experiment | Reference / diagnostic delta |
| --- | --- |
| Original handler, 1 / 4 / 12 immediate-zero shifts | 1.585 / 2.001; 4.813 / 4.003; 11.511 / 12.000 |
| Empty queue, 7 shifts, 256 ticks | 8.024 / 8.003 |
| Filled queue, 7 shifts, 256 ticks | 3.973 / 3.971 |
| POP ES handler, empty queue, 3 / 6 / 9 shifts | 5.248 / 4.003; 7.042 / 6.003; 10.700 / 10.000 |
| POP ES handler, partially primed queue, 3 / 6 / 9 shifts | 3.906 / 4.003; 5.899 / 6.003; 9.966 / 10.000 |
| POP ES handler, filled queue, 3 / 6 / 9 shifts | 0 / 0.003; 2.258 / 2.003; 4.549 / 5.968 |

The independent ordinary-stack loops estimated 20.147, 20.138, 20.110 and
20.110 reference clocks for DS→DS, CS→DS, ES→ES and CS→ES respectively;
the frozen diagnostic CPU predicted 20.135, 20.138, 20.112 and 20.108.
These are whole loops including block controls.

**Absolute #DE delivery timing remains wrong.** In the 18-case POP ES
handler matrix, reference whole-loop estimates exceed the diagnostic CPU by
41.296–42.974 clocks; the first case is 274.627 versus 233.141. The 86Box
source's real-mode delivery charges 70 clocks, while the existing diagnostic
trace charges its synthetic delivery as INT with 24 clocks. This explains
why a large residual is plausible but does not establish an exact accounting
of the remainder. No overall handler-timing match is claimed.

Block-count quantization is approximately two clocks in the fault matrices
and 0.57 clocks in the high-resolution filled-queue case. Timer/ISR and block
setup, handler overhead and differing CPU/BIOS scaffolds add uncertainty;
quantization alone is not an error bound. The per-experiment `speed-audit.json`
files record every 4-fps sample and the boot screenshot supplies the 100%
glyph. All samples passed in the qualified runs; sampling does not establish
speed between frames. The retained failures and diagnostics do not use a kit.

## Retained evidence

`primary/`, `hires-repeat/`, `segment-stack/`, and `segment-handler/` contain
guest sources, measured raw outputs, the frozen prototype prediction, speed
audit and boot image. `failed-candidate/` keeps the earlier candidate's
predictions and short-loop traces, where a zero pending count incorrectly
discarded a primed queue. The short trace fixture is distinct from the timed
1,000-iteration programs. `nested-control/` retains a separate CPU-only
two-level #DE program and its original prototype logs. `provenance.json`
records source, program, emulator, result and audit hashes and the known
host-side config rewrites. Raw full-frame speed streams, copied VM disks and
full build logs remain in the private `out/dev/1902-*` working directories;
they contain no accepted package or hardware evidence.

The tested binary and source identities for a later combined build are listed
in `combined-control/build-identity.json`. The exact combined source was
built with the locked Verilator; its executable SHA-256 is
`4414d541cb6d386ab7332e7d8f6b0b8a105686d3881945d6a4873bae6576556f`.
That binary is byte-identical to the independently built earlier combined
snapshot executable used for the retained 44 CPU fixture cases. The exact
source's focused governor bench, including adjacent delivery and nested-event
controls, passed, as did 15 affected Python tests. A 262,144-cycle sampled
default-off module differential against the base governor passed across
presets and turbo settings. This is not formal, full-CPU or netlist
equivalence. The separate CPU nested-handler control also passed against the
combined executable, but does not establish nested reference timing.

The historical prototype reference measurements do not by themselves test
the combined build. Rebuilding,
changing the feature flag or promoting the core requires the normal exact
source, timing, seal and issue-specific acceptance gates.
