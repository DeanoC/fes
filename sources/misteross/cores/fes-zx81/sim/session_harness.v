// SPDX-License-Identifier: GPL-2.0-or-later
module zx81_session_harness (
    input wire clk_sys, pixel_clk, reset_hold,
    input wire [31:0] gpo,
    input wire force_media_busy,
    input wire [15:0] peek_addr,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire [39:0] keyboard,
    output wire [7:0] peek_data,
    output wire [15:0] cpu_addr,
    output reg [31:0] cpu_writes = 32'd0,
    output wire [10:0] raster_h,
    output wire [9:0] raster_v,
    output wire [23:0] machine_rgb, rgb,
    output wire de, hs, vs, ui_active, faulted, quiesced,
    output wire [31:0] displayed_sequence, underflows
);
    wire request, response_valid, response_error;
    wire [6:0] opcode;
    wire [7:0] index;
    wire [15:0] argument, response_data;
    wire tape_ready, tape_busy;
    wire [14:0] tape_size;
    wire [13:0] tape_addr;
    wire [7:0] tape_data;
    fes_computer_gp #(.ENABLE_SESSION_DISPLAY(1)) gp (
        .clk(clk_sys), .gpo(gpo), .build_id(128'd0), .gpi(gpi), .exec_reset(exec_reset),
        .keyboard(keyboard), .media_ready(tape_ready), .media_size(tape_size),
        .media_byte0(), .media_byte1(), .media_byte2(), .media_addr(tape_addr),
        .media_q(tape_data), .media_busy(tape_busy || force_media_busy),
        .display_request(request), .display_opcode(opcode), .display_index(index), .display_argument(argument),
        .display_response_valid(response_valid), .display_response_error(response_error),
        .display_response_data(response_data), .display_quiesced(quiesced)
    );
    wire ce_6m5, video_pixel, hblank, vblank, ram_we;
    zx81_machine #(.FIRMWARE_INIT("build/sim/fes-zx81-session/session-rom.hex")) machine (
        .clk_sys(clk_sys), .reset(exec_reset), .keyboard(keyboard),
        .tape_ready(tape_ready), .tape_size(tape_size), .tape_data(tape_data),
        .tape_addr_out(tape_addr), .tape_busy(tape_busy), .ce_6m5(ce_6m5),
        .video_pixel(video_pixel), .hblank(hblank), .vblank(vblank),
        .hsync_out(), .vsync_out(), .halt_n(), .cpu_addr(cpu_addr),
        .peek_addr(peek_addr), .peek_data(peek_data),
        .ram_address(), .ram_write_data(), .ram_write_enable(ram_we),
        .external_ram_data(8'd0), .external_peek_data(8'd0),
        .bus_addr(), .bus_wdata(), .bus_mreq_n(), .bus_iorq_n(), .bus_rd_n(), .bus_wr_n(),
        .bus_m1_n(), .bus_rfsh_n(), .bus_cpu_clock(), .bus_rdata(8'd0), .bus_peek_data(8'd0),
        .bus_dsel(1'b0), .bus_romcs(1'b0), .bus_wait(1'b0), .bus_ram_present(1'b0)
    );
    always @(posedge clk_sys) if (ram_we && !exec_reset) cpu_writes <= cpu_writes + 32'd1;
    zx81_video_720p video (
        .clk_sys(clk_sys), .ce_6m5(ce_6m5), .zx_pixel(video_pixel), .hblank(hblank), .vblank(vblank),
        .pixel_clk(pixel_clk), .red(machine_rgb[23:16]), .green(machine_rgb[15:8]), .blue(machine_rgb[7:0]),
        .de(de), .hsync(hs), .vsync(vs), .frame_tick(),
        .raster_h(raster_h), .raster_v(raster_v), .src_x_max(), .src_y_max()
    );
    zx81_session_display display (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(reset_hold), .exec_reset(exec_reset),
        .request(request), .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .quiesced_sys(quiesced), .raster_h(raster_h), .raster_v(raster_v), .machine_rgb(machine_rgb),
        .rgb(rgb), .ui_active(ui_active), .displayed_sequence(displayed_sequence),
        .underflows(underflows), .faulted(faulted)
    );
endmodule
