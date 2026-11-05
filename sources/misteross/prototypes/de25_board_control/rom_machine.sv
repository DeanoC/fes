// SPDX-License-Identifier: GPL-3.0-or-later
// Simulation integration of the real machine and the ROM stream sink. No
// physical producer selects this wrapper or supplies a reader/CSR interface.
module rom_machine #(
    parameter integer ROM_BYTES = 8192
) (
    input wire clk,
    input wire reset_n,
    input wire cpu_held,
    input wire start,
    input wire abort_load,
    input wire [31:0] expected_crc32,
    input wire stream_valid,
    output wire stream_ready,
    input wire [127:0] stream_data,
    input wire stream_last,
    output wire loaded,
    output wire failed,
    output wire [13:0] received,
    output wire [13:0] written,
    output wire [31:0] observed_crc32,
    output wire halted,
    output wire [7:0] result_byte,
    output reg [31:0] cpu_writes = 0,
    output reg [31:0] cpu_fetches = 0
);
    wire [12:0] rom_address;
    wire [7:0] rom_data;
    wire halt_n, ram_we, cpu_mreq_n, cpu_m1_n;
    wire machine_reset = !reset_n || cpu_held || !loaded || failed;
    assign halted = !halt_n;
    fes_embedded_rom #(.ROM_BYTES(ROM_BYTES)) image (
        .clk(clk),.reset_n(reset_n),.cpu_held(cpu_held),.start(start),.abort_load(abort_load),
        .expected_crc32(expected_crc32),.stream_valid(stream_valid),.stream_ready(stream_ready),
        .stream_data(stream_data),.stream_last(stream_last),.loaded(loaded),.failed(failed),
        .received(received),.written(written),.observed_crc32(observed_crc32),
        .read_address(rom_address),.read_data(rom_data)
    );
    zx81_machine #(.EXTERNAL_ROM(1)) machine (
        .clk_sys(clk),.reset(machine_reset),.keyboard(40'hffffffffff),
        .rom_address(rom_address),.external_rom_data(rom_data),
        .tape_ready(1'b0),.tape_size(15'd0),.tape_data(8'd0),.tape_addr_out(),.tape_busy(),
        .save_ready(),.save_busy(),.save_changed(),.save_size(),.save_addr(14'd0),.save_q(),
        .ce_6m5(),.video_pixel(),.hblank(),.vblank(),.hsync_out(),.vsync_out(),
        .halt_n(halt_n),.cpu_addr(),.peek_addr(16'h4000),.peek_data(result_byte),
        .ram_address(),.ram_write_data(),.ram_write_enable(ram_we),
        .external_ram_data(8'd0),.external_peek_data(8'd0),
        .bus_addr(),.bus_wdata(),.bus_mreq_n(cpu_mreq_n),.bus_iorq_n(),.bus_rd_n(),.bus_wr_n(),
        .bus_m1_n(cpu_m1_n),.bus_rfsh_n(),.bus_cpu_clock(),.bus_rdata(8'd0),.bus_peek_data(8'd0),
        .bus_dsel(1'b0),.bus_romcs(1'b0),.bus_wait(1'b0),.bus_ram_present(1'b0)
    );
    always @(posedge clk) begin
        if (!reset_n) cpu_writes <= 0;
        else if (!machine_reset && ram_we) cpu_writes <= cpu_writes + 1;
        if (!reset_n) cpu_fetches <= 0;
        else if (!cpu_mreq_n && !cpu_m1_n) cpu_fetches <= cpu_fetches + 1;
    end
endmodule
