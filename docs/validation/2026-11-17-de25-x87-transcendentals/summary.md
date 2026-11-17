# x87 transcendentals and packed decimal on the DE25 backend

The second of the three DE25 387 steps (envelope, then transcendentals, then
remainder). Closes [#1958](https://github.com/DeanoC/fes/issues/1958), under
the x87 tracker [#1768](https://github.com/DeanoC/fes/issues/1768).

FSIN, FCOS, FSINCOS, FPTAN, FPATAN, F2XM1, FYL2X, FYL2XP1, FBLD and FBSTP are
built for `X87_BACKEND = 1`. With `X87_BACKEND = 0` all ten still stop in
decode as not built, and the DE10 stays identity-off.

**Host simulation and a virtual-pin machine fit only.** No DE25 board shell,
seal, package or kit run exists for this configuration and none was made.
No real 387 was consulted.

Where to read what:

| File | What it is |
| --- | --- |
| [method.md](method.md) | The method and pass rules, committed before any run, with three dated amendments that say what had been seen |
| [rules.md](rules.md) | Each instruction's rules as read from the 80387 manual, with the page, and what the manual leaves open, measured |
| [bound.md](bound.md) | The two bounds **proposed** for Deano. Not agreed |
| [demand.md](demand.md) | Generated: what the census titles ask of this step |
| [references.md](references.md) | Generated: the references against each other, and the model against them. No RTL |
| [results.md](results.md) | Generated: the unit bench and the engine on the final head |
| [lockstep.md](lockstep.md) | Generated: lockstep against the Bochs oracle on the final head, against the merge-base baseline |
| [fits.md](fits.md) | Generated: the machine fits against the bands committed before them |

Every figure in the generated files is read out of a committed report under
[evidence/](evidence/) or [fits/](fits/) by [generate.py](generate.py); this
file states no figure of its own that those do not carry.

## What is settled and what is not

- **Settled by Deano (17 Nov 2026):** the transcendentals follow the 387's
  method, not Bochs bit for bit.
- **Not settled: the numeric bounds.** [bound.md](bound.md) proposes 4
  representable values against the host x87 and the brackets, and 2^21
  against the oracle as a sanity check only. Every run here used those
  values and its report says "proposed, not agreed". **No transcendental
  result gate is claimed as passed**: the distributions are in
  [results.md](results.md) and [lockstep.md](lockstep.md).
- **Not settled: real-387 captures.** None exists here. "Follows the 387"
  means "follows the manual"; the build hosts are modern x87 units.

## What was built

**The eight transcendentals are a microprogram.** `scripts/x87_trans_ucode.py`
is the one place the kernels are written: short sequences of the backend's own
add, subtract, multiply and divide over constants in memory. From that one
script come the engine's two ROMs, an exact integer model of the
microprogram, and the model's vectors.

- **The engine**, `fes_x87_trans`, runs the microprogram: sixteen
  temporaries, an index, an exponent, a few flags and a one-level link. It
  drives the unit's own `fes_x87_core`, one operation at a time. It adds no
  multiplier and no DSP block.
- **It has no exception logic.** Every intermediate is rounded to nearest at
  64 bits and stays in the normal range by construction (asserted in every
  simulation). The instruction's **last** operation is handed back unrounded
  to the unit's own `A_RND`, so the rounding under the control word,
  overflow, underflow and the inexact flag are the code the arithmetic
  already uses and the oracle already verifies.
- **The unit answers everything else itself**: the stack faults, the
  unsupported formats, the NaNs, the zeros, the infinities, the 2^63 range
  and C2, FPTAN's pushed 1.0, FSINCOS's push, the two-operand forms' pop.
- **What follows the 387 and what does not.** The reduction is by a pi/4 of
  67 significant bits, as the manual says (rules.md G2); the ranges, the
  special cases and the error bound are the manual's. The kernels are
  table-and-polynomial, **not the 387's CORDIC**, whose microcode is not
  published and cannot be observed. Nothing is ported from Bochs.

**FBLD and FBSTP** are sequencer arms guarded by `X87_BACKEND == 1`: shifts
and adds, no DSP. Both are memory forms and are in all three decode tables;
FBSTP has a store routine of its own so that its data pointer is the
operand's first byte.

**One predicate says what is built**, and it takes the backend:
`x87_decode` and `x87_format` gained the argument, `fes_i386_decode` the
parameter. The bench's C++ copy is compared with it over all 2,048 x87
opcodes at both parameter values in every lockstep run.

## How it was checked

Three layers, the first two exact:

1. **The engine against the model, bit for bit**, with no tolerance
   (`scripts/test_fes_x87_trans_engine.py`).
2. **The unit against the model's instruction-level vectors, bit for bit**,
   in all four rounding modes, inside the unit bench.
3. **The unit against the references, within the proposed bounds** for a
   finite result and exactly for everything else: the host x87 in the unit
   bench, the Bochs oracle in lockstep.

The model is what is compared with the mathematical result, over far more
operands than a simulation reaches: [references.md](references.md) has its
error in units of the last place and its distance from both build hosts.

Each new check was made to fail on purpose before it was relied on. The
engine bench caught an injected rounding fault. The unit bench fails 2,602
cases with the bound unset or at 0, and exactly one at 1. The lockstep
agent's injections (a result one past the bound, the bound unset, the
resynchronisation skipped, a form leaked into the DE10 set, C1 never gated)
each failed the named cases. The packed decimal injections are listed with
what caught each in the FBLD/FBSTP commits' messages.

## What the references turned out to be

This shaped the method more than anything else, and all of it was measured
before any RTL existed ([references.md](references.md)):

- **The Bochs oracle's transcendental values are inaccurate.** Against
  QEMU's brackets of the mathematical result the host x87 returned a
  neighbour of it on every row and the oracle missed on hundreds, by up to
  hundreds of thousands of representable values. So against the oracle the
  value compare is a sanity check, and the accuracy check is the host and
  the brackets. Flags, C0, C2 and C3 agree between oracle and host
  everywhere, and that is what keeps lockstep's force.
- **The two build hosts agree with each other**, an Intel and an AMD part,
  to within one representable value and in every flag, including beside
  multiples of pi/2. They disagree on C1 for bit-identical results, which is
  why C1 is gated only on a stack fault or an exact result.
- **The model agrees with the hosts beside multiples of pi/2 to the bit**,
  which is a measurement of the hosts against the manual's 67-bit pi/4
  rather than an assumption about it.

## Where the unit follows the manual against the references

Each is a `divergences.toml` entry with a directed case that asserts the
unit's side, and each is counted in the lockstep report:

- **A trigonometric form of an infinity sets C2** (PRM Table 3-10). The
  oracle and both hosts leave it clear.
- **FYL2XP1 of an operand more negative than -1 is invalid** (PRM Table
  3-10); at exactly -1, which the manual leaves undefined, the unit treats
  it as the logarithm of zero. The references return a finite value.
- **A tiny result raises UE with PE** (PRM 3.2.5.2, 3.2.6): the hosts do,
  and the oracle does not for some denormal operands.
- **Two more where the oracle's flags come from its own working**: UE for
  FPATAN from its quotient where the result is beside pi, and DE in the
  two-operand forms with both operands normal. The unit equals the host on
  every measured instance.

## Where the design was changed to fit the references

Recorded because it is the kind of change that should be visible
([method.md](method.md), amendment 3): below 2^-68 a trigonometric form now
returns its operand exactly (FCOS 1.0) in every rounding mode, as both hosts
and the oracle do. It first returned the mathematically directed result,
which crossed the denormal boundary and changed a tag and the underflow
flag in nine lockstep cases.

## The duplicate backend RTL

`sources/misteross/boards/de25nano/x87/rtl/` holds five files that **nothing
reads**: the standalone build and bench, and the machine, all read
`cores/fes-common/rtl/i386/`. Two of the five differ from the copies that
are built, one of them by the missing workaround for DeanoC/yosys#79.
Nothing keeps them in sync. **Proposed, not done here:** delete the
directory's `rtl/` in a PR of its own, since a stale copy of a miscompile
workaround is the kind of thing that gets edited by mistake.

## Found on the way, and where each went

- **A pre-existing defect**, not this work's: with the identity on, the x87
  data pointer after a multi-chunk store is the last chunk's address. It is
  [#1877](https://github.com/DeanoC/fes/issues/1877) already; FBSTP avoids
  it with its own store routine and the new lockstep programs do not use
  the affected stores.
- **A locked-Yosys parse defect**: a declaration with a wildcard-imported
  type is rejected after a procedural loop with its own loop variable. It is
  an error, not a miscompile. The locked-binary repro is on
  [DeanoC/yosys#74](https://github.com/DeanoC/yosys/issues/74), of which it
  is a second form; the RTL avoids it by keeping the loop in a function.
- **The CPU-level locked-Yosys netlist accepted the engine with no
  workaround.**

## What is not done

- **The bounds are proposed, not agreed**, so no transcendental result gate
  is claimed.
- **QEMU's `test-i386-fbstp.c` was not run as such.** Its seventeen cases'
  operands and expectations are covered by the unit bench's own packed
  decimal cases against the host, written from the instruction's rules; the
  file itself was not fetched and compared, which method.md's amendment 1
  said would be done. QEMU's four transcendental tables **are** run on the
  unit, in the unit bench, under the proposed bound
  ([results.md](results.md)); QEMU has no table for FSIN, FCOS, FSINCOS or
  FPTAN.
- **The 2^63 range cases** are in the unit bench and the lockstep suite;
  there is no separate report of them.
- **Lockstep is masked only**: the bench drives no IRQ 13. The unmasked
  cases are in the unit bench, against the host.
- **The rules-to-cases listing** that method.md promised (each rule in
  rules.md with the cases that cover it) is not generated.
- **No clock ladder.** The fits are at 61.44 MHz only.
- **No DE25 board shell, seal, package or kit run.**
- **The remainder on the backend, the governor's 387 clocks, the identity's
  default and sharing the two 64x64 multipliers** are later steps.
