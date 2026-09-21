// Quicksilva QS Character Board. CPU window 8400-87FF, 1 KiB.
// Inverse codes use 8600-87FF; the ULA inversion stage still applies.
// Mixed-width 512x20 writes with A1BE, 1024x10 reads.
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
    wire sel = (plug_addr[15:10] == 6'h21);
    wire [9:0] q;
    assign plug_rdata = sel ? {2'b00, q[7:0]} : 10'd0;

    (* keep, BEL = "MISTRAL_M10K.26.1.0" *)
    MISTRAL_M10K #(
        .CFG_ABITS(9),
        .CFG_DBITS(20),
        .CFG_RD_ABITS(10),
        .CFG_RD_DBITS(10),
        .CFG_MIXED_WIDTH(1),
        .CFG_DUAL_CLOCK(1),
        .CFG_BYTE_ENABLE(1)
    ) slot_cell (
        .CLK1(FPGA_CLK1_50),
        .CLK2(FPGA_CLK1_50),
        .A1ADDR(plug_addr[9:1]),
        .A1DATA({2'b00, plug_wdata, 2'b00, plug_wdata}),
        .A1EN(sel & plug_mem_we),
        .A1BE(plug_addr[0] ? 2'b10 : 2'b01),
        .B1ADDR(plug_addr[9:0]),
        .B1DATA(q),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
endmodule
