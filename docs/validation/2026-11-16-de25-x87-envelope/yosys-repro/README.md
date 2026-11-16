# The locked Yosys silently miscompiles the DE25 x87 backend

Found 16 Nov 2026 by running the x87 unit bench's `--netlist` mode with the
DE25 arithmetic backend selected. The RTL simulation passes 763 of 763 cases;
the netlist of the same design fails 240 of them with wrong arithmetic. The
Yosys run reports **warnings, not errors**, so a flow that did not simulate
the netlist would have taken it as a success.

Yosys: the i386 lock's, `DeanoC/yosys` commit `e240a465`
(`cores/fes-common/rtl/i386/toolchain.lock`), from its authenticated slot in
the shared toolchain cache. Binary SHA-256 is recorded in the unit bench's
`report.json` for the run that found it.

## The defect

Reading or writing a **packed-struct member of an unpacked-array element** —
`stage[1].value <= in`, `out <= stage[2].value` — does not address the array
element. Yosys implicitly declares a fresh identifier spelled
`\stage[1].value`, which nothing drives, so the member reads as zero and the
array is never written.

    repro.sv:14: Warning: Identifier `\stage[1].value' is implicitly declared.
    repro.sv:15: Warning: Identifier `\stage[1].flag' is implicitly declared.
    repro.sv:17: Warning: Identifier `\stage[2].value' is implicitly declared.

In `repro.netlist.v` the consequence is visible directly:

    assign ok_out = 8'h00;
    assign out[7:1] = 7'h00;

Both outputs should follow `in` through the two pipeline stages.

**The workaround is a packed array.** The locked Yosys handles member reads
and writes against a *packed* array of packed structs correctly:

    item_t u_stage [1:2];   // broken: `\u_stage[1].value' implicitly declared
    item_t [1:2] p_stage;   // correct

`repro.sv` holds both forms in one module, so the netlist shows `p_out`
driven from `p_stage[8:1]` while `u_out` is left at zero. `fes_x87_fast.sv`
uses the packed form, and with it the DE25 configuration passes the unit
bench's netlist mode 763 of 763.

For the record of what does **not** work: reading the whole element into a
wire first (`assign copy2 = stage[2];`) does not rescue it once any member
write to the same array is present — `ok_out` is zero too. That was tried
first, believed before being tested, and reverted.

## Reproducing

    yosys -s repro.ys
    yosys -s packed-vs-unpacked.ys

from this directory, with the locked Yosys. `y.log` and `repro.netlist.v` are
the outputs of the first; `packed-vs-unpacked.log` and
`packed-vs-unpacked.netlist.v` of the second.

`repro.sv` is the defect on its own. `packed-vs-unpacked.sv` holds **both**
forms in one module, which is what establishes the workaround rather than
asserting it: the netlist drives `p_out <= p_stage[8:1]` from the packed array
and leaves `u_out` at zero from the unpacked one, and only the unpacked
identifiers are reported as implicitly declared.

## What this does and does not block

It does **not** block the DE25 387. The DE25 flow is Quartus Pro reading
SystemVerilog directly, with no Yosys in it, and Quartus Pro compiles the same
RTL correctly — PR #1892's 200 MHz four-corner fit is evidence of that.

It does mean, in terms, that **the locked-Yosys netlist bench is a DE10-only
gate**. The DE10 configuration passes it, 763 of 763, which is what protects
PR #1889. The DE25 configuration cannot be checked through it, and nobody
should later cite a green netlist bench as covering the DE25 path.

The dangerous part is not the miscompile but the **false coverage**: a silent
miscompile is the worst kind of gate failure, because the gate reports
success. The project rule that a toolchain workaround gets a fork issue and a
locked-binary repro exists for this, and this directory is that repro. The
issue is [DeanoC/yosys#79](https://github.com/DeanoC/yosys/issues/79).
