// Sinclair 16K pack window at 4000-7FFF. Sixteen BEL-locked slot cells.
// Mixed-width 512x20 writes with A1BE, 1024x10 reads. Signature stays in the shell.
module cart (
    input wire FPGA_CLK1_50,
    input wire [15:0] plug_addr,
    input wire [7:0] plug_wdata,
    input wire plug_mem_we,
    input wire plug_io_we,
    input wire plug_io_rd,
    output wire [9:0] plug_rdata
);
    (* keep *) wire unused_io = plug_io_we ^ plug_io_rd;
    wire sel = (plug_addr[15:14] == 2'b01);
    wire [3:0] bank = plug_addr[13:10];
    wire [9:0] q0, q1, q2, q3, q4, q5, q6, q7;
    wire [9:0] q8, q9, q10, q11, q12, q13, q14, q15;
    (* keep, BEL = "MISTRAL_M10K.26.1.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell0 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd0))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q0),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.2.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell1 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd1))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q1),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.5.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell2 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd2))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q2),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.6.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell3 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd3))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q3),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.9.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell4 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd4))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q4),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.10.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell5 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd5))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q5),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.13.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell6 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd6))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q6),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.14.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell7 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd7))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q7),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.17.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell8 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd8))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q8),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.18.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell9 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd9))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q9),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.21.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell10 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd10))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q10),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.22.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell11 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd11))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q11),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.25.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell12 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd12))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q12),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.26.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell13 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd13))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q13),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.29.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell14 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd14))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q14),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    (* keep, BEL = "MISTRAL_M10K.26.30.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell15 (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN((sel & plug_mem_we & (bank == 4'd15))),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q15),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    wire [9:0] qsel =
        (bank == 4'd0) ? q0 :
        (bank == 4'd1) ? q1 :
        (bank == 4'd2) ? q2 :
        (bank == 4'd3) ? q3 :
        (bank == 4'd4) ? q4 :
        (bank == 4'd5) ? q5 :
        (bank == 4'd6) ? q6 :
        (bank == 4'd7) ? q7 :
        (bank == 4'd8) ? q8 :
        (bank == 4'd9) ? q9 :
        (bank == 4'd10) ? q10 :
        (bank == 4'd11) ? q11 :
        (bank == 4'd12) ? q12 :
        (bank == 4'd13) ? q13 :
        (bank == 4'd14) ? q14 : q15;
    assign plug_rdata = sel ? {2'b00, qsel[7:0]} : 10'd0;
endmodule
