// SPDX-License-Identifier: GPL-2.0-or-later
// Sinclair 16K RAM pack on the freeze-scaffold Z80-like edge.
// The cart clock port must stay a distinct IB name. `--fes-slot-clock clk_sys`
// splices that pad onto the shell 52 MHz net; naming the port clk_sys leaves
// M10K on the inferred pad output.
`include "zx81_bus_pack.vh"
module cart (
    input wire FPGA_CLK1_50,
    input wire [`ZX81_BUS_REQ-1:0] plug_addr,
    output wire [`ZX81_BUS_RSP-1:0] plug_rdata
);
    wire [15:0] cpu_a = plug_addr[`ZX81_BUS_A];
    wire [7:0] cpu_d = plug_addr[`ZX81_BUS_DWR];
    wire mreq_n = plug_addr[`ZX81_BUS_MREQ_N];
    wire wr_n = plug_addr[`ZX81_BUS_WR_N];
    wire [13:0] peek_a = plug_addr[`ZX81_BUS_PEEK_A];
    wire sel = (cpu_a[15:14] == 2'b01) && !mreq_n;
    wire [7:0] read_data;
    wire [7:0] peek_data;
    wire present;
    zx81_ram_pack memory (
        .clock(FPGA_CLK1_50), .address(cpu_a[13:0]),
        .write_data(cpu_d), .write_enable(sel && !wr_n),
        .peek_address(peek_a),
        .read_data(read_data), .peek_data(peek_data)
    );
    // A real retained register crosses the boundary; a top-level constant
    // would disappear during independent synthesis and could not be linked.
    (* keep *) MISTRAL_FF presence (
        .CLK(FPGA_CLK1_50), .DATAIN(1'b1), .Q(present),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0)
    );
    assign plug_rdata = {
        present, 1'b0, 1'b0, 1'b0,
        peek_data, read_data
    };
endmodule
