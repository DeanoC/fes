// SPDX-License-Identifier: GPL-2.0-or-later
// Standard FES pc386 with the DE25 atomic control, allocated display path and
// its window in HPS DDR. Board integration supplies qualified clocks, HDMI
// serialization, the display memory crossing/guard and the three window
// masters' crossings into F2SDRAM.
// The computer slave and its transport stay on the board's peripheral clock
// (clk_sys), as on the Spectrum; pc386_system, its window ports, the mailbox
// and the audio source run on machine_clk, the 61.437908 MHz machine clock.
// The GP words, transport health and the reset release cross here.
// No Cyclone V primitive belongs here.
module fes_de25_pc386_standard #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire clk_sys, machine_clk, pixel_clk, reset_n, pixel_reset_n, machine_reset_n,
    input wire video_ready, stream_ready, clock_fault,
    input wire [4:0] computer_address,
    input wire computer_read, computer_write,
    input wire [3:0] computer_byteenable,
    input wire [31:0] computer_writedata,
    output wire [31:0] computer_readdata,
    output wire computer_readdatavalid, computer_writeresponsevalid, computer_waitrequest,
    output wire [1:0] computer_response,
    input wire [6:0] scanout_address,
    input wire scanout_read, scanout_write,
    input wire [3:0] scanout_byteenable,
    input wire [31:0] scanout_writedata,
    output wire [31:0] scanout_readdata,
    output wire scanout_readdatavalid, scanout_writeresponsevalid, scanout_waitrequest,
    output wire [1:0] scanout_response,
    output wire [31:0] avm_address,
    output wire [6:0] avm_burstcount,
    output wire avm_read,
    output wire [15:0] avm_byteenable,
    input wire avm_waitrequest,
    input wire [127:0] avm_readdata,
    input wire avm_readdatavalid,
    input wire [1:0] avm_response,
    // fes.fabric.de25-hps-window's control block, in clk_sys.
    input wire [5:0] window_address,
    input wire window_read, window_write,
    input wire [3:0] window_byteenable,
    input wire [31:0] window_writedata,
    output wire [31:0] window_readdata,
    output wire window_readdatavalid, window_writeresponsevalid, window_waitrequest,
    output wire [1:0] window_response,
    // The window master, in machine_clk, byte addressed: the three ports arbitrated.
    output wire [31:0] win_address,
    output wire [7:0] win_burstcount,
    output wire win_read, win_write,
    output wire [127:0] win_writedata,
    output wire [15:0] win_byteenable,
    input wire win_waitrequest,
    input wire [127:0] win_readdata,
    input wire win_readdatavalid,
    input wire win_writeresponsevalid,
    input wire [1:0] win_response,
    output wire [27:0] video,
    output wire [23:0] video_rgb,
    output wire video_de, video_hs, video_vs,
    output wire signed [15:0] audio_sample,
    output wire exec_reset,
    output wire ui_active, display_faulted, display_quiesced
);
    wire [31:0] gpo, gpi, machine_gpo, machine_gpi;
    wire transport_ready, transport_fault;
    (* preserve, dont_merge *) reg transport_ready_source = 1'b0;
    always @(posedge clk_sys) transport_ready_source <= transport_ready;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] transport_ready_sync = 2'b00;
    always @(posedge machine_clk) transport_ready_sync <= {transport_ready_sync[0], transport_ready_source};
    (* preserve, dont_merge *) reg display_clock_fault = 1'b0;
    always @(posedge clk_sys)
        if (clock_fault || transport_fault) display_clock_fault <= 1'b1;
    reg video_initialized = 1'b0;
    (* preserve, dont_merge *) reg video_ready_source = 1'b0;
    always @(posedge pixel_clk) begin
        if (!pixel_reset_n) video_initialized <= 1'b0;
        else if (video_ready) video_initialized <= 1'b1;
        video_ready_source <= pixel_reset_n && stream_ready && (video_initialized || video_ready);
    end
    assign {video_vs, video_hs, video_de, video_rgb} = video[26:0];
    fes_de25_computer_transport transport (
        .clk(clk_sys), .reset_n(reset_n), .video_ready(video_ready_source),
        .address(computer_address), .read(computer_read), .write(computer_write),
        .byteenable(computer_byteenable), .writedata(computer_writedata),
        .readdata(computer_readdata), .readdatavalid(computer_readdatavalid),
        .writeresponsevalid(computer_writeresponsevalid), .response(computer_response),
        .waitrequest(computer_waitrequest), .gpo(gpo), .gpi(gpi),
        .transport_ready(transport_ready), .faulted(transport_fault)
    );
    fes_gp_crossing gp_crossing (
        .transport_clk(clk_sys), .machine_clk(machine_clk), .gpo(gpo), .gpi(gpi),
        .machine_gpo(machine_gpo), .machine_gpi(machine_gpi)
    );
    // A lost transport holds the machine: its GP words read as idle.
    wire [31:0] held_gpo = transport_ready_sync[1] ? machine_gpo : 32'd0;

    wire ddr_hold, grant_valid, fault_clear, fault_no_grant, fault_outside, window_idle;
    wire [31:0] grant_base;
    wire [2:0] fault_port;
    wire [31:0] w0_address, w1_address, w2_address;
    wire [7:0] w0_burstcount, w1_burstcount, w2_burstcount;
    wire w0_read, w0_write, w1_read, w1_write, w2_read, w2_write;
    wire [127:0] w0_writedata, w0_readdata;
    wire [15:0] w0_byteenable;
    wire [63:0] w1_writedata, w1_readdata, w2_writedata, w2_readdata;
    wire [7:0] w1_byteenable, w2_byteenable;
    wire w0_waitrequest, w0_readdatavalid, w1_waitrequest, w1_readdatavalid, w2_waitrequest, w2_readdatavalid;
    wire p0_reset, p0_drained, p0_read, p0_write, p0_waitrequest, p0_readdatavalid;
    wire [27:0] p0_address;
    wire [7:0] p0_burstcount;
    wire [127:0] p0_writedata, p0_readdata;
    wire [15:0] p0_byteenable;
    wire p1_reset, p1_drained, p1_read, p1_write, p1_waitrequest, p1_readdatavalid;
    wire [28:0] p1_address;
    wire [7:0] p1_burstcount, p1_byteenable;
    wire [63:0] p1_writedata, p1_readdata;
    wire p2_reset, p2_drained, p2_read, p2_write, p2_waitrequest, p2_readdatavalid;
    wire [28:0] p2_address;
    wire [7:0] p2_burstcount, p2_byteenable;
    wire [63:0] p2_writedata, p2_readdata;
    wire [27:0] machine_video;
    wire display_request, response_valid, response_error;
    wire [6:0] display_opcode;
    wire [7:0] display_index;
    wire [15:0] display_argument, response_data;
    /* verilator lint_off PINCONNECTEMPTY */
    pc386_system system (
        .clk(machine_clk), .pixel_clk(pixel_clk), .pixel_locked(pixel_reset_n),
        .gpo(held_gpo), .build_id(BUILD_ID), .gpi(machine_gpi),
        .exec_reset(exec_reset), .ddr_hold(ddr_hold),
        .p0_reset(p0_reset), .p1_reset(p1_reset), .p2_reset(p2_reset),
        .video(machine_video), .sample(audio_sample), .status(), .debug_valid(), .debug_byte(),
        .display_request(display_request), .display_opcode(display_opcode), .display_index(display_index),
        .display_argument(display_argument), .display_response_valid(response_valid),
        .display_response_error(response_error), .display_response_data(response_data),
        .display_quiesced(display_quiesced),
        .p0_address(p0_address), .p0_burstcount(p0_burstcount), .p0_read(p0_read), .p0_write(p0_write),
        .p0_writedata(p0_writedata), .p0_byteenable(p0_byteenable), .p0_waitrequest(p0_waitrequest),
        .p0_readdata(p0_readdata), .p0_readdatavalid(p0_readdatavalid),
        .p1_address(p1_address), .p1_burstcount(p1_burstcount), .p1_read(p1_read), .p1_write(p1_write),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_waitrequest(p1_waitrequest),
        .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid),
        .p2_address(p2_address), .p2_burstcount(p2_burstcount), .p2_read(p2_read), .p2_write(p2_write),
        .p2_writedata(p2_writedata), .p2_byteenable(p2_byteenable), .p2_waitrequest(p2_waitrequest),
        .p2_readdata(p2_readdata), .p2_readdatavalid(p2_readdatavalid)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    fes_de25_hps_window window (
        .hold(ddr_hold || !machine_reset_n), .grant_valid(grant_valid), .grant_base(grant_base),
        .fault_clear(fault_clear), .fault_port(fault_port), .fault_no_grant(fault_no_grant),
        .fault_outside(fault_outside),
        .p0_clk(machine_clk), .p0_reset(p0_reset), .p0_drained(p0_drained),
        .p0_address(p0_address), .p0_burstcount(p0_burstcount), .p0_waitrequest(p0_waitrequest),
        .p0_readdata(p0_readdata), .p0_readdatavalid(p0_readdatavalid), .p0_read(p0_read),
        .p0_writedata(p0_writedata), .p0_byteenable(p0_byteenable), .p0_write(p0_write),
        .p1_clk(machine_clk), .p1_reset(p1_reset), .p1_drained(p1_drained),
        .p1_address(p1_address), .p1_burstcount(p1_burstcount), .p1_waitrequest(p1_waitrequest),
        .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid), .p1_read(p1_read),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_write(p1_write),
        .p2_clk(machine_clk), .p2_reset(p2_reset), .p2_drained(p2_drained),
        .p2_address(p2_address), .p2_burstcount(p2_burstcount), .p2_waitrequest(p2_waitrequest),
        .p2_readdata(p2_readdata), .p2_readdatavalid(p2_readdatavalid), .p2_read(p2_read),
        .p2_writedata(p2_writedata), .p2_byteenable(p2_byteenable), .p2_write(p2_write),
        .m0_address(w0_address), .m0_burstcount(w0_burstcount), .m0_read(w0_read), .m0_write(w0_write),
        .m0_writedata(w0_writedata), .m0_byteenable(w0_byteenable), .m0_waitrequest(w0_waitrequest),
        .m0_readdata(w0_readdata), .m0_readdatavalid(w0_readdatavalid),
        .m1_address(w1_address), .m1_burstcount(w1_burstcount), .m1_read(w1_read), .m1_write(w1_write),
        .m1_writedata(w1_writedata), .m1_byteenable(w1_byteenable), .m1_waitrequest(w1_waitrequest),
        .m1_readdata(w1_readdata), .m1_readdatavalid(w1_readdatavalid),
        .m2_address(w2_address), .m2_burstcount(w2_burstcount), .m2_read(w2_read), .m2_write(w2_write),
        .m2_writedata(w2_writedata), .m2_byteenable(w2_byteenable), .m2_waitrequest(w2_waitrequest),
        .m2_readdata(w2_readdata), .m2_readdatavalid(w2_readdatavalid)
    );
    wire unused = ^{win_response};
    fes_de25_window_arbiter arbiter (
        .clk(machine_clk), .reset(!machine_reset_n),
        .s0_address(w0_address), .s0_burstcount(w0_burstcount), .s0_read(w0_read), .s0_write(w0_write),
        .s0_writedata(w0_writedata), .s0_byteenable(w0_byteenable), .s0_waitrequest(w0_waitrequest),
        .s0_readdata(w0_readdata), .s0_readdatavalid(w0_readdatavalid),
        .s1_address(w1_address), .s1_burstcount(w1_burstcount), .s1_read(w1_read), .s1_write(w1_write),
        .s1_writedata(w1_writedata), .s1_byteenable(w1_byteenable), .s1_waitrequest(w1_waitrequest),
        .s1_readdata(w1_readdata), .s1_readdatavalid(w1_readdatavalid),
        .s2_address(w2_address), .s2_burstcount(w2_burstcount), .s2_read(w2_read), .s2_write(w2_write),
        .s2_writedata(w2_writedata), .s2_byteenable(w2_byteenable), .s2_waitrequest(w2_waitrequest),
        .s2_readdata(w2_readdata), .s2_readdatavalid(w2_readdatavalid),
        .m_address(win_address), .m_burstcount(win_burstcount), .m_read(win_read), .m_write(win_write),
        .m_writedata(win_writedata), .m_byteenable(win_byteenable), .m_waitrequest(win_waitrequest),
        .m_readdata(win_readdata), .m_readdatavalid(win_readdatavalid),
        .m_writeresponsevalid(win_writeresponsevalid), .idle(window_idle)
    );
    fes_de25_hps_window_control window_control (
        .clk_sys(clk_sys), .reset_n(reset_n), .address(window_address), .read(window_read),
        .write(window_write), .byteenable(window_byteenable), .writedata(window_writedata),
        .readdata(window_readdata), .readdatavalid(window_readdatavalid),
        .writeresponsevalid(window_writeresponsevalid), .response(window_response),
        .waitrequest(window_waitrequest), .machine_clk(machine_clk), .machine_reset_n(machine_reset_n),
        .grant_valid(grant_valid),
        .grant_base(grant_base), .fault_clear(fault_clear),
        .machine_drained(window_idle && p0_drained && p1_drained && p2_drained), .machine_held(ddr_hold),
        .fault_port(fault_port), .fault_no_grant(fault_no_grant), .fault_outside(fault_outside)
    );
    // The pc386 picture is 960x720 at the left 160, as on the DE10.
    fes_de25_session_display #(.BUILD_ID(BUILD_ID),
        .PICTURE_LEFT(16'd160), .PICTURE_TOP(16'd0),
        .PICTURE_WIDTH(16'd960), .PICTURE_HEIGHT(16'd720)) display (
        .clk_sys(machine_clk), .pixel_clk(pixel_clk), .reset_n(pixel_reset_n), .exec_reset(exec_reset),
        .clock_fault(display_clock_fault), .display_ready(video_ready), .stream_ready(stream_ready),
        .machine(machine_video), .request(display_request), .opcode(display_opcode),
        .index(display_index), .argument(display_argument), .response_valid(response_valid),
        .response_error(response_error), .response_data(response_data), .quiesced_sys(display_quiesced),
        .video(video), .ui_active(ui_active), .locked(), .faulted(display_faulted),
        .address(scanout_address), .read(scanout_read), .write(scanout_write),
        .byteenable(scanout_byteenable), .writedata(scanout_writedata),
        .readdata(scanout_readdata), .readdatavalid(scanout_readdatavalid),
        .writeresponsevalid(scanout_writeresponsevalid), .response(scanout_response),
        .waitrequest(scanout_waitrequest), .avm_address(avm_address), .avm_burstcount(avm_burstcount),
        .avm_read(avm_read), .avm_byteenable(avm_byteenable), .avm_waitrequest(avm_waitrequest),
        .avm_readdata(avm_readdata), .avm_readdatavalid(avm_readdatavalid), .avm_response(avm_response)
    );
endmodule
