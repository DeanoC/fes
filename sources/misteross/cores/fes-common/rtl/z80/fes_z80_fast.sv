// SPDX-License-Identifier: MIT
// Documented Z80 ISA, transaction interface; one ready transaction per enabled
// clock. Undefined encodings trap. No original pin/T-state timing is implied.
module fes_z80_fast (
    input logic clk, reset, enable, int_n, nmi_n,
    input logic bus_ready,
    input logic [7:0] bus_rdata,
    output logic bus_req,
    output logic [2:0] bus_kind,
    output logic [3:0] bus_extra,
    output logic [4:0] bus_delay,
    output logic [15:0] bus_addr,
    output logic [7:0] bus_wdata,
    output logic [15:0] refresh_addr,
    output logic halted, illegal, retired,
    output logic [15:0] retire_pc,
    output logic [15:0] debug_pc, debug_sp, debug_af, debug_bc, debug_de,
                        debug_hl, debug_ix, debug_iy, debug_ir,
    output logic [2:0] debug_iff
);
    wire interrupt_blocked=1'b0, bus_resume=1'b0;
    fes_z80_engine #(.NMOS(1'b0)) cpu (.*);
endmodule
