// The real ZX81 CPU/ULA and expansion edge, with an independently built
// cart's logical interface. This is behavioral acceptance, not CRAM-link
// evidence. CART_PRESENT=0 keeps the vacant 1 KiB even if the cart netlist
// is linked for simulation.
`include "zx81_bus_pack.vh"
module expansion_machine #(
    parameter CART_PRESENT = 0
) (
    input wire clk_sys, reset,
    input wire [39:0] keyboard,
    input wire tape_ready,
    input wire [14:0] tape_size,
    input wire [7:0] tape_data,
    output wire [13:0] tape_addr_out,
    output wire tape_busy,
    output wire ce_6m5, video_pixel, hblank, vblank, hsync_out, vsync_out, halt_n,
    output wire [15:0] cpu_addr,
    input wire [15:0] peek_addr,
    output wire [7:0] peek_data
);
    wire [15:0] bus_addr;
    wire [7:0] bus_wdata, bus_rdata, bus_peek_data;
    wire bus_mreq_n, bus_iorq_n, bus_rd_n, bus_wr_n, bus_m1_n, bus_rfsh_n;
    wire bus_dsel, bus_romcs, bus_wait, socket_ram_present;
    wire [`ZX81_BUS_REQ-1:0] plug_addr;
    wire [`ZX81_BUS_RSP-1:0] cart_rdata, plug_rdata;
    zx81_machine #(.EXTERNAL_RAM(1)) machine (
        .clk_sys(clk_sys), .reset(reset), .keyboard(keyboard),
        .tape_ready(tape_ready), .tape_size(tape_size), .tape_data(tape_data),
        .tape_addr_out(tape_addr_out), .tape_busy(tape_busy),
        .ce_6m5(ce_6m5), .video_pixel(video_pixel),
        .hblank(hblank), .vblank(vblank), .hsync_out(hsync_out), .vsync_out(vsync_out),
        .halt_n(halt_n), .cpu_addr(cpu_addr), .peek_addr(peek_addr), .peek_data(peek_data),
        .ram_address(), .ram_write_data(), .ram_write_enable(),
        .external_ram_data(8'b0), .external_peek_data(8'b0),
        .bus_addr(bus_addr), .bus_wdata(bus_wdata),
        .bus_mreq_n(bus_mreq_n), .bus_iorq_n(bus_iorq_n),
        .bus_rd_n(bus_rd_n), .bus_wr_n(bus_wr_n),
        .bus_m1_n(bus_m1_n), .bus_rfsh_n(bus_rfsh_n),
        .bus_rdata(bus_rdata), .bus_peek_data(bus_peek_data),
        .bus_dsel(bus_dsel), .bus_romcs(bus_romcs),
        .bus_wait(bus_wait), .bus_ram_present(CART_PRESENT != 0 && socket_ram_present)
    );
    zx81_expansion_socket socket (
        .clock(clk_sys),
        .cpu_addr(bus_addr),
        .cpu_wdata(bus_wdata),
        .cpu_mreq_n(bus_mreq_n),
        .cpu_iorq_n(bus_iorq_n),
        .cpu_rd_n(bus_rd_n),
        .cpu_wr_n(bus_wr_n),
        .cpu_m1_n(bus_m1_n),
        .cpu_rfsh_n(bus_rfsh_n),
        .peek_address(peek_addr[13:0]),
        .bus_rdata(bus_rdata),
        .bus_peek_data(bus_peek_data),
        .bus_dsel(bus_dsel),
        .bus_romcs(bus_romcs),
        .bus_wait(bus_wait),
        .bus_ram_present(socket_ram_present),
        .plug_addr(plug_addr),
        .plug_rdata_in(cart_rdata),
        .plug_rdata(plug_rdata)
    );
    cart pack (
        .FPGA_CLK1_50(clk_sys),
        .plug_addr(plug_addr),
        .plug_rdata(cart_rdata)
    );
endmodule
