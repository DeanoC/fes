// Drive the registered ZX81 expansion edge without the CPU. CART is the
// independently synthesized cart (`module cart`) linked for this run.
`include "zx81_bus_pack.vh"
module expansion_bus_harness (
    input wire clk,
    input wire [15:0] cpu_addr,
    input wire [7:0] cpu_wdata,
    input wire cpu_mreq_n,
    input wire cpu_iorq_n,
    input wire cpu_rd_n,
    input wire cpu_wr_n,
    input wire cpu_m1_n,
    input wire cpu_rfsh_n,
    input wire [13:0] peek_address,
    output wire [7:0] bus_rdata,
    output wire [7:0] bus_peek_data,
    output wire bus_dsel,
    output wire bus_romcs,
    output wire bus_wait,
    output wire bus_ram_present
);
    wire [`ZX81_BUS_REQ-1:0] plug_addr;
    wire [`ZX81_BUS_RSP-1:0] cart_rdata, plug_rdata;
    zx81_expansion_socket socket (
        .clock(clk),
        .cpu_addr(cpu_addr),
        .cpu_wdata(cpu_wdata),
        .cpu_mreq_n(cpu_mreq_n),
        .cpu_iorq_n(cpu_iorq_n),
        .cpu_rd_n(cpu_rd_n),
        .cpu_wr_n(cpu_wr_n),
        .cpu_m1_n(cpu_m1_n),
        .cpu_rfsh_n(cpu_rfsh_n),
        .peek_address(peek_address),
        .bus_rdata(bus_rdata),
        .bus_peek_data(bus_peek_data),
        .bus_dsel(bus_dsel),
        .bus_romcs(bus_romcs),
        .bus_wait(bus_wait),
        .bus_ram_present(bus_ram_present),
        .plug_addr(plug_addr),
        .plug_rdata_in(cart_rdata),
        .plug_rdata(plug_rdata)
    );
    cart pack (
        .FPGA_CLK1_50(clk),
        .plug_addr(plug_addr),
        .plug_rdata(cart_rdata)
    );
endmodule
