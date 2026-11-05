// SPDX-License-Identifier: GPL-3.0-or-later
// Host fixture of the actual board computer and ROM Avalon peripherals.
module rom_board_machine #(
    parameter integer ROM_BYTES = 8192,
    parameter [127:0] BUILD_ID = 128'h00112233445566778899aabbccddeeff
) (
    input wire clk, reset_n,
    input wire [6:0] rom_offset,
    input wire rom_read, rom_write,
    input wire [3:0] rom_strobes,
    input wire [31:0] rom_wdata,
    output wire [31:0] rom_rdata,
    output wire rom_rvalid, rom_wvalid, rom_wait,
    output wire [1:0] rom_response,
    input wire [4:0] computer_offset,
    input wire computer_read, computer_write,
    input wire [31:0] computer_wdata,
    output wire [31:0] computer_rdata,
    output wire computer_rvalid, computer_wvalid, computer_wait,
    output wire [1:0] computer_response,
    output wire held, loaded, failed, halted, ce_6m5,
    output wire [7:0] marker,
    output wire [31:0] fetches
);
    wire [12:0] rom_address;
    wire [7:0] rom_data;
    fes_de25_rom #(.ROM_BYTES(ROM_BYTES),.BUILD_ID(BUILD_ID)) image (
        .clk(clk),.reset_n(reset_n),.address(rom_offset),.read(rom_read),.write(rom_write),
        .byteenable(rom_strobes),.writedata(rom_wdata),.readdata(rom_rdata),
        .readdatavalid(rom_rvalid),.writeresponsevalid(rom_wvalid),
        .response(rom_response),.waitrequest(rom_wait),
        .rom_address(rom_address),.rom_data(rom_data),.rom_cpu_held(held),
        .rom_loaded(loaded),.rom_failed(failed)
    );
    fes_de25_computer #(.BUILD_ID(BUILD_ID)) computer (
        .clk(clk),.pixel_clk(clk),.reset_n(reset_n),.address(computer_offset),
        .read(computer_read),.write(computer_write),.byteenable(4'hf),
        .writedata(computer_wdata),.readdata(computer_rdata),
        .readdatavalid(computer_rvalid),.writeresponsevalid(computer_wvalid),
        .response(computer_response),.waitrequest(computer_wait),
        .video_rgb(),.video_de(),.video_hs(),.video_vs(),
        .expansion_running(1'b0),.diagnostic_address(8'd0),.expansion_response(20'd0),
        .expansion_request(),.memory_peek(marker),
        .rom_address(rom_address),.rom_data(rom_data),.rom_loaded(loaded),.rom_failed(failed),.rom_cpu_held(held)
    );
    assign halted = !computer.halt_n;
    assign ce_6m5 = computer.ce_6m5;
    assign fetches = computer.fetch_count;
endmodule
