// S1c: eval() for a Verilator --timing model driven by a testbench that only toggles the
// clock and calls eval(). Intel's RAM model (common_28nm_ram_pulse_generator in
// altera_lnsim.sv) makes its read and write pulses from 1 ps and 2 ps delays, so nothing
// is read or written unless simulation time moves between clock edges. Each call
// evaluates, runs the delayed events that fall in the next S1C_SETTLE_PS, then moves
// time on by S1C_STEP_PS. The testbench copy is made by replacing `<dut>.eval()` with
// `s1c_eval(<dut>)`; nothing else in it changes.
#pragma once
#include "verilated.h"

#ifndef S1C_STEP_PS
#define S1C_STEP_PS 5000
#endif
#ifndef S1C_SETTLE_PS
#define S1C_SETTLE_PS 1000
#endif

template <class Model>
static inline void s1c_eval(Model &dut) {
    VerilatedContext *const context = dut.contextp();
    const vluint64_t start = context->time();
    dut.eval();
    while (dut.eventsPending() && dut.nextTimeSlot() <= start + S1C_SETTLE_PS) {
        context->time(dut.nextTimeSlot());
        dut.eval();
    }
    context->time(start + S1C_STEP_PS);
}
