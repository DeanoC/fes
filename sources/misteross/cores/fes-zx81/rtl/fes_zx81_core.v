// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
// Standard ZX81 machine, control and capture. Board wrappers own clocks,
// HPS transport, HDMI, audio serialization, expansion placement and the
// session plane's memory adapter. No FPGA-family primitive belongs here.
module fes_zx81_core #(
    parameter [127:0] BUILD_ID = 128'd0,
    parameter EXPANSION_SOCKET = 0,
    parameter FIRMWARE_INIT = "",
    parameter integer CLOCK_NUMERATOR = 1625,
    parameter integer CLOCK_DENOMINATOR = 13056
) (
    input wire clk_sys, pixel_clk, reset_hold,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire [27:0] machine_video,
    output wire signed [15:0] audio_sample,
    output wire display_request,
    output wire [6:0] display_opcode,
    output wire [7:0] display_index,
    output wire [15:0] display_argument,
    input wire display_response_valid, display_response_error,
    input wire [15:0] display_response_data,
    input wire display_quiesced,
    output wire [15:0] bus_addr,
    output wire [7:0] bus_wdata,
    output wire bus_mreq_n, bus_iorq_n, bus_rd_n, bus_wr_n, bus_m1_n, bus_rfsh_n,
    output wire bus_cpu_clock,
    input wire [7:0] bus_rdata, bus_peek_data,
    input wire bus_dsel, bus_romcs, bus_wait, bus_ram_present,
    input wire [15:0] peek_addr,
    output wire [7:0] peek_data,
    output wire [15:0] cpu_addr,
    output wire ram_write_enable,
    output wire [10:0] raster_h,
    output wire [9:0] raster_v
);
    wire machine_reset = exec_reset || reset_hold;
    wire [39:0] keyboard;
    wire [143:0] keyboard_rows;
    wire tape_ready, tape_busy, save_ready, save_busy, save_changed;
    wire [14:0] tape_size, save_size;
    wire [13:0] tape_addr, media_write_addr, ram_address;
    wire [7:0] tape_data, save_q, ram_write_data;
    wire media_read_req;
    wire [19:0] media_read_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable, unit0_state;
    wire [31:0] unit0_size;
    reg save_read_ready;
    always @(posedge clk_sys) save_read_ready <= media_read_req;
    wire ce_6m5, video_pixel, hblank, vblank, machine_de, machine_hs, machine_vs;
    wire [7:0] red, green, blue;
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1),
        .ENABLE_ZX81_TAPE(1),
        // ZX80's cassette entry is the ZX81 SAVE completion address.
`ifdef ZX81_MODEL_ZX80
        .ENABLE_ZX81_TAPE_WRITE(0),
`else
        .ENABLE_ZX81_TAPE_WRITE(1),
`endif
        .ENABLE_SESSION_DISPLAY(1), .MEDIA_AW(14),
        .UNIT0_MIN(`FES_COMPUTER_ZX81_TAPE_MIN_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_ZX81_TAPE_MAX_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(gpo), .build_id(BUILD_ID), .gpi(gpi),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_busy(tape_busy), .media_changed(save_changed), .media_reset(1'b0), .media_frozen(),
        .media_read_req(media_read_req), .media_read_addr(media_read_addr),
        .media_read_ready(save_read_ready), .media_read_data(save_q),
        .save_ready(save_ready), .save_busy(save_busy), .save_size(save_size),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_ready(1'b1),
        .unit0_state(unit0_state), .unit0_size(unit0_size),
        .media_write_unit(), .unit1_state(), .unit1_size(),
        .display_request(display_request), .display_opcode(display_opcode),
        .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(display_response_valid),
        .display_response_error(display_response_error),
        .display_response_data(display_response_data),
        .display_quiesced(display_quiesced),
        .storage_request(), .storage_opcode(), .storage_index(), .storage_argument(),
        .storage_response_valid(1'b0), .storage_response_error(1'b0), .storage_response_data(16'd0),
        .execution_accepted()
    );
    zx81_keyboard keyboard_map (
        .clk(clk_sys), .reset(machine_reset), .rows(keyboard_rows), .matrix(keyboard)
    );
    zx81_tape_store tape (
        .clk(clk_sys), .unit_state(unit0_state), .unit_size(unit0_size),
        .write_addr(media_write_addr), .write_data(media_write_data),
        .write_enable(media_write_enable), .tape_addr(tape_addr),
        .tape_ready(tape_ready), .tape_size(tape_size), .tape_data(tape_data)
    );

    zx81_machine #(.EXTERNAL_RAM(EXPANSION_SOCKET), .FIRMWARE_INIT(FIRMWARE_INIT),
        .CLOCK_NUMERATOR(CLOCK_NUMERATOR), .CLOCK_DENOMINATOR(CLOCK_DENOMINATOR)) machine (
        .clk_sys(clk_sys),
        .reset(machine_reset),
        .keyboard(keyboard),
        .tape_ready(tape_ready),
        .tape_size(tape_size),
        .tape_data(tape_data),
        .tape_addr_out(tape_addr),
        .tape_busy(tape_busy),
        .save_ready(save_ready), .save_busy(save_busy), .save_changed(save_changed),
        .save_size(save_size), .save_addr(media_read_addr[13:0]), .save_q(save_q),
        .ce_6m5(ce_6m5),
        .video_pixel(video_pixel),
        .hblank(hblank),
        .vblank(vblank),
        .hsync_out(),
        .vsync_out(),
        .halt_n(),
        .cpu_addr(cpu_addr),
        .peek_addr(peek_addr),
        .peek_data(peek_data),
        .ram_address(ram_address), .ram_write_data(ram_write_data),
        .ram_write_enable(ram_write_enable),
        .external_ram_data(8'b0), .external_peek_data(8'b0),
        .bus_addr(bus_addr), .bus_wdata(bus_wdata),
        .bus_mreq_n(bus_mreq_n), .bus_iorq_n(bus_iorq_n),
        .bus_rd_n(bus_rd_n), .bus_wr_n(bus_wr_n),
        .bus_m1_n(bus_m1_n), .bus_rfsh_n(bus_rfsh_n),
        .bus_cpu_clock(bus_cpu_clock),
        .bus_rdata(bus_rdata), .bus_peek_data(bus_peek_data),
        .bus_dsel(bus_dsel), .bus_romcs(bus_romcs),
        .bus_wait(bus_wait), .bus_ram_present(bus_ram_present)
    );
    zx81_video_720p video (
        .clk_sys(clk_sys),
        .ce_6m5(ce_6m5),
        .zx_pixel(video_pixel),
        .hblank(hblank),
        .vblank(vblank),
        .pixel_clk(pixel_clk),
        .red(red),
        .green(green),
        .blue(blue),
        .de(machine_de),
        .hsync(machine_hs),
        .vsync(machine_vs),
        .frame_tick(), .raster_h(raster_h), .raster_v(raster_v),
        .src_x_max(),
        .src_y_max()
    );

    assign machine_video = {1'b1, machine_vs, machine_hs, machine_de, red, green, blue};
    assign audio_sample = bus_ram_present ? 16'sd0 : $signed({1'b0, bus_peek_data, 7'b0});
endmodule
