# pc386 286 governor reference diagnostic — 16 November 2026

This is an unfinished, default-off experiment for issue #1902. It is not a
production governor fix or a tier-1 qualification. The user decided that the
Ultima VI speaker-pitch difference from the reference blocks tier 1. The
experimental governor has not been enabled in a registered package and has not
been tested against that pitch gate in an exact sealed artifact.

## Model and boundary

The experiment estimates the **86Box 6.0 286 reference**, not a physical
80286. It maintains an effective 16-byte queue, accounts for instruction bytes,
prefix costs, data-bus occupancy and architectural redirects at committed
boundaries, and applies measured form-specific execution costs. The physical
286 has a six-byte queue; the reference's effective queue is a surrogate. The
reference's GPL-2 source was inspected for timing behavior and parameters; no
reference source code was copied into the FES implementation.

`GOV_REFERENCE_DIAGNOSTIC` defaults to zero, and the pc386 producer does not
enable it. The implementation is still incomplete: it substitutes costs for
only some instruction forms and suppresses the existing 286 access wait charge
globally when enabled. A later diagnostic models bounded REP chunks, but word
alignment, trap-flag stepping, MMIO and reference timing of interrupted or
faulted strings remain unmeasured. It does not model all prefixes, I/O, faults,
interruptions or 386-only instructions comprehensively. Matching
selected sequences does not establish correct behavior for other instructions
or queue histories. Max, turbo and non-286 behavior require continued
regression protection. This record does not authorize promotion or relaxing
existing timing, seal, lockstep or game gates.

## Reference measurements

The reference was 86Box 6.0 configured as a 12.5 MHz 286 with one wait state.
Figures below are whole-loop clocks inferred from 1,000-iteration blocks over
64 BIOS ticks; they include timer, interrupt and harness overhead. Predictions
were sent to the task transcript before their reference runs. The transcript is
not an independent timestamp authority.

- A sequence and prefix holdout predicted 18, 34, 38, 34, 31 and 31 clocks;
  its three identical outputs from one boot estimated 18.038, 34.036, 38.044,
  34.036, 31.031 and 31.031. The output SHA-256 was
  `ad6129a35fba4b08e63b6200cb4f1bffecedb2ac75266abe72296976a4736850`.
- An independent six-form holdout covering memory CMP, PUSHF/POP, short and
  near JMP, and CALL/RET used byte-identical timed instructions and a different
  loop scaffold. Three identical reference outputs across two boots estimated
  18.045, 18.045, 19.047, 23.042, 25.052 and 50.046 clocks; the frozen
  candidate CPU charged 18.018, 18.016, 19.016, 23.016, 25.016 and 50.019.
  The reference output SHA-256 was
  `f8c89280b719ce980b5fa30ce68b6e6d3a274fcf5a1f6cb9b24a6bb4e5b77349`.
  These are not isolated per-instruction timings or broad opcode coverage.
- A corrected RAM-to-RAM REP MOVS probe checked DS=ES and verified 64 nonzero
  copied bytes. Counts 0, 1, 8 and 64 for MOVSB and 8 for MOVSW estimated
  32.027, 36.046, 64.053, 294.902 and 64.053 clocks respectively. The frozen
  candidate charged 32.006, 36.004, 64.004, 288.004 and 64.004. The longer
  case remains a material discrepancy. Three identical outputs came from one
  reference boot, SHA-256
  `33ad5dcb44a65e6a0b9e106d5b572520266333f02016673f5dc69f6ede2f5a78`.
- A later count-boundary test verified DS=ES and nonzero copy/readback. For
  MOVSB counts 24, 25, 26, 27, 52, 53 and 64, reference estimates were
  127.734, 131.953, 136.038, 144.067, 244.113, 251.088 and 294.902;
  the frozen candidate charged 128.006, 132.004, 136.004, 140.004, 240.004,
  244.004 and 288.004. Three identical outputs came from one boot, SHA-256
  `d21e935ec4516243f9f89db23164833ca683cb6ce96315dd4319b108b1a61092`.
  The predeclared hypothesis that each refetch adds only a two-clock prefix
  charge predicted 142 at count 27, missing the reference by about 2.07 clocks
  (one-block quantization about 0.47). Reference chunking is a plausible
  explanation, but a general correction remains unproven.

A later private chunk-budget model was fixed before a new 16-case byte-string
reference matrix covering MOVS, STOS, LODS and REPNE SCAS. Five outputs across
two boots were identical and contained segment, MOVS-write and STOS-write
controls; all predeclared whole-loop predictions were within the larger of
0.25 clock and twice one-block quantization of the reference. This was a
predeclared diagnostic bound, not a tier threshold. The [chunk matrix](2026-11-16-pc386-286-governor/rep-chunk-matrix/README.md)
retains sources, commands, raw output, predictions and limitations. A first
attempt had prefilled its destination and could not prove a write; it was
stopped and replaced before the corrected run. This improves the surrogate;
it does not qualify unmeasured forms or justify promotion.

An earlier REP run failed to restore ES after reading the DOS interrupt vector
and is invalid for RAM-to-RAM conclusions. An intervening run checked segment
equality but did not verify nonzero data. The measurements above use the later
readback-verified program. The first attempt at the sequence holdout booted a
previous private diagnostic disk: it created three empty H files. A recursive
file comparison found all common files unchanged; FAT metadata may differ.
The failed run and the disk as found afterwards were retained. The successful
rerun explicitly checked the absolute path to its own private disk.

