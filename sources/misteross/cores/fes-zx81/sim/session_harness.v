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
    output wire [23:0] machine_rgb,
    output wire de, hs, vs,
    output wire [27:0] machine_word, hdmi_word,
    output wire ui_active, faulted, quiesced,
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
        .tape_addr_out(tape_addr), .tape_busy(tape_busy),
        .save_ready(), .save_busy(), .save_changed(), .save_size(),
        .save_addr(14'd0), .save_q(), .ce_6m5(ce_6m5),
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
    assign machine_word = {1'b1, vs, hs, de, machine_rgb};
    wire ddr_hold, ddr_reset, ddr_drained, ddr_read, ddr_waitrequest, ddr_readdatavalid;
    wire [27:0] ddr_address;
    wire [7:0] ddr_burstcount;
    wire [127:0] ddr_readdata;
    fes_display_plane #(
        .PICTURE_LEFT(16'd312), .PICTURE_TOP(16'd90),
        .PICTURE_WIDTH(16'd656), .PICTURE_HEIGHT(16'd540)
    ) display (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(reset_hold), .exec_reset(exec_reset),
        .request(request), .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .quiesced_sys(quiesced), .machine(machine_word), .video(hdmi_word), .ui_active(ui_active), .locked(),
        .displayed_sequence(displayed_sequence), .underflows(underflows), .faulted(faulted),
        .ddr_hold(ddr_hold), .ddr_reset(ddr_reset), .ddr_drained(ddr_drained),
        .ddr_address(ddr_address), .ddr_burstcount(ddr_burstcount), .ddr_read(ddr_read),
        .ddr_waitrequest(ddr_waitrequest), .ddr_readdata(ddr_readdata),
        .ddr_readdatavalid(ddr_readdatavalid)
    );
    // As the ZX81 top wires it: the plane reads port 0; ports 1 and 2 are off.
    fes_hps_ddr #(.P0_WRITE_ENABLE(1'b0), .P1_ENABLE(1'b0), .P2_ENABLE(1'b0)) memory (
        .hold(ddr_hold),
        .p0_clk(pixel_clk), .p0_reset(ddr_reset), .p0_drained(ddr_drained),
        .p0_address(ddr_address), .p0_burstcount(ddr_burstcount),
        .p0_waitrequest(ddr_waitrequest), .p0_readdata(ddr_readdata),
        .p0_readdatavalid(ddr_readdatavalid), .p0_read(ddr_read),
        .p0_writedata(128'd0), .p0_byteenable(16'd0), .p0_write(1'b0),
        .p1_clk(pixel_clk), .p1_reset(), .p1_drained(), .p1_address(29'd0), .p1_burstcount(8'd0),
        .p1_waitrequest(), .p1_readdata(), .p1_readdatavalid(), .p1_read(1'b0),
        .p1_writedata(64'd0), .p1_byteenable(8'd0), .p1_write(1'b0),
        .p2_clk(pixel_clk), .p2_reset(), .p2_drained(), .p2_address(29'd0), .p2_burstcount(8'd0),
        .p2_waitrequest(), .p2_readdata(), .p2_readdatavalid(), .p2_read(1'b0),
        .p2_writedata(64'd0), .p2_byteenable(8'd0), .p2_write(1'b0)
    );
endmodule
