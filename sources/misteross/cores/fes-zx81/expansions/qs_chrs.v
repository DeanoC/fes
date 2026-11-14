// SPDX-License-Identifier: GPL-2.0-or-later
// Quicksilva QS Character Board on the freeze-scaffold Z80-like edge.
// CPU window 8400-87FF (1 KiB). ROMCS is asserted in that window so the
// onboard ROM alias at 8000-9FFF does not win. The machine maps ULA /RFSH
// character fetches onto this same window (char[6:0] and row[2:0]).
// The board RAM starts empty and holds no glyph data. Like the real
// board's character switch, which is off at power-up, the cart leaves
// character fetches to the machine's own ROM until the CPU first writes
// to the window; from then on every glyph comes from the board RAM. CPU
// reset turns the characters off again; the RAM keeps its contents.
`include "zx81_bus_pack.vh"
module cart (
    input wire FPGA_CLK1_50,
    input wire [`ZX81_BUS_REQ-1:0] plug_addr,
    output wire [`ZX81_BUS_RSP-1:0] plug_rdata
);
    wire [15:0] cpu_a = plug_addr[`ZX81_BUS_A];
    wire [7:0] cpu_d = plug_addr[`ZX81_BUS_DWR];
    wire mreq_n = plug_addr[`ZX81_BUS_MREQ_N];
    wire rd_n = plug_addr[`ZX81_BUS_RD_N];
    wire wr_n = plug_addr[`ZX81_BUS_WR_N];
    wire rfsh_n = plug_addr[`ZX81_BUS_RFSH_N];
    wire reset_n = plug_addr[`ZX81_BUS_RESET_N];
    wire sel = (cpu_a[15:10] == 6'h21) && !mreq_n;
    wire [7:0] read_data;
    // The glyph byte is a registered M10K read. ROMCS and DSEL use that
    // same edge so the response word describes one request. WAIT stays 0;
    // a cart that drives it has to register that bit on this stage too.
    // A /RFSH fetch is claimed only once characters are on. An unclaimed
    // fetch leaves ROMCS low, so the machine shows the ROM glyph instead.
    reg romcs_q = 1'b0, dsel_q = 1'b0, chars_on = 1'b0;
    always @(posedge FPGA_CLK1_50) begin
        romcs_q <= sel && (rfsh_n || chars_on);
        dsel_q <= sel && !rd_n;
        if (!reset_n)
            chars_on <= 1'b0;
        else if (sel && !wr_n)
            chars_on <= 1'b1;
    end
`ifdef SYNTHESIS
    // Same TDP write-enable overlay as the 16K pack. Mixed-width A1EN/A1BE
    // decoded the window but did not hold CPU writes (PEEK 8400 stayed 0
    // while vacant still read ROM byte 33). No INIT: the RAM powers up zero.
    wire [9:0] read_q;
    (* keep, BEL = "MISTRAL_M10K.26.1.0" *)
    MISTRAL_M10K_TDP #(
        .CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(0)
    ) slot_cell (
        .CLK1(FPGA_CLK1_50), .CLK2(FPGA_CLK1_50),
        .A1ADDR(cpu_a[9:0]),
        .B1ADDR(cpu_a[9:0]),
        .A1DATA({2'b0, cpu_d}),
        .B1DATA(10'b0),
        .A1Q(read_q),
        .B1Q(),
        .A1EN(1'b1),
        .B1EN(1'b1),
        .A1WE(sel && !wr_n),
        .B1WE(1'b0),
        .ACLR0(1'b0),
        .ACLR1(1'b0)
    );
    assign read_data = read_q[7:0];
`else
    zx81_dpram #(
        .ADDRWIDTH(10), .NUMWORDS(1024)
    ) memory (
        .clock(FPGA_CLK1_50),
        .address_a(cpu_a[9:0]),
        .data_a(cpu_d),
        .wren_a(sel && !wr_n),
        .q_a(read_data),
        .address_b(10'b0),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b()
    );
`endif
    assign plug_rdata = {
        1'b0, 1'b0, romcs_q, dsel_q,
        8'b0, read_data
    };
endmodule
