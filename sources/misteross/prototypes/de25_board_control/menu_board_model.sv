// SPDX-License-Identifier: GPL-3.0-or-later
// Model-only observation of reset and frame completion; the pixel oracle
// keeps its own raster and memory rather than reading internal coordinates.
module menu_board_model #(
    parameter [127:0] BUILD_ID = 128'h0123456789abcdef0123456789abcdef
)(
    input wire clk, reset_n, pixel_clk, pixel_reset_n,
    output wire qualified_reset_n, running, stopped, fault,
    output wire [31:0] displayed_sequence, underflows,
    output wire [23:0] video_rgb,
    output wire video_de, video_hs, video_vs,
    input wire [7:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output wire [31:0] readdata,
    output wire readdatavalid, writeresponsevalid,
    output wire [1:0] response,
    output wire waitrequest,
    output wire maintenance_reset_n,
    output wire ddr_arvalid,
    output wire [31:0] ddr_araddr,
    output wire [7:0] ddr_arlen,
    output wire [2:0] ddr_arsize,
    output wire [1:0] ddr_arburst,
    output wire ddr_arid,
    output wire ddr_arlock,
    output wire [3:0] ddr_arcache,
    output wire [2:0] ddr_arprot,
    output wire [3:0] ddr_arqos,
    input wire ddr_arready,
    input wire ddr_rvalid,
    input wire [255:0] ddr_rdata,
    input wire ddr_rlast,
    input wire [1:0] ddr_rresp,
    input wire ddr_rid,
    output wire ddr_rready,
    output wire ddr_awvalid,
    output wire [31:0] ddr_awaddr,
    output wire [7:0] ddr_awlen,
    output wire [2:0] ddr_awsize,
    output wire [1:0] ddr_awburst,
    output wire ddr_awid, ddr_awlock,
    output wire [3:0] ddr_awcache,
    output wire [2:0] ddr_awprot,
    output wire [3:0] ddr_awqos,
    input wire ddr_awready,
    output wire ddr_wvalid,
    output wire [255:0] ddr_wdata,
    output wire [31:0] ddr_wstrb,
    output wire ddr_wlast,
    input wire ddr_wready,
    input wire ddr_bvalid,
    input wire [1:0] ddr_bresp,
    input wire ddr_bid,
    output wire ddr_bready
);
    fes_de25_menu #(.BUILD_ID(BUILD_ID),.CSR_TIMEOUT_CYCLES(256)) dut (.*);
    assign qualified_reset_n=dut.pixel_core_reset_n;
    assign running=dut.unused_running;
    assign stopped=dut.unused_stopped;
    assign fault=dut.unused_fault;
    assign displayed_sequence=dut.unused_displayed;
    assign underflows=dut.unused_underflows;
endmodule
