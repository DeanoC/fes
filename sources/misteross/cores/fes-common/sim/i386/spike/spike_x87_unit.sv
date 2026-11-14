// SPDX-License-Identifier: MIT
// The real x87 unit (rtl/i386/fes_i386_x87.sv) in the timing-spike envelope,
// so its area and slack can be measured before the CPU side is built
// (#1768 slice 2). scripts/quartus_x87_spike.py --unit fits it with the
// seal's settings, as it fits the slice-1 spike. Nothing here ships in a
// core.
//
// Every input comes from the envelope's noise register, so nothing is
// constant, and every output folds into a signature that drives LED0, so
// nothing is trimmed. REGS selects whether the eight physical registers'
// readout (regs_o, which the lockstep reads through the CPU's arch_* ports
// and the sealed machine leaves unused) is part of the signature: with
// REGS 0 Quartus trims those wires, as the seal does.
module spike_x87_unit #(
    parameter int REGS = 1
) (
    input  wire  FPGA_CLK1_50,
    input  wire  KEY0_N,
    output logic LED0
);
    wire clk = FPGA_CLK1_50;
    logic         reset;
    logic [255:0] noise;
    logic         hold, nx, error, latched, busy_o, clr_pointers;
    logic [31:0]  sval;
    logic [15:0]  cw_o, sw_o, tw_o;
    logic [10:0]  opcode_o;
    logic [639:0] regs_o;
    logic [63:0]  signature, fold;
    spike_stimulus #(.WIDTH(256)) stimulus (.clk, .key0_n(KEY0_N), .reset, .noise);
    fes_i386_x87 dut (
        .clk, .reset, .present(noise[0]), .f0(noise[1] && noise[2]), .f1(noise[3] && noise[4]),
        .error, .latched,
        .iss(noise[5]), .iss_wait(noise[6]), .iss_op(noise[17:7]), .iss_fmt(noise[20:18]),
        .iss_first(noise[21]), .iss_fire(noise[22] && noise[21]), .iss_last(noise[29]),
        .iss_part(noise[28:23]),
        .hold, .sval, .nx,
        .wb(noise[29]), .wb_last(noise[30]), .wb_val(noise[62:31]), .wb_part(noise[68:63]),
        .flush(noise[69] && noise[70]),
        .cw_o, .sw_o, .tw_o, .opcode_o, .busy_o, .clr_pointers, .regs_o
    );
    always_comb begin
        fold = {sval, cw_o, sw_o} ^ {tw_o, opcode_o, 31'd0, hold, nx, error, latched, busy_o, clr_pointers};
        if (REGS != 0)
            for (int n = 0; n < 10; n++) fold = fold ^ regs_o[64*n +: 64];
    end
    always_ff @(posedge clk) begin
        signature <= {signature[62:0], signature[63]} ^ fold;
        LED0 <= ^signature;
    end
endmodule
