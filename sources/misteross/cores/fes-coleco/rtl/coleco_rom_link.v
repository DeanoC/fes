// SPDX-License-Identifier: GPL-2.0-or-later
// Blank 8 KiB BIOS at 0x0000-0x1fff and 16 KiB Coleco cartridge at
// 0x8000-0xffff (mirrored). The format-4 ROM map binds every lane: lanes 0-7
// hold the BIOS, lanes 8-23 the cartridge. Column 5 rows 1-14 and 40-49 sit
// outside the native video CRAM rectangle (y 1800-3442) and placement
// (x 5-38, y 23-38) and the v2 expansion socket (x 24-28, y 1-19).
// This ROM map does not use rows 15-39 of column 5. Rows 23-38 lie inside the
// native video placement rectangle. Column 5 does hold M10K sites at rows
// 32-55, which the SMS lanes use, so the gap here is a choice of this map and
// not a device limit.
// Latency matches sg1000_rom_link: two system clocks, CFG_ASYNC_READ=0.
module coleco_rom_link (
    input wire clk,
    input wire [15:0] address,
    output reg [7:0] data,
    input wire [15:0] peek_address,
    output wire [7:0] peek_data
);
    wire [7:0] source_data;
`ifdef VERILATOR
    reg [7:0] memory [0:24575];
    initial begin
        $readmemh("cores/fes-coleco/rtl/coleco_reset_rom.hex", memory, 0, 8191);
        $readmemh("cores/fes-coleco/rtl/coleco_cartridge.hex", memory, 8192, 24575);
    end
    function automatic [14:0] source(input [15:0] a);
        source = a[15:13] == 3'b000 ? {2'b00, a[12:0]} : 15'd8192 + {1'b0, a[13:0]};
    endfunction
    reg [7:0] sim_stage;
    always @(posedge clk) sim_stage <= memory[source(address)];
    assign source_data = sim_stage;
    assign peek_data = memory[source(peek_address)];
`else
    wire [9:0] lane_addr = address[9:0];
    reg [4:0] lane_d;
    always @(posedge clk)
        lane_d <= address[15:13] == 3'b000 ? {2'b00, address[12:10]} : 5'd8 + {1'b0, address[13:10]};
    wire [9:0] q0;
    wire [9:0] q1;
    wire [9:0] q2;
    wire [9:0] q3;
    wire [9:0] q4;
    wire [9:0] q5;
    wire [9:0] q6;
    wire [9:0] q7;
    wire [9:0] q8;
    wire [9:0] q9;
    wire [9:0] q10;
    wire [9:0] q11;
    wire [9:0] q12;
    wire [9:0] q13;
    wire [9:0] q14;
    wire [9:0] q15;
    wire [9:0] q16;
    wire [9:0] q17;
    wire [9:0] q18;
    wire [9:0] q19;
    wire [9:0] q20;
    wire [9:0] q21;
    wire [9:0] q22;
    wire [9:0] q23;
    reg [9:0] selected;
    always @* begin
        case (lane_d)
            5'd0: selected = q0;
            5'd1: selected = q1;
            5'd2: selected = q2;
            5'd3: selected = q3;
            5'd4: selected = q4;
            5'd5: selected = q5;
            5'd6: selected = q6;
            5'd7: selected = q7;
            5'd8: selected = q8;
            5'd9: selected = q9;
            5'd10: selected = q10;
            5'd11: selected = q11;
            5'd12: selected = q12;
            5'd13: selected = q13;
            5'd14: selected = q14;
            5'd15: selected = q15;
            5'd16: selected = q16;
            5'd17: selected = q17;
            5'd18: selected = q18;
            5'd19: selected = q19;
            5'd20: selected = q20;
            5'd21: selected = q21;
            5'd22: selected = q22;
            default: selected = q23;
        endcase
    end
    assign source_data = selected[7:0];
    assign peek_data = 8'hff;
    (* keep, BEL = "MISTRAL_M10K.5.42.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane0 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q0), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.43.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane1 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q1), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.44.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane2 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q2), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.45.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane3 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q3), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.46.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane4 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q4), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.47.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane5 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q5), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.48.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane6 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q6), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.49.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane7 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q7), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.1.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane8 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q8), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.2.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane9 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q9), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.3.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane10 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q10), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.4.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane11 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q11), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.5.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane12 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q12), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.6.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane13 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q13), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.7.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane14 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q14), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.8.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane15 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q15), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.9.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane16 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q16), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.10.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane17 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q17), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.11.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane18 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q18), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.12.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane19 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q19), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.13.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane20 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q20), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.14.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane21 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q21), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.40.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane22 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q22), .ACLR0(1'b0), .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.5.41.0" *)
    MISTRAL_M10K #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0), .INIT(10240'b0)) lane23 (
        .CLK1(clk), .A1ADDR(10'd0), .A1DATA(10'd0), .A1EN(1'b1),
        .B1EN(1'b1), .B1ADDR(lane_addr), .B1DATA(q23), .ACLR0(1'b0), .ACLR1(1'b0)
    );
`endif
    always @(posedge clk)
        data <= source_data;
endmodule
