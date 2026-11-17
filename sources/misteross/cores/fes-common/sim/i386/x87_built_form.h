// SPDX-License-Identifier: MIT
// The ESC forms the RTL's x87 unit builds: the bench's copy of the RTL's
// predicate, fes_i386_pkg's x87_decode over x87_format, which fes_i386_decode's
// `known` and the unit's `unbuilt` both read.
//
// #1768 slice 2 built the interface, the state, the loads, the stores and the
// register forms, slice 3 the arithmetic -- add, subtract, multiply, the
// compares, FRNDINT, FSCALE and FXTRACT -- and slice 4 the divides, the square
// root and the remainders. FBLD and FBSTP (#1958) are built on the DE25
// backend only, so the predicate takes the backend, as the RTL's does: `de25`
// is X87_BACKEND == 1. The transcendentals are not built on either. A form
// the unit does not build stops the RTL as not built, which is how the
// lockstep cannot pass over an instruction the build does not run.
//
// **This copy is not kept in step by inspection.** x87_built_tb.cpp includes
// this header and compares it with the RTL's two functions over all 2^11
// opcodes at both backend values, and scripts/test_fes_i386_lockstep.py runs
// that before any suite and records the count of built forms per backend.
#ifndef FES_I386_X87_BUILT_FORM_H
#define FES_I386_X87_BUILT_FORM_H

#include <cstdint>

inline bool x87_built_form(uint8_t esc, uint8_t modrm, bool de25) {
    const unsigned low = esc & 7u;
    const unsigned rf = (modrm >> 3) & 7u;
    if ((modrm >> 6) != 3) {
        switch (low << 3 | rf) {
            case 1 << 3 | 0: case 5 << 3 | 0: case 3 << 3 | 5:           // FLD m32, m64, m80
            case 3 << 3 | 0: case 7 << 3 | 0: case 7 << 3 | 5:           // FILD m32, m16, m64
            case 1 << 3 | 2: case 5 << 3 | 2: case 3 << 3 | 2: case 7 << 3 | 2:   // FST, FIST
            case 1 << 3 | 3: case 5 << 3 | 3: case 3 << 3 | 7:           // FSTP m32, m64, m80
            case 3 << 3 | 3: case 7 << 3 | 3: case 7 << 3 | 7:           // FISTP m32, m16, m64
            case 1 << 3 | 5: case 1 << 3 | 7: case 5 << 3 | 7:           // FLDCW, FNSTCW, FNSTSW m16
            case 1 << 3 | 6: case 5 << 3 | 6:                            // FNSTENV, FNSAVE
            case 1 << 3 | 4: case 5 << 3 | 4:                            // FLDENV, FRSTOR
                return true;
            // Slice 3's arithmetic and compares and slice 4's divides: D8
            // and DC with a 32- or 64-bit real, DA and DE with a 32- or
            // 16-bit integer, reg fields 0 to 7. Fields 6 and 7 are
            // FDIV and FDIVR.
            case 0 << 3 | 0: case 0 << 3 | 1: case 0 << 3 | 2: case 0 << 3 | 3:
            case 0 << 3 | 4: case 0 << 3 | 5: case 0 << 3 | 6: case 0 << 3 | 7:  // D8
            case 4 << 3 | 0: case 4 << 3 | 1: case 4 << 3 | 2: case 4 << 3 | 3:
            case 4 << 3 | 4: case 4 << 3 | 5: case 4 << 3 | 6: case 4 << 3 | 7:  // DC
            case 2 << 3 | 0: case 2 << 3 | 1: case 2 << 3 | 2: case 2 << 3 | 3:
            case 2 << 3 | 4: case 2 << 3 | 5: case 2 << 3 | 6: case 2 << 3 | 7:  // DA
            case 6 << 3 | 0: case 6 << 3 | 1: case 6 << 3 | 2: case 6 << 3 | 3:
            case 6 << 3 | 4: case 6 << 3 | 5: case 6 << 3 | 6: case 6 << 3 | 7:  // DE
                return true;
            // FBLD and FBSTP (DF /4, /6): built on the DE25 backend only.
            case 7 << 3 | 4: case 7 << 3 | 6:
                return de25;
            default:
                return false;
        }
    }
    switch (low) {
        case 0:  // D8: the arithmetic and compares into ST(0)
            return (modrm & 0xf8) == 0xc0 || (modrm & 0xf8) == 0xc8     // FADD, FMUL
                || (modrm & 0xf8) == 0xd0 || (modrm & 0xf8) == 0xd8     // FCOM, FCOMP
                || (modrm & 0xf8) == 0xe0 || (modrm & 0xf8) == 0xe8     // FSUB, FSUBR
                || (modrm & 0xf8) == 0xf0 || (modrm & 0xf8) == 0xf8;    // FDIV, FDIVR
        case 1:  // D9
            if (modrm == 0xd0 || modrm == 0xe0 || modrm == 0xe1 || modrm == 0xe4 || modrm == 0xe5
                || modrm == 0xf6 || modrm == 0xf7)
                return true;                                     // FNOP, FCHS, FABS, FTST, FXAM, FDECSTP, FINCSTP
            if (modrm == 0xf4 || modrm == 0xfc || modrm == 0xfd) return true;   // FXTRACT, FRNDINT, FSCALE
            if (modrm == 0xf5 || modrm == 0xf8 || modrm == 0xfa) return true;   // FPREM1, FPREM, FSQRT
            if ((modrm & 0xf8) == 0xc0 || (modrm & 0xf8) == 0xc8) return true;  // FLD ST(i), FXCH
            return modrm >= 0xe8 && modrm <= 0xee;               // the constants
        case 2:  // DA
            return modrm == 0xe9;                                // FUCOMPP
        case 3:  // DB
            return modrm == 0xe0 || modrm == 0xe1 || modrm == 0xe2 || modrm == 0xe3 || modrm == 0xe4;
        case 4:  // DC: the same arithmetic into ST(i)
            return (modrm & 0xf8) == 0xc0 || (modrm & 0xf8) == 0xc8
                || (modrm & 0xf8) == 0xe0 || (modrm & 0xf8) == 0xe8
                || (modrm & 0xf8) == 0xf0 || (modrm & 0xf8) == 0xf8;   // FDIVR, FDIV ST(i)
        case 5:  // DD
            return (modrm & 0xf8) == 0xc0 || (modrm & 0xf8) == 0xd0 || (modrm & 0xf8) == 0xd8
                || (modrm & 0xf8) == 0xe0 || (modrm & 0xf8) == 0xe8;   // ... and FUCOM, FUCOMP
        case 6:  // DE: the popping forms, and FCOMPP
            return modrm == 0xd9
                || (modrm & 0xf8) == 0xc0 || (modrm & 0xf8) == 0xc8
                || (modrm & 0xf8) == 0xe0 || (modrm & 0xf8) == 0xe8
                || (modrm & 0xf8) == 0xf0 || (modrm & 0xf8) == 0xf8;   // FDIVRP, FDIVP
        case 7:  // DF
            return modrm == 0xe0;                                // FNSTSW AX
        default:
            return false;
    }
}

#endif
