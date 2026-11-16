// SPDX-License-Identifier: GPL-2.0-or-later
// Quartus build of the ZX81 machine ROM lanes (scripts/build_fes_zx81_quartus.py).
// The Quartus producer compiles this file in place of rtl/zx81_rom_link.v and
// keeps that file's interface, lane shape and read pipeline: 1024x10 lanes,
// address [12:10] selects the lane and [9:0] the word, ZX81_ROM_4K (the ZX80)
// builds the low four lanes and ties the upper four to zero.
//
// Each lane is an altsyncram ROM whose init file is zx81_rom_lane<N>.mif. The
// producer writes those files all zero, so the sealed RBF holds blank lanes,
// and places lane N at M10K_X5_Y<73+N>_N0, the BEL rtl/zx81_rom_link.v gives
// nextpnr. The kit then splices the user's ROM into the same CRAM bits, the
// m10k-1024x10-v1 encoding of scripts/rom_map.py.
//
// The read is the same two stages as rtl/zx81_rom_link.v: the lane registers
// its address on clk and the bank selector is registered on that edge, the
// lane mux is combinational, and the output register is the second stage.
// No ROM bytes are in this file or in the RBF.
`include "zx81_model.vh"

module zx81_rom_lane_quartus #(
    parameter INIT_FILE = ""
) (
    input  wire       clk,
    input  wire [9:0] address,
    output wire [9:0] q
);
    altsyncram #(
        .operation_mode("ROM"),
        .width_a(10),
        .widthad_a(10),
        .numwords_a(1024),
        .outdata_reg_a("UNREGISTERED"),
        .address_aclr_a("NONE"),
        .outdata_aclr_a("NONE"),
        .ram_block_type("M10K"),
        .init_file(INIT_FILE),
        .intended_device_family("Cyclone V"),
        .lpm_type("altsyncram")
    ) rom (
        .clock0(clk),
        .address_a(address),
        .q_a(q)
    );
endmodule

module zx81_rom_link (
    input  wire        clk,
    input  wire [12:0] address,
    output reg  [7:0]  data
);
    wire [9:0] lane_addr = address[9:0];
    wire [9:0] q0, q1, q2, q3, q4, q5, q6, q7;
    reg [9:0] q;
`ifdef ZX81_ROM_4K
    reg [1:0] bank_d;
    always @(posedge clk) bank_d <= address[11:10];
    always @* begin
        case (bank_d)
            2'd0: q = q0;
            2'd1: q = q1;
            2'd2: q = q2;
            default: q = q3;
        endcase
    end
`else
    reg [2:0] bank_d;
    always @(posedge clk) bank_d <= address[12:10];
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
`endif
    zx81_rom_lane_quartus #(.INIT_FILE("zx81_rom_lane0.mif")) lane0 (.clk(clk), .address(lane_addr), .q(q0));
    zx81_rom_lane_quartus #(.INIT_FILE("zx81_rom_lane1.mif")) lane1 (.clk(clk), .address(lane_addr), .q(q1));
    zx81_rom_lane_quartus #(.INIT_FILE("zx81_rom_lane2.mif")) lane2 (.clk(clk), .address(lane_addr), .q(q2));
    zx81_rom_lane_quartus #(.INIT_FILE("zx81_rom_lane3.mif")) lane3 (.clk(clk), .address(lane_addr), .q(q3));
`ifdef ZX81_ROM_4K
    assign q4 = 10'd0;
    assign q5 = 10'd0;
    assign q6 = 10'd0;
    assign q7 = 10'd0;
`else
    zx81_rom_lane_quartus #(.INIT_FILE("zx81_rom_lane4.mif")) lane4 (.clk(clk), .address(lane_addr), .q(q4));
    zx81_rom_lane_quartus #(.INIT_FILE("zx81_rom_lane5.mif")) lane5 (.clk(clk), .address(lane_addr), .q(q5));
    zx81_rom_lane_quartus #(.INIT_FILE("zx81_rom_lane6.mif")) lane6 (.clk(clk), .address(lane_addr), .q(q6));
    zx81_rom_lane_quartus #(.INIT_FILE("zx81_rom_lane7.mif")) lane7 (.clk(clk), .address(lane_addr), .q(q7));
`endif
    always @(posedge clk)
        data <= q[7:0];
endmodule
