// SPDX-License-Identifier: GPL-2.0-or-later
// Eight registered logical slots; CGA slot 2 and MDA slot 3, remaining vacant.
module pcxt_display_bus #(parameter bit DIAGNOSTIC_PROBE = 0, FLOPPY = 0,
    parameter integer CLOCK_HZ = 100000000) (
    input wire clk, reset, cycle_valid,
    input wire media_present,
    output wire media_read_valid,
    output wire [18:0] media_read_addr,
    input wire media_read_ready,
    input wire [7:0] media_read_data,
    output wire cycle_ready,
    input wire [19:0] cycle_address,
    input wire [7:0] cycle_wdata,
    input wire [1:0] cycle_kind,
    input wire cycle_write, cycle_aen, cycle_lock,
    input wire [1:0] cycle_dma_channel,
    input wire cycle_terminal_count, guest_ce,
    output wire cycle_done,
    output wire [7:0] cycle_rdata,
    output wire [1:0] cycle_fault,
    output wire [7:0] irq,
    output wire [2:0] drq,
    input wire light_pen_trigger, light_pen_switch,
    output wire [11:0] cga_font_address, mda_font_address,
    input wire [7:0] cga_font_data, mda_font_data,
    output wire [1:0] pixel_ce, de, hs, vs, sof, eol, unsupported_interlace,
    output wire [7:0] rgbi
);
    wire [47:0] request;
    wire [255:0] responses;
    pcxt_isa_bridge bridge (.*);
    genvar n;
    generate for (n=0; n<8; n=n+1) begin: slots
        wire [47:0] card_request;
        wire [31:0] card_response;
        pcxt_isa_boundary boundary (
            .clk(clk), .reset(reset), .request(request), .card_request(card_request),
            .card_response(card_response), .response(responses[n*32 +: 32])
        );
        if (n == 1) begin: cga
            pcxt_cga_card #(.CLOCK_HZ(CLOCK_HZ)) card (
                .clk(clk), .reset(reset), .request(card_request), .response(card_response),
                .font_address(cga_font_address), .font_data(cga_font_data),
                .light_pen_trigger(light_pen_trigger), .light_pen_switch(light_pen_switch),
                .pixel_ce(pixel_ce[0]), .de(de[0]), .hs(hs[0]), .vs(vs[0]),
                .sof(sof[0]), .eol(eol[0]), .rgbi(rgbi[3:0]),
                .unsupported_interlace(unsupported_interlace[0])
            );
        end else if (n == 2) begin: mda
            pcxt_mda_card #(.CLOCK_HZ(CLOCK_HZ)) card (
                .clk(clk), .reset(reset), .request(card_request), .response(card_response),
                .font_address(mda_font_address), .font_data(mda_font_data),
                .light_pen_trigger(1'b0), .light_pen_switch(1'b0),
                .pixel_ce(pixel_ce[1]), .de(de[1]), .hs(hs[1]), .vs(vs[1]),
                .sof(sof[1]), .eol(eol[1]), .rgbi(rgbi[7:4]),
                .unsupported_interlace(unsupported_interlace[1])
            );
        end else if (n == 3 && FLOPPY) begin: floppy
            pcxt_fdc_card card (
                .clk(clk), .reset(reset), .guest_ce(guest_ce),
                .request(card_request), .response(card_response),
                .media_present(media_present), .media_read_valid(media_read_valid),
                .media_read_addr(media_read_addr), .media_read_ready(media_read_ready),
                .media_read_data(media_read_data)
            );
        end else if (n == 0 && DIAGNOSTIC_PROBE && !FLOPPY) begin: probe
            /* verilator lint_off PINCONNECTEMPTY */
            pcxt_isa_probe card (.clk(clk), .reset(reset), .request(card_request), .response(card_response), .commit_pulse());
            /* verilator lint_on PINCONNECTEMPTY */
        end else begin: vacant
            assign card_response = 0;
            wire unused_request = ^card_request;
        end
    end endgenerate
    // The diagnostic probe also claims DMA2, so the floppy profile leaves it
    // vacant. Disabled profiles retain the prior zero-response slot behavior.
    generate if (!FLOPPY) begin: no_floppy
        assign media_read_valid = 1'b0;
        assign media_read_addr = 19'b0;
    end endgenerate
    wire unused_media_inputs = &{1'b0, media_present, media_read_ready, media_read_data};
endmodule
