// SPDX-License-Identifier: GPL-3.0-or-later
// Read-only 128-to-256-bit boundary for aligned, even-length reader bursts.
// The original ledger counts both response halves. DISABLE keeps the buffered
// half; the parent endpoint remains responsible for fault/drain. Reset alone
// supplies no drain receipt.
module fes_de25_read_width (
    input wire clk,reset_n,
    input wire reader_arvalid,
    input wire [31:0] reader_araddr,
    input wire [7:0] reader_arlen,
    input wire [2:0] reader_arsize,
    input wire [1:0] reader_arburst,
    input wire [3:0] reader_arid,
    output wire reader_arready,
    output wire reader_rvalid,
    output wire [127:0] reader_rdata,
    output wire reader_rlast,
    output wire [1:0] reader_rresp,
    output wire reader_rid,
    input wire reader_rready,
    output wire ddr_arvalid,
    output wire [31:0] ddr_araddr,
    output wire [7:0] ddr_arlen,
    input wire ddr_arready,
    input wire ddr_rvalid,
    input wire [255:0] ddr_rdata,
    input wire ddr_rlast,
    input wire [1:0] ddr_rresp,
    input wire ddr_rid,
    output wire ddr_rready
);
    reg upper_valid,upper_last,upper_id;
    reg [127:0] upper_data;
    reg [1:0] upper_response;
    wire descriptor_valid=reader_araddr[4:0]==0 && reader_arlen[0] &&
        reader_arsize==4 && reader_arburst==1 && reader_arid==0;
    assign ddr_arvalid=reset_n && reader_arvalid && descriptor_valid;
    assign ddr_araddr=reader_araddr;
    assign ddr_arlen={1'b0,reader_arlen[7:1]};
    assign reader_arready=reset_n && descriptor_valid && ddr_arready;
    // Expose raw RVALID while empty, including unsolicited responses, so a
    // new response cannot hide behind a buffer and manufacture a drain ACK.
    assign reader_rvalid=reset_n && (upper_valid || ddr_rvalid);
    assign reader_rdata=upper_valid ? upper_data : ddr_rdata[127:0];
    assign reader_rlast=upper_valid && upper_last;
    assign reader_rresp=upper_valid ? upper_response : ddr_rresp;
    assign reader_rid=upper_valid ? upper_id : ddr_rid;
    assign ddr_rready=reset_n && !upper_valid && reader_rready;
    always @(posedge clk) begin
        if (!reset_n) begin
            upper_valid<=0;upper_last<=0;upper_id<=0;upper_data<=0;upper_response<=0;
        end else begin
            if (upper_valid && reader_rready) upper_valid<=0;
            if (ddr_rvalid && ddr_rready) begin
                upper_valid<=1;upper_data<=ddr_rdata[255:128];
                upper_last<=ddr_rlast;upper_id<=ddr_rid;upper_response<=ddr_rresp;
            end
        end
    end
endmodule
