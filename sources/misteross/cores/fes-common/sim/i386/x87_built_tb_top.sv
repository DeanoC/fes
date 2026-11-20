// SPDX-License-Identifier: MIT
// The RTL's answer to "is this ESC form built", for sim/i386/x87_built_tb.cpp
// to compare with the lockstep bench's C++ copy (x87_built_form.h) over every
// x87 opcode.
//
// It is fes_i386_pkg's two functions composed exactly as their two callers
// compose them: fes_i386_decode's `known` takes x87_format of the ESC's low
// bits and the ModRM reg field and hands it to x87_decode with the opcode,
// and the unit's `unbuilt` is x87_decode of the same opcode and the format
// the decode gave it.
module x87_built_tb_top (
    input  logic [10:0] op,          // {ESC's low three bits, the ModRM byte}
    output logic        built
);
    import fes_i386_pkg::*;
    assign built = x87_decode(op, x87_format(op[10:8], op[5:3])) != X87_UNIMPL;
endmodule
