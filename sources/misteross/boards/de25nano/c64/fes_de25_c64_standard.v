// SPDX-License-Identifier: GPL-2.0-or-later
// Standard FES Commodore 64 with the DE25 atomic control and allocated
// display path. Board integration supplies qualified clocks, HDMI
// serialization, the display memory crossing/guard and the SDRAM pads.
// The computer slave and its transport stay on the board's peripheral clock
// (clk_sys), as on the ZX81; the machine, mailbox and audio source run on
// machine_clk, the machine's 52.224 MHz, which has no phase relation to it.
// The GP words, transport health and the reset release cross here. The C64
// keeps its RAM, firmware and disk in block RAM as on the DE10, so the board
// SDRAM is held deselected with its clock disabled.
// No Cyclone V primitive belongs here.
module fes_de25_c64_standard #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    // Board-owned releases are qualified independently in their clock domains.
    input wire clk_sys, machine_clk, pixel_clk, reset_n, pixel_reset_n,
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
    output wire [27:0] video,
    output wire [23:0] video_rgb,
    output wire video_de, video_hs, video_vs,
    output wire signed [15:0] audio_sample,
    output wire exec_reset,
    output wire dram_clk, dram_cke,
    output wire [12:0] dram_addr,
    output wire [1:0] dram_ba, dram_cs_n, dram_dqm,
    output wire dram_ras_n, dram_cas_n, dram_we_n,
    input wire [15:0] dram_dq_i,
    output wire [15:0] dram_dq_o,
    output wire dram_dq_oe,
    output wire ui_active, display_faulted, display_quiesced
);
    wire [31:0] gpo, gpi, machine_gpo, machine_gpi;
    wire transport_ready, transport_fault;
    // Transport health is one registered level into the machine clock. A
    // fault or reset there holds the machine two machine clocks later.
    (* preserve, dont_merge *) reg transport_ready_source = 1'b0;
    always @(posedge clk_sys) transport_ready_source <= transport_ready;
    (* async_reg = "true", altera_attribute = "-name SYNCHRONIZER_IDENTIFICATION FORCED_IF_ASYNCHRONOUS" *) reg [1:0] transport_ready_sync = 2'b00;
    always @(posedge machine_clk) transport_ready_sync <= {transport_ready_sync[0], transport_ready_source};
    wire core_reset_hold = !transport_ready_sync[1];
    // Launch one monotonic system-domain fault bit into the pixel synchronizer.
    (* preserve, dont_merge *) reg display_clock_fault = 1'b0;
    always @(posedge clk_sys)
        if (clock_fault || transport_fault) display_clock_fault <= 1'b1;
    // Preserve initial transmitter admission, then qualify running execution
    // only by the existing transport epoch, as the ZX81 wrapper does.
    reg video_initialized = 1'b0;
    (* preserve, dont_merge *) reg video_ready_source = 1'b0;
    always @(posedge pixel_clk) begin
        if (!pixel_reset_n) video_initialized <= 1'b0;
        else if (video_ready) video_initialized <= 1'b1;
        video_ready_source <= pixel_reset_n && stream_ready && (video_initialized || video_ready);
    end
    wire [27:0] machine_video;
    wire display_request, response_valid, response_error;
    wire [6:0] display_opcode;
    wire [7:0] display_index;
    wire [15:0] display_argument, response_data;
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

    fes_c64_core #(.BUILD_ID(BUILD_ID)) core (
        .clk_sys(machine_clk), .pixel_clk(pixel_clk), .reset_hold(core_reset_hold),
        .gpo(machine_gpo), .gpi(machine_gpi), .exec_reset(exec_reset), .machine_video(machine_video),
        .audio_sample(audio_sample), .display_request(display_request),
        .display_opcode(display_opcode), .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(response_valid), .display_response_error(response_error),
        .display_response_data(response_data), .display_quiesced(display_quiesced)
    );
    // The board SDRAM is unused: clock disabled, both chips deselected and
    // no command, the data pins released.
    assign dram_clk = 1'b0;
    assign dram_cke = 1'b0;
    assign dram_addr = 13'd0;
    assign dram_ba = 2'd0;
    assign dram_cs_n = 2'b11;
    assign dram_dqm = 2'b11;
    assign {dram_ras_n, dram_cas_n, dram_we_n} = 3'b111;
    assign dram_dq_o = 16'd0;
    assign dram_dq_oe = 1'b0;
    // The 384x272 PAL picture is scaled into a centred 960x720 viewport.
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
