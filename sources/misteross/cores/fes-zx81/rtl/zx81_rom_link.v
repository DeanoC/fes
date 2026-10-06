// SPDX-License-Identifier: GPL-2.0-or-later
// Eight empty 1024x10 lanes for the ZX81 machine ROM.
// Address [12:10] selects the block and [9:0] selects the lane.
// The sealed OSS image leaves these lanes empty. Launch splices BASIC
// into the RAM muxes; this file does not read zx8x.hex.
//
// Cyclone V M10K cannot do a true asynchronous read, so each lane registers
// its address on clk (CFG_ASYNC_READ=0, live CLK1, B1EN held high). The bank
// selector is registered on that same edge and the lane mux is combinational,
// then the output register supplies the second stage. CPU-facing latency is
// two system clocks.
//
// zx81_machine_clock spaces ce_6m5 by eight or nine 52.224 MHz clocks and
// toggles the Z80 edge every other pulse, so a half-cycle is about sixteen
// host clocks. T80pa samples DI on CEN_n at T3, and the address is held
// across that window. Two clocks of ROM latency are stable at the sample.
// During /RFSH the glyph address is held for that same half-cycle while
// rfsh_chr is rewritten every clk_sys, so the first two clocks of stale
// data are overwritten before shifter_start. Do not fold /RFSH into the
// CPU mem_out mux: that is still a combinational loop through T80.
// The tape loader substitutes tape_loader_patch and does not read these
// lanes for the patched bytes. The Verilator model uses the same two stages.

module zx81_rom_link (
    input  wire        clk,
    input  wire [12:0] address,
    output reg  [7:0]  data
);
    wire [7:0] source_data;
`ifdef VERILATOR
    reg [7:0] memory [0:8191];
    reg [7:0] sim_stage;
    always @(posedge clk) sim_stage <= memory[address];
    assign source_data = sim_stage;
`else
    wire [9:0] lane_addr = address[9:0];
    reg [2:0] bank_d;
    always @(posedge clk) bank_d <= address[12:10];
    wire [9:0] q0, q1, q2, q3, q4, q5, q6, q7;
    reg [9:0] q;
    always @* begin
        case (bank_d)
            3'd0: q = q0;
            3'd1: q = q1;
            3'd2: q = q2;
            3'd3: q = q3;
            3'd4: q = q4;
            3'd5: q = q5;
            3'd6: q = q6;
            default: q = q7;
        endcase
    end
    assign source_data = q[7:0];
    (* keep, BEL = "MISTRAL_M10K.5.73.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane0 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q0), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.74.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane1 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q1), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.75.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane2 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q2), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.76.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane3 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q3), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.77.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane4 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q4), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.78.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane5 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q5), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.79.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane6 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q6), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.80.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane7 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q7), .ACLR0(1'b0), .ACLR1(1'b0)
    );
`endif
    always @(posedge clk)
        data <= source_data;
endmodule
