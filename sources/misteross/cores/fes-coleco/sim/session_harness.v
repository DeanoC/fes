// SPDX-License-Identifier: GPL-2.0-or-later
// The Coleco native shell's session path without the machine: the real
// Coleco mailbox, a moving TMS9918-shaped logical raster through the native
// source, CDC and Direct output (the linked part), then the shared plane.
module coleco_session_harness (
    input wire clk_sys, pixel_clk, reset_hold,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire [15:0] controller_buttons,
    output wire [27:0] machine_word, hdmi_word,
    output wire ui_active, faulted, quiesced,
    output wire [31:0] displayed_sequence, underflows,
    output reg [31:0] source_frames = 32'd0
);
    wire request, response_valid, response_error;
    wire [6:0] opcode;
    wire [7:0] index;
    wire [15:0] argument, response_data;
    /* verilator lint_off PINCONNECTEMPTY */
    coleco_application_gp #(.ENABLE_SESSION_DISPLAY(1)) gp (
        .clk(clk_sys), .gpo(gpo), .build_id(128'd0), .gpi(gpi), .exec_reset(exec_reset),
        .controller_buttons(controller_buttons), .controller_keypad(),
        .media_ready(), .media_size(), .media_addr(15'd0), .media_q(),
        .firmware_write_addr(), .firmware_write_data(), .firmware_write_enable(),
        .display_request(request), .display_opcode(opcode), .display_index(index),
        .display_argument(argument), .display_response_valid(response_valid),
        .display_response_error(response_error), .display_response_data(response_data),
        .display_quiesced(quiesced)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    // A logical coordinate lasts twelve system clocks, as the VDP's does.
    // 342 dots by 262 lines, 256x192 visible, pixel colour from x and y.
    reg [3:0] dot_clock = 4'd0;
    reg [8:0] dot = 9'd0, line = 9'd0;
    always @(posedge clk_sys) begin
        dot_clock <= dot_clock == 4'd11 ? 4'd0 : dot_clock + 4'd1;
        if (dot_clock == 4'd11) begin
            dot <= dot == 9'd341 ? 9'd0 : dot + 9'd1;
            if (dot == 9'd341) begin
                line <= line == 9'd261 ? 9'd0 : line + 9'd1;
                if (line == 9'd261) source_frames <= source_frames + 32'd1;
            end
        end
    end
    wire logical_blank = dot > 9'd255 || line > 9'd191;
    wire [31:0] source_request, crossed_request;
    coleco_native_video source (
        .clk_sys(clk_sys), .hold(exec_reset), .logical_x(dot[7:0]), .logical_y(line[7:0]),
        .logical_pixel(dot[3:0] ^ line[4:1]), .logical_blank(logical_blank), .request(source_request)
    );
    fes_native_cdc crossing (
        .source_clk(clk_sys), .pixel_clk(pixel_clk),
        .source_request(source_request), .request(crossed_request)
    );
    fes_native_video #(.SCANLINES(0)) part (
        .clock(pixel_clk), .request(crossed_request), .response(machine_word)
    );
    fes_display_plane #(
        .PICTURE_LEFT(16'd384), .PICTURE_TOP(16'd168),
        .PICTURE_WIDTH(16'd512), .PICTURE_HEIGHT(16'd384)
    ) display (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(reset_hold), .exec_reset(exec_reset),
        .request(request), .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .quiesced_sys(quiesced), .machine(machine_word), .video(hdmi_word), .ui_active(ui_active), .locked(),
        .displayed_sequence(displayed_sequence), .underflows(underflows), .faulted(faulted)
    );
endmodule
