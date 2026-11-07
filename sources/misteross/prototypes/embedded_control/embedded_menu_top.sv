// SPDX-License-Identifier: GPL-3.0-or-later
// Host integration of the menu video engine and actual owned read endpoint.
module embedded_menu_top #(
    parameter [31:0] WINDOW_BASE = 32'h95000000,
    parameter [31:0] WINDOW_BYTES = 32'd8388608,
    parameter bit CSR_MODE = 1'b0
)(
    input wire clk, reset_n, video_reset_n,
    input wire csr_valid, csr_write,
    input wire [7:0] csr_address,
    input wire [31:0] csr_wdata,
    input wire [3:0] csr_wstrb,
    output wire csr_ready, csr_response_valid, csr_error,
    output wire [31:0] csr_rdata,
    input wire csr_response_ready,
    input wire start, initial_slot,
    input wire [31:0] initial_sequence,
    output wire start_ready,
    input wire submit_valid, submit_slot,
    input wire [31:0] submit_sequence,
    output wire submit_ready,
    input wire quiesce,
    output wire running, stopped, fault,
    output wire [31:0] displayed_sequence, underflows,
    output wire [23:0] rgb,
    output wire de, hs, vs,
    output wire reader_abort,
    output wire [63:0] reader_generation,
    output wire m_arvalid,
    output wire [31:0] m_araddr,
    output wire [7:0] m_arlen,
    output wire [2:0] m_arsize,
    output wire [1:0] m_arburst,
    output wire m_arid,
    input wire m_arready, m_rvalid,
    input wire [255:0] m_rdata,
    input wire m_rlast,
    input wire [1:0] m_rresp,
    input wire m_rid,
    output wire m_rready
);
    wire reader_valid, reader_ready, reader_rvalid, reader_rlast, reader_rready;
    wire [31:0] reader_address;
    wire [7:0] reader_length;
    wire [127:0] reader_rdata;
    wire narrow_arvalid, narrow_arready, narrow_rvalid, narrow_rlast, narrow_rid, narrow_rready;
    wire [31:0] narrow_araddr;
    wire [7:0] narrow_arlen;
    wire [2:0] narrow_arsize;
    wire [1:0] narrow_arburst, narrow_rresp;
    wire [3:0] narrow_arid;
    wire [127:0] narrow_rdata;
    assign m_arsize = 3'd5;
    assign m_arburst = 2'd1;
    assign m_arid = 1'b0;
    fes_de25_read_width width_adapter (
        .clk(clk), .reset_n(reset_n),
        .reader_arvalid(narrow_arvalid), .reader_araddr(narrow_araddr), .reader_arlen(narrow_arlen),
        .reader_arsize(narrow_arsize), .reader_arburst(narrow_arburst), .reader_arid(narrow_arid),
        .reader_arready(narrow_arready), .reader_rvalid(narrow_rvalid), .reader_rdata(narrow_rdata),
        .reader_rlast(narrow_rlast), .reader_rresp(narrow_rresp), .reader_rid(narrow_rid), .reader_rready(narrow_rready),
        .ddr_arvalid(m_arvalid), .ddr_araddr(m_araddr), .ddr_arlen(m_arlen), .ddr_arready(m_arready),
        .ddr_rvalid(m_rvalid), .ddr_rdata(m_rdata), .ddr_rlast(m_rlast), .ddr_rresp(m_rresp),
        .ddr_rid(m_rid), .ddr_rready(m_rready)
    );
    wire control_ready, control_response, control_error;
    wire [31:0] control_rdata;
    wire menu_valid;
    wire [31:0] menu_rdata;
    wire menu_error;
    reg menu_response, menu_response_error;
    reg [31:0] menu_response_data;
    assign csr_ready = CSR_MODE ? ((!control_response && !menu_response) || csr_response_ready) : control_ready;
    assign csr_response_valid = control_response || (CSR_MODE && menu_response);
    assign csr_error = control_response ? control_error : menu_response_error;
    assign csr_rdata = control_response ? control_rdata : menu_response_data;
    assign menu_valid = CSR_MODE && csr_valid && csr_address[7] && csr_ready;
    always @(posedge clk) begin
        if (!reset_n) begin
            menu_response<=0; menu_response_error<=0; menu_response_data<=0;
        end else begin
            if (csr_response_ready) menu_response<=0;
            if (menu_valid) begin
                menu_response<=1; menu_response_error<=menu_error; menu_response_data<=menu_rdata;
            end
        end
    end
    fes_embedded_control #(.BUILD_ID(128'd1), .ABI_ID(32'h10203043), .DATA_WIDTH(128)) control (
        .clk(clk), .reset_n(reset_n), .csr_valid(csr_valid && (!CSR_MODE || (!csr_address[7] && csr_ready))), .csr_write(csr_write),
        .csr_address(csr_address), .csr_wdata(csr_wdata), .csr_wstrb(csr_wstrb),
        .csr_ready(control_ready), .csr_response_valid(control_response), .csr_error(control_error),
        .csr_rdata(control_rdata), .csr_response_ready(csr_response_ready),
        .reader_valid(reader_valid), .reader_address(reader_address), .reader_length(reader_length),
        .reader_ready(reader_ready), .reader_rvalid(reader_rvalid), .reader_rdata(reader_rdata),
        .reader_rlast(reader_rlast), .reader_rready(reader_rready), .reader_abort(reader_abort),
        .reader_generation(reader_generation),
        .m_arvalid(narrow_arvalid), .m_araddr(narrow_araddr), .m_arlen(narrow_arlen), .m_arsize(narrow_arsize),
        .m_arburst(narrow_arburst), .m_arid(narrow_arid), .m_arready(narrow_arready), .m_rvalid(narrow_rvalid),
        .m_rdata(narrow_rdata), .m_rlast(narrow_rlast), .m_rresp(narrow_rresp), .m_rid({3'd0,narrow_rid}), .m_rready(narrow_rready)
    );
    generate if (CSR_MODE) begin : csr_menu
        fes_embedded_menu_control #(.WINDOW_BASE(WINDOW_BASE), .WINDOW_BYTES(WINDOW_BYTES)) menu (
            .clk(clk), .reset_n(reset_n && video_reset_n), .valid(menu_valid), .write(csr_write),
            .address(csr_address), .wdata(csr_wdata), .wstrb(csr_wstrb), .rdata(menu_rdata), .error(menu_error),
            .start_ready(start_ready), .submit_ready(submit_ready), .running(running), .stopped(stopped), .fault(fault),
            .displayed_sequence(displayed_sequence), .underflows(underflows), .rgb(rgb), .de(de), .hs(hs), .vs(vs),
            .reader_valid(reader_valid), .reader_address(reader_address), .reader_length(reader_length),
            .reader_ready(reader_ready), .reader_abort(reader_abort), .reader_generation(reader_generation),
            .reader_rvalid(reader_rvalid), .reader_rdata(reader_rdata), .reader_rready(reader_rready)
        );
    end else begin : direct_menu
        assign menu_rdata=0;
        assign menu_error=0;
    fes_embedded_menu_scanout #(.WINDOW_BASE(WINDOW_BASE), .WINDOW_BYTES(WINDOW_BYTES)) scanout (
        .clk(clk), .reset_n(reset_n && video_reset_n),
        .start(start), .initial_slot(initial_slot), .initial_sequence(initial_sequence), .start_ready(start_ready),
        .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence), .submit_ready(submit_ready),
        .quiesce(quiesce), .running(running), .stopped(stopped), .fault(fault),
        .displayed_sequence(displayed_sequence), .underflows(underflows), .rgb(rgb), .de(de), .hs(hs), .vs(vs),
        .reader_valid(reader_valid), .reader_address(reader_address), .reader_length(reader_length),
        .reader_ready(reader_ready), .reader_abort(reader_abort), .reader_generation(reader_generation),
        .reader_rvalid(reader_rvalid), .reader_rdata(reader_rdata), .reader_rready(reader_rready)
    );
    end endgenerate
endmodule
