// SPDX-License-Identifier: MIT
// Standalone native drive test with the production combined firmware ROM.
module c64_drive_sim_top (
    input wire clk, reset,
    input wire atn_low, host_clock_low, host_data_low,
    output wire drive_clock_low, drive_data_low,
    input wire [7:0] disk_data, disk_pb,
    input wire byte_ready_n,
    output wire [7:0] disk_pa_out, disk_pa_ddr, disk_pb_out, disk_pb_ddr,
    output wire byte_ready_enable, read_mode,
    output wire [15:0] debug_pc,
    output wire ref_ce,
    output wire phi,
    output wire [13:0] rom_address
);
    wire [7:0] rom_data;
    c64_rom rom(.clk(clk), .address(15'd0), .data(),
        .video_clk(clk), .video_address(12'd0), .video_data(),
        .drive_address(rom_address), .drive_data(rom_data));
    c64_drive1541 drive(.*);
endmodule
