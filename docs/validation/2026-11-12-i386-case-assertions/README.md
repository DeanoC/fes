# i386 generated case assertions, 12 November 2026

This is host-only evidence for the i386 CPU source and diagnostic at commit
`5ca9ba943e16997c8abc3be6f888e145358916da` (CPU base `9886d43d`). It does
not qualify a later netlist, four-state behavior or physical hardware. The
normal netlist simulator retains its `--no-assert-case` waiver.

The authenticated OFF slot `ddc23ba25a44cf71e5cd8cf87da51a7c9cbd73b26826e25caa1453c082b632f5`
used Yosys `0a7927bf126eb3d868c50b3bb5e11b69ff656ea9` and Verilator
`bbb60a403932a9f695cb6ff001ceb73de6ad9234` (5.054). The slot receipt SHA-256
was `a21eb4afa43fa404469446e8d92e99d9c118c3b4e504a50af6e36cab2bc74b83`;
the Verilator executable's SHA-256 was
`9de5203c8d4bd5572f89a370cefee6c0f3ab786376c6648078c7461208bf645f`.
The Yosys binary, Verilator wrapper, uninstrumented netlist, probe netlist and
individual source-input hashes are in [summary.json](summary.json).

The private probe netlist exposed the selector of each of its 320 generated
`parallel_case` functions. Each function was checked to contain exactly one
case pattern per selector bit and a default. The locked Yosys read that netlist
and ran this two-state SAT pipeline:

```text
hierarchy -top fes_i386_cpu
proc -norom -noopt
flatten
cutpoint t:$pmux
opt
sat -ignore_unknown_cells -prove audit_bad 0 -show audit_bad
```

The pipeline cut all 320 pmux outputs before optimization. The FF and memory
cells named in `summary.json` had no SAT model, leaving their outputs free.
With those overapproximations, no counterexample exists for any of the 320
one-hot-or-zero selectors at a settled combinational fixed point. The same
model was satisfiable without a proof constraint; forcing site 0's selector to be
multi-hot produced a counterexample. Separate controls showed an ignored FF
output and an ignored memory-read output could each be one. The record includes
hashes of all five SAT logs. No source-level reachability, four-state or hardware
claim follows from this result.

The private Verilator model enabled the generated case assertions and counted
stops nonfatally. Selector probes were sampled after the same testbench `eval`
returned; these extra outputs can change simulator scheduling. Across 383,495,489
sampled evaluations, 75 of the 320 sites fired 152,414 times; none of the
observed firing evaluations remained multi-hot at that sampled boundary. The
other 245 sites did not fire in this run. The per-site counts, selector mappings
and unobserved indexes are in `summary.json`.

The same model passed all directed reset, unreal, protected, paging, no-387 and
memory modes; six bounded 100-program random variants; all 54,096 generated
unbuilt encodings; and 1,758,699 non-revoked vectors across all 941 files in the
pinned SingleStepTests/80386 revision `459d49fbe6280e9ed46fee887b58dacd9cb880ab`.
The [corpus receipt](corpus-receipt.json) records each file's pinned Git blob;
its file list was compared with the upstream tree with no missing or extra
entries. The report includes per-file totals and the exact receipt SHA-256.
This covers those inputs, not every possible instruction sequence.

A separately built diagnostic control forced one actual selector multi-hot. On
the same committed source and locked toolchain, the stop handler counted that
site and the settled probe recorded one multi-hot evaluation; the test process
continued. The normal and control full JSON reports and SAT logs remain under
`/var/tmp/fes1654-full-5ca9ba943` and `/var/tmp/fes1654-neg-5ca9ba943` on
Powerboat. Their hashes are in `summary.json`. To reproduce, use the diagnostic
command in the [i386 simulation guide](../../../sources/misteross/cores/fes-common/sim/i386/README.md#verilator)
with `--unbuilt --corpus /abs/pinned-corpus`; use `--negative-control 316` for
the injected selector control.
