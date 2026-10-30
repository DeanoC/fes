// SPDX-License-Identifier: GPL-2.0-or-later
// Complete logical ISA path for the read-only PC/XT floppy adapter.
module fdc_top (
    input wire clk, reset, guest_ce,
    output wire observed_commit,
    output wire [19:0] observed_seek_ticks,
    input wire cycle_valid,
    output wire cycle_ready,
    input wire [19:0] cycle_address,
    input wire [7:0] cycle_wdata,
    input wire [1:0] cycle_kind,
    input wire cycle_write, cycle_aen,
    input wire [1:0] cycle_dma_channel,
    input wire cycle_terminal_count,
    output wire cycle_done,
    output wire [7:0] cycle_rdata,
    output wire [1:0] cycle_fault,
    output wire [7:0] irq,
    output wire [2:0] drq,
    input wire media_present,
    output wire media_read_valid,
    output wire [18:0] media_read_addr,
    input wire media_read_ready,
    input wire [7:0] media_read_data
);
    wire [47:0] request, card_request;
    wire [31:0] card_response, response;
    pcxt_isa_bridge #(.SLOTS(1)) bridge (
        .cycle_lock(1'b0), .responses(response), .request(request), .*);
    pcxt_isa_boundary boundary (
        .clk(clk), .reset(reset), .request(request), .response(response),
        .card_request(card_request), .card_response(card_response));
    pcxt_fdc_card card (
        .clk(clk), .reset(reset), .guest_ce(guest_ce), .request(card_request),
        .response(card_response), .media_present(media_present),
        .media_read_valid(media_read_valid), .media_read_addr(media_read_addr),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data));
    assign observed_commit = card.commit;
    assign observed_seek_ticks = card.seek_ticks;
endmodule
