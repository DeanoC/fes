// SPDX-License-Identifier: GPL-2.0-or-later
`include "zx81_bus_pack.vh"
module fes_de25_zx81_static (
    input wire clk, running,
    input wire [7:0] diagnostic_address,
    input wire [19:0] bus_response,
    output reg [45:0] bus_request = 0,
    output wire [7:0] peek_data,
    output wire [15:0] cpu_addr
);
    (* preserve, dont_merge *) reg [19:0] response_q = 0;
    wire [15:0] a;
    wire [7:0] d;
    wire mreq_n, iorq_n, rd_n, wr_n, m1_n, rfsh_n, cpu_clock;
    wire [15:0] peek_addr = {8'h40, diagnostic_address};
    always @(posedge clk) begin
        response_q <= bus_response;
        bus_request <= {running, cpu_clock, peek_addr[13:0], rfsh_n, m1_n,
                        wr_n | !running, rd_n, iorq_n, mreq_n | !running, d, a};
    end
`ifdef QUARTUS
    localparam FIRMWARE = "zx8x.mif";
`else
    localparam FIRMWARE = "zx8x.hex";
`endif
    zx81_machine #(.EXTERNAL_RAM(1), .FIRMWARE_INIT(FIRMWARE),
        .CLOCK_NUMERATOR(13), .CLOCK_DENOMINATOR(200)) machine (
        .clk_sys(clk), .reset(!running), .keyboard(40'hffffffffff),
        .tape_ready(1'b0), .tape_size(15'd0), .tape_data(8'd0), .tape_addr_out(), .tape_busy(),
        .save_ready(), .save_busy(), .save_changed(), .save_size(), .save_addr(14'd0), .save_q(),
        .ce_6m5(), .video_pixel(), .hblank(), .vblank(), .hsync_out(), .vsync_out(), .halt_n(),
        .cpu_addr(cpu_addr), .peek_addr(peek_addr), .peek_data(peek_data),
        .ram_address(), .ram_write_data(), .ram_write_enable(),
        .external_ram_data(8'd0), .external_peek_data(8'd0),
        .bus_addr(a), .bus_wdata(d), .bus_mreq_n(mreq_n), .bus_iorq_n(iorq_n),
        .bus_rd_n(rd_n), .bus_wr_n(wr_n), .bus_m1_n(m1_n), .bus_rfsh_n(rfsh_n),
        .bus_cpu_clock(cpu_clock), .bus_rdata(response_q[`ZX81_BUS_DRD]),
        .bus_peek_data(response_q[`ZX81_BUS_PEEK_D]), .bus_dsel(response_q[`ZX81_BUS_DSEL]),
        .bus_romcs(response_q[`ZX81_BUS_ROMCS]), .bus_wait(response_q[`ZX81_BUS_WAIT]),
        .bus_ram_present(response_q[`ZX81_BUS_RAM_PRESENT]));
endmodule
