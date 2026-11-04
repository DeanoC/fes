// SPDX-License-Identifier: GPL-2.0-or-later
// Standard FES machine with the DE25 atomic control and allocated display path.
// Board integration supplies qualified clocks, HDMI serialization, memory
// crossing/guard and expansion placement. No Cyclone V primitive belongs here.
module fes_de25_zx81_standard #(
    parameter [127:0] BUILD_ID = 128'd0,
    parameter EXPANSION_SOCKET = 1,
    parameter FIRMWARE_INIT = ""
) (
    // Board-owned releases are qualified independently in their clock domains.
    input wire clk_sys, pixel_clk, reset_n, pixel_reset_n,
    input wire video_ready, clock_fault, expansion_running,
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
    output wire [4:0] avm_burstcount,
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
    output wire [15:0] bus_addr,
    output wire [7:0] bus_wdata,
    output wire bus_mreq_n, bus_iorq_n, bus_rd_n, bus_wr_n, bus_m1_n, bus_rfsh_n,
    output wire bus_cpu_clock, bus_reset,
    input wire [7:0] bus_rdata, bus_peek_data,
    input wire bus_dsel, bus_romcs, bus_wait, bus_ram_present,
    input wire [15:0] peek_addr,
    output wire [7:0] peek_data,
    output wire [15:0] cpu_addr,
    output wire ram_write_enable,
    output wire ui_active, display_faulted, display_quiesced
);
    wire [31:0] gpo, gpi;
    wire transport_ready, transport_fault;
    wire core_reset_hold = !transport_ready || (EXPANSION_SOCKET && !expansion_running);
    // Export the same full reset seen by the machine. Execution HOLD alone
    // does not include transport failure or an expansion freeze.
    assign bus_reset = exec_reset || core_reset_hold;
    // Launch one monotonic system-domain fault bit into the pixel synchronizer.
    // Reset/fault decoding may involve several transport registers; it must
    // settle in this clock domain before crossing to the memory controller.
    (* preserve, dont_merge *) reg display_clock_fault = 1'b0;
    always @(posedge clk_sys)
        if (clock_fault || transport_fault) display_clock_fault <= 1'b1;
    // Board visibility is a pixel-domain decode. Launch a registered level
    // into the transport's system-domain readiness synchronizer.
    (* preserve, dont_merge *) reg video_ready_source = 1'b0;
    always @(posedge pixel_clk) video_ready_source <= video_ready;
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
    fes_zx81_core #(.BUILD_ID(BUILD_ID), .EXPANSION_SOCKET(EXPANSION_SOCKET),
        .HPS_DDR_WINDOW(0), .FIRMWARE_INIT(FIRMWARE_INIT),
        .CLOCK_NUMERATOR(13), .CLOCK_DENOMINATOR(200)) core (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk),
        .reset_hold(core_reset_hold),
        .gpo(gpo), .gpi(gpi), .exec_reset(exec_reset), .machine_video(machine_video),
        .audio_sample(audio_sample), .display_request(display_request),
        .display_opcode(display_opcode), .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(response_valid), .display_response_error(response_error),
        .display_response_data(response_data), .display_quiesced(display_quiesced),
        .bus_addr(bus_addr), .bus_wdata(bus_wdata), .bus_mreq_n(bus_mreq_n), .bus_iorq_n(bus_iorq_n),
        .bus_rd_n(bus_rd_n), .bus_wr_n(bus_wr_n), .bus_m1_n(bus_m1_n), .bus_rfsh_n(bus_rfsh_n),
        .bus_cpu_clock(bus_cpu_clock), .bus_rdata(bus_rdata), .bus_peek_data(bus_peek_data),
        .bus_dsel(bus_dsel), .bus_romcs(bus_romcs), .bus_wait(bus_wait), .bus_ram_present(bus_ram_present),
        .peek_addr(peek_addr), .peek_data(peek_data), .cpu_addr(cpu_addr),
        .ram_write_enable(ram_write_enable), .raster_h(), .raster_v()
    );
    fes_de25_session_display #(.BUILD_ID(BUILD_ID)) display (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_n(pixel_reset_n), .exec_reset(exec_reset),
        .clock_fault(display_clock_fault), .display_ready(video_ready),
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
