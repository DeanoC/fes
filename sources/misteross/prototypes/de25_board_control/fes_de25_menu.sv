// SPDX-License-Identifier: GPL-3.0-or-later
// CPU CSR remains on 100 MHz. The owned reader, width adapter and scanout
// share the pixel clock; Platform Designer crosses their AXI port to HPS.
`ifndef FES_DE25_COMPUTER_BUILD_ID
`define FES_DE25_COMPUTER_BUILD_ID 128'h0
`endif
module fes_de25_menu #(
    parameter [127:0] BUILD_ID = `FES_DE25_COMPUTER_BUILD_ID,
    parameter integer MEMORY_BACKEND = 3,
    parameter [31:0] DDR_BASE = 32'h95000000,
    parameter [31:0] DDR_BYTES = 32'd8388608,
    parameter integer CSR_TIMEOUT_CYCLES = 1048576
)(
    input wire clk, reset_n, pixel_clk, pixel_reset_n,
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
    localparam bit MAINTENANCE_HELD = 1'b1;
    assign maintenance_reset_n = !MAINTENANCE_HELD;
    wire pixel_core_reset_n, command_valid, command_write, command_ready;
    wire result_valid, result_error;
    wire [7:0] command_address;
    wire [31:0] command_wdata, result_data;
    wire [3:0] command_wstrb;
    wire unused_failed;
    fes_de25_menu_cdc #(.TIMEOUT_CYCLES(CSR_TIMEOUT_CYCLES)) csr_crossing (
        .clk(clk),.reset_n(reset_n),.pixel_clk(pixel_clk),.pixel_reset_n(pixel_reset_n),
        .address(address),.read(read),.write(write),.byteenable(byteenable),.writedata(writedata),
        .waitrequest(waitrequest),.readdata(readdata),.readdatavalid(readdatavalid),
        .writeresponsevalid(writeresponsevalid),.response(response),.failed(unused_failed),
        .pixel_core_reset_n(pixel_core_reset_n),.command_valid(command_valid),.command_write(command_write),
        .command_address(command_address),.command_wdata(command_wdata),.command_wstrb(command_wstrb),
        .command_ready(command_ready),.result_valid(result_valid),.result_error(result_error),.result_data(result_data)
    );
    // Invalid compile parameters never grant either slot. Keeping the existing
    // pipeline geometry guard also makes this failure visible through its CSR.
    localparam [31:0] EFFECTIVE_BYTES = MEMORY_BACKEND==3 && DDR_BASE==32'h95000000 && DDR_BYTES==32'd8388608 ? DDR_BYTES : 32'd0;
    wire unused_start_ready, unused_submit_ready, unused_running, unused_stopped,
        unused_fault, unused_abort;
    wire [31:0] unused_displayed, unused_underflows;
    wire [63:0] unused_generation;
    wire [2:0] unused_arsize;
    wire [1:0] unused_arburst;
    wire unused_arid;
    embedded_menu_top #(.BUILD_ID(BUILD_ID),.MAINTENANCE_HELD(MAINTENANCE_HELD),
        .WINDOW_BASE(DDR_BASE),.WINDOW_BYTES(EFFECTIVE_BYTES),.CSR_MODE(1'b1)) pipeline (
        .clk(pixel_clk),.reset_n(pixel_core_reset_n),.video_reset_n(pixel_core_reset_n),
        .csr_valid(command_valid),.csr_write(command_write),.csr_address(command_address),
        .csr_wdata(command_wdata),.csr_wstrb(command_wstrb),.csr_ready(command_ready),
        .csr_response_valid(result_valid),.csr_error(result_error),.csr_rdata(result_data),.csr_response_ready(1'b1),
        .start(1'b0),.initial_slot(1'b0),.initial_sequence(32'd0),.start_ready(unused_start_ready),
        .submit_valid(1'b0),.submit_slot(1'b0),.submit_sequence(32'd0),.submit_ready(unused_submit_ready),
        .quiesce(1'b0),.running(unused_running),.stopped(unused_stopped),.fault(unused_fault),
        .displayed_sequence(unused_displayed),.underflows(unused_underflows),
        .rgb(video_rgb),.de(video_de),.hs(video_hs),.vs(video_vs),
        .reader_abort(unused_abort),.reader_generation(unused_generation),
        .m_arvalid(ddr_arvalid),.m_araddr(ddr_araddr),.m_arlen(ddr_arlen),
        .m_arsize(unused_arsize),.m_arburst(unused_arburst),.m_arid(unused_arid),
        .m_arready(ddr_arready),.m_rvalid(ddr_rvalid || ddr_bvalid),.m_rdata(ddr_rdata),
        .m_rlast(ddr_rlast),.m_rresp(ddr_bvalid ? 2'b10 : ddr_rresp),.m_rid(ddr_rid),.m_rready(ddr_rready)
    );
    assign ddr_arsize = 3'd5;
    assign ddr_arburst = 2'd1;
    assign ddr_arid = 1'b0;
    assign ddr_arlock = 1'b0;
    assign ddr_arcache = 4'd0;
    assign ddr_arprot = 3'b010; // Non-secure data read; no instruction access.
    assign ddr_arqos = 4'd0;
    // Platform Designer requires a complete AXI port set. These are ties,
    // not another producer or a software-selectable write path.
    assign ddr_awvalid = 1'b0;
    assign ddr_awaddr = 32'd0;
    assign ddr_awlen = 8'd0;
    assign ddr_awsize = 3'd0;
    assign ddr_awburst = 2'd0;
    assign ddr_awid = 1'b0;
    assign ddr_awlock = 1'b0;
    assign ddr_awcache = 4'd0;
    assign ddr_awprot = 3'd0;
    assign ddr_awqos = 4'd0;
    assign ddr_wvalid = 1'b0;
    assign ddr_wdata = 256'd0;
    assign ddr_wstrb = 32'd0;
    assign ddr_wlast = 1'b0;
    assign ddr_bready = 1'b1;
endmodule
