// SPDX-License-Identifier: GPL-3.0-or-later
// Host model of the actual owned endpoint and fixed framebuffer source.
// No board mapping, bridge capability, pixel clock or HDMI route is implied.
module embedded_window_top #(
    parameter [127:0] BUILD_ID = 128'd1,
    parameter [31:0] CONTROL_ABI = 32'h10203042,
    parameter [31:0] WINDOW_BASE = 32'h94000000,
    parameter [31:0] WINDOW_BYTES = 32'd8388608,
    parameter [31:0] ROW_BYTES = 32'd5120,
    parameter [31:0] STRIDE_BYTES = 32'd5120,
    parameter [31:0] ROWS = 32'd720
)(
    input wire clk, reset_n, window_reset_n, control_reset_n,
    input wire csr_valid, csr_write,
    input wire [7:0] csr_address,
    input wire [31:0] csr_wdata,
    input wire [3:0] csr_wstrb,
    output wire csr_ready, csr_response_valid, csr_error,
    output wire [31:0] csr_rdata,
    input wire csr_response_ready,
    input wire start,
    output wire start_ready, busy, complete, aborted, fault,
    output wire [31:0] requested_bytes, delivered_bytes,
    output wire stream_valid,
    output wire [127:0] stream_data,
    output wire stream_first, stream_row_last, stream_last,
    input wire stream_ready,
    output wire [7:0] invalid_ready, invalid_request, invalid_fault,
    output wire reader_abort,
    output wire [63:0] reader_generation,
    output wire m_arvalid,
    output wire [31:0] m_araddr,
    output wire [7:0] m_arlen,
    output wire [2:0] m_arsize,
    output wire [1:0] m_arburst,
    output wire [3:0] m_arid,
    input wire m_arready, m_rvalid,
    input wire [127:0] m_rdata,
    input wire m_rlast,
    input wire [1:0] m_rresp,
    input wire [3:0] m_rid,
    output wire m_rready
);
    wire reader_valid, reader_ready, reader_rvalid, reader_rlast, reader_rready;
    wire [31:0] reader_address;
    wire [7:0] reader_length;
    wire [127:0] reader_rdata;
    fes_embedded_control #(.BUILD_ID(BUILD_ID), .ABI_ID(CONTROL_ABI), .DATA_WIDTH(128)) control (
        .clk(clk), .reset_n(reset_n && control_reset_n), .csr_valid(csr_valid), .csr_write(csr_write),
        .csr_address(csr_address), .csr_wdata(csr_wdata), .csr_wstrb(csr_wstrb),
        .csr_ready(csr_ready), .csr_response_valid(csr_response_valid), .csr_error(csr_error),
        .csr_rdata(csr_rdata), .csr_response_ready(csr_response_ready),
        .reader_valid(reader_valid), .reader_address(reader_address), .reader_length(reader_length),
        .reader_ready(reader_ready), .reader_rvalid(reader_rvalid), .reader_rdata(reader_rdata),
        .reader_rlast(reader_rlast), .reader_rready(reader_rready), .reader_abort(reader_abort),
        .reader_generation(reader_generation),
        .m_arvalid(m_arvalid), .m_araddr(m_araddr), .m_arlen(m_arlen), .m_arsize(m_arsize),
        .m_arburst(m_arburst), .m_arid(m_arid), .m_arready(m_arready), .m_rvalid(m_rvalid),
        .m_rdata(m_rdata), .m_rlast(m_rlast), .m_rresp(m_rresp), .m_rid(m_rid), .m_rready(m_rready)
    );
    fes_embedded_window_reader #(.WINDOW_BASE(WINDOW_BASE), .WINDOW_BYTES(WINDOW_BYTES),
        .ROW_BYTES(ROW_BYTES), .STRIDE_BYTES(STRIDE_BYTES), .ROWS(ROWS)) window_reader (
        .clk(clk), .reset_n(reset_n && window_reset_n), .start(start), .start_ready(start_ready), .busy(busy),
        .complete(complete), .aborted(aborted), .fault(fault),
        .requested_bytes(requested_bytes), .delivered_bytes(delivered_bytes),
        .reader_valid(reader_valid), .reader_address(reader_address), .reader_length(reader_length),
        .reader_ready(reader_ready), .reader_abort(reader_abort), .reader_rvalid(reader_rvalid),
        .reader_generation(reader_generation),
        .reader_rdata(reader_rdata), .reader_rready(reader_rready), .stream_valid(stream_valid),
        .stream_data(stream_data), .stream_first(stream_first), .stream_row_last(stream_row_last),
        .stream_last(stream_last), .stream_ready(stream_ready)
    );
    // Invalid elaborated windows must produce no request even when a caller
    // supplies START and an apparently enabled/ready downstream interface.
    genvar bad;
    reg invalid_armed;
    always @(posedge clk) invalid_armed <= reset_n;
    generate for (bad=0; bad<8; bad=bad+1) begin: invalid_windows
        /* verilator lint_off PINCONNECTEMPTY */
        fes_embedded_window_reader #(
            .WINDOW_BASE(bad==0 ? 32'h94000001 : bad==5 ? 32'hfffffff0 : 32'h94000000),
            .WINDOW_BYTES(bad==1 ? 32'd16 : 32'd8388608),
            .ROW_BYTES(bad==2 ? 32'd0 : bad==6 ? 32'd17 : 32'd5120),
            .STRIDE_BYTES(bad==3 ? 32'd5104 : 32'd5120),
            .ROWS(bad==4 ? 32'd0 : bad==7 ? 32'd2161 : 32'd720)
        ) rejected (
            .clk(clk), .reset_n(reset_n), .start(1'b1), .start_ready(invalid_ready[bad]),
            .busy(), .complete(), .aborted(), .fault(invalid_fault[bad]),
            .requested_bytes(), .delivered_bytes(), .reader_valid(invalid_request[bad]),
            .reader_address(), .reader_length(), .reader_ready(1'b1), .reader_abort(!invalid_armed),
            .reader_generation(invalid_armed ? 64'd1 : 64'd0),
            .reader_rvalid(1'b0), .reader_rdata(128'd0), .reader_rready(),
            .stream_valid(), .stream_data(), .stream_first(), .stream_row_last(),
            .stream_last(), .stream_ready(1'b1)
        );
        /* verilator lint_on PINCONNECTEMPTY */
    end endgenerate
endmodule
