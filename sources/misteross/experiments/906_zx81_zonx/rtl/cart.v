// Bi-Pak Zon X-81 AY register file. I/O decode matches (port & 008F).
// Select at xxDF/xxCF, data at xx0F. No Spectrum readback.
// Select cell B1ADDR is live plug_addr LUTs that stay at 10'h0DF on both
// ports (kit-proven). Data cell A1ADDR/B1ADDR splice qsel[3:0] into the
// free nibble so two-cycle OUT (C),A at xx0F hits 16 locations.
module cart (
    input wire FPGA_CLK1_50,
    input wire [15:0] plug_addr,
    input wire [7:0] plug_wdata,
    input wire plug_mem_we,
    input wire plug_io_we,
    input wire plug_io_rd,
    output wire [9:0] plug_rdata
);
    wire sel_reg = plug_io_we && ((plug_addr[7:0] & 8'h8f) == 8'h8f);
    wire sel_data = plug_io_we && ((plug_addr[7:0] & 8'h8f) == 8'h0f);
    wire sel_read = plug_io_rd && (
        ((plug_addr[7:0] & 8'h8f) == 8'h8f) ||
        ((plug_addr[7:0] & 8'h8f) == 8'h0f)
    );
    (* keep *) wire unused_mem = plug_mem_we ^ sel_read;
    (* keep *) wire [9:0] sel_rd = {
        plug_addr[9] & plug_addr[7],
        plug_addr[8] & plug_addr[7],
        plug_addr[7] | plug_addr[0],
        plug_addr[6] | plug_addr[0],
        plug_addr[5],
        plug_addr[4] | plug_addr[0],
        plug_addr[3:0]
    };
    wire [9:0] qsel;
    wire [9:0] q;
    wire [3:0] ay_sel = qsel[3:0];
    (* keep *) wire [8:0] data_a = {plug_addr[9:8], ay_sel, plug_addr[3:1]};
    (* keep *) wire [9:0] data_b = {plug_addr[9:8], ay_sel, plug_addr[3:0]};

    (* keep, BEL = "MISTRAL_M10K.26.2.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) ay_sel_cell (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN(sel_reg),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(sel_rd),
        .B1DATA(qsel),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    (* keep, BEL = "MISTRAL_M10K.26.1.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) ay_regs (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(data_a),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN(sel_data),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(data_b),
        .B1DATA(q),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );

    assign plug_rdata = {2'b00, q[7:0]};
endmodule
