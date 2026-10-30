// SPDX-License-Identifier: GPL-3.0-or-later
// Portable producer prototype. No board pinout, clock source, DDR mapping or
// physical CSR bridge is implied by this simulation top.
module embedded_producer_top #(
    parameter [127:0] BUILD_ID = 128'd0,
    parameter [31:0] CONTROL_ABI = 32'd0
)(
    input wire clk, reset_n,
    input wire csr_valid, csr_write,
    input wire [7:0] csr_address,
    input wire [31:0] csr_wdata,
    input wire [3:0] csr_wstrb,
    output wire csr_ready,
    output wire csr_response_valid, csr_error,
    output wire [31:0] csr_rdata,
    input wire csr_response_ready,
    input wire reader_valid,
    input wire [31:0] reader_address,
    input wire [7:0] reader_length,
    output wire reader_ready,
    output wire reader_rvalid,
    output wire [127:0] reader_rdata,
    output wire reader_rlast,
    input wire reader_rready,
    output wire reader_abort,
    output wire m_arvalid,
    output wire [31:0] m_araddr,
    output wire [7:0] m_arlen,
    output wire [2:0] m_arsize,
    output wire [1:0] m_arburst,
    output wire [3:0] m_arid,
    input wire m_arready,
    input wire m_rvalid,
    input wire [127:0] m_rdata,
    input wire m_rlast,
    input wire [1:0] m_rresp,
    input wire [3:0] m_rid,
    output wire m_rready
);
    // The producer computes BUILD_ID before compilation and passes exactly
    // that parameter through to the relative control aperture.
    fes_embedded_control #(.BUILD_ID(BUILD_ID), .ABI_ID(CONTROL_ABI), .DATA_WIDTH(128)) control (.*);
endmodule