## Simulation evidence

The frozen candidate CPU executable was built with
`GOV_REFERENCE_DIAGNOSTIC=1`, `GOV_N=5` and `GOV_D=3072`; its SHA-256 was
`bc5c8c551496111bbea3c2a79f56563c4d1f8f7769cc2f47e323e6a07b973516`.
Candidate-enabled Bochs lockstep passed 84 directed, 7 unreal-mode, 148 paging,
27 memory and 50 seed-42 random programs. Four sampled IRQ/turbo runs completed
with matching architectural final states. Those tests do not cover the full
corpus or prove reference timing.

With the diagnostic explicitly disabled, a whole-CPU comparison against a
frozen clean-main build matched all 19 benchmark markers, cycle and charge
counts, IRQ traces, run counters and final state for max, 286, 386 and turbo
modes. Its JSON report SHA-256 was
`08d968ba896a015af35bfbfaa0c3235fc2805fec231ccac64400be1d99646ac7`.
An independent original-versus-modified governor-module differential also
matched 262,144 randomized cycles across presets, turbo and frequency
settings. The former used the locked Verilator; the latter used system
Verilator 5.032 and unconstrained synthetic inputs. Neither is a formal,
netlist or physical equivalence proof.

A separate clean private source snapshot enabled the diagnostic by default so
that the machine and standalone CPU gate elaborate the same implementation.
Its initial commit was `5779e60367ab717e9ed06bed3d4df030302d94d1`, based on
`c79f80fadad217a832602073c3a251099558072b`. The unmodified full default
`--netlist` lockstep passed all 941 in-scope files and 1,758,699 corpus tests;
its report SHA-256 was
`9423cf0910ea4c8f618a3daefb2adb493541e5d497e74a869a4d08a88389078d`.
That suite used max pacing, so it does not validate the enabled 286 governor.
The same candidate netlist reproduced the known clean-main seed-41 program-16
exception discrepancy in preset 1. The independently developed oracle fix at
commit `0ed7775c15bfe7f53c0ba16c7303ee03ec44bf5b` was merged into the private
diagnostic snapshot as `7af34f4a143c6823d4adeffbc4a429c899cbaf08`; the same
candidate netlist then passed all 17 seed-41 programs. Its passing log SHA-256
was `40a448263b76fb75fd945070f220c86d8714e27de194bb453ed881d305cf2826`.
The corrected oracle is under separate review. Full post-merge reports must be
regenerated before they can be used by the producer.

The historical default-off candidate at commit `8cbb02309` had these
SHA-256s. The experiments above used this pre-merge source, not the later
merged source:

| File | SHA-256 |
| --- | --- |
| `fes_i386_cpu.sv` | `2fc264ce7a13c5f2397eba965d05785fbaa728505ec068fdfdd8d9678798fbe8` |
| `fes_i386_governor.sv` | `eff364d6b0d778e3f4606bbb506046b9ee152f878d0b6d3d0b4e414eca74f0e1` |
| `fes_i386_pkg.sv` | `5db1ca51d91b92028aac0f6502b683b48671eedb79da8c8f01797fe7ee362a16` |
| `fes_i386_qalign.sv` | `9b537a40f30a401884bb9a61e38326cff00985ebc95e5f51130df095e34b85d1` |

The base merge at `e1f814977c24554172930f71c743930b7e7ac6d9` also brought
x87 changes into the i386 CPU, package and decoder. Its relevant source hashes
are below. The older simulation, netlist and hardware reports do not establish
results for these merged bytes; revalidation is required.

| File at `e1f814977` | SHA-256 |
| --- | --- |
| `fes_i386_cpu.sv` | `edf9bb9fcd286e2e80476357f208ee02784f6b1d61bb7f086a9e041584c1a5e9` |
| `fes_i386_governor.sv` | `eff364d6b0d778e3f4606bbb506046b9ee152f878d0b6d3d0b4e414eca74f0e1` |
| `fes_i386_pkg.sv` | `e86b99e88d4821898f43abb4d13ec8feb1b72cb5dd502080afbd3f324afec974` |
| `fes_i386_qalign.sv` | `9b537a40f30a401884bb9a61e38326cff00985ebc95e5f51130df095e34b85d1` |
| `fes_i386_decode.sv` | `5351d994595be178d1afb5c47106cf46ecdda8fc5b274d48d7ff921e842ecbf0` |
| `fes_i386_x87.sv` | `da35a4d698d8ead84d0c95e26c02e3083ed2c59672d5a8655f46d83877442952` |

The focused check is:

```sh
cd sources/misteross
python3 -m unittest -v tests.test_fes_i386_governor \
  tests.test_fes_i386_reference_diagnostic tests.test_i386_governor_inventory
```

The deterministic instruction-form inventory marks rows unmeasured and helps
select representative tests. Its conservative classifier cross-product
includes illegal or irrelevant combinations; it is not a release gate.

The [retained fixtures](2026-11-16-pc386-286-governor/README.md) include source,
raw output, commands, controls and SHA-256s, plus a script to check their
arithmetic and optionally reproduce the assembled binary hashes. The private
reference disk, ROMs and emulator are not shipped. Full captures, frozen
executables, VM manifests and reports also remain in the task worktree's
ignored `out/dev/pc386-gov-*`, `out/1902-*`, and
`out/dev/1902-governor-diagnostic` directories; those local paths are not a
substitute for published artifacts or exact package evidence. No Quartus
timing/seal, hardware acceptance or game pitch pass is claimed here.
