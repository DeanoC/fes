// SPDX-License-Identifier: GPL-3.0-or-later
`ifndef FES_DE25_COMPUTER_BUILD_ID
`define FES_DE25_COMPUTER_BUILD_ID 128'd0
`endif
// One bounded-response Avalon slave, sharing the computer's clock/reset.
// It initiates no memory traffic. No address or execution command is writable.
module fes_de25_rom #(
    parameter integer ROM_BYTES = 8192,
    parameter [127:0] BUILD_ID = `FES_DE25_COMPUTER_BUILD_ID
) (
    input wire clk, reset_n,
    input wire [6:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata,
    output reg readdatavalid, writeresponsevalid,
    output reg [1:0] response,
    output wire waitrequest,
    input wire [12:0] rom_address,
    output wire [7:0] rom_data,
    input wire rom_cpu_held,
    output wire rom_loaded, rom_failed
);
    reg busy;
    assign waitrequest = !reset_n || busy;
    wire accept = (read || write) && !waitrequest;
    wire malformed = (read && write) || byteenable != 4'hf;
    wire [31:0] value;
    wire error;
    // Progress is consumed through the CSR bank, not separate board pins.
    wire [13:0] unused_received, unused_written;
    wire [31:0] unused_crc;
    fes_embedded_rom_csr #(.ROM_BYTES(ROM_BYTES),.BUILD_ID(BUILD_ID)) image (
        .clk(clk),.reset_n(reset_n),.cpu_held(rom_cpu_held),
        .csr_valid(accept),.csr_write(malformed ? 1'b1 : write),.csr_address(address),
        // A malformed bus request is an invalid partial write. The existing
        // CSR fault path retires it without performing the requested access.
        .csr_wstrb(malformed ? 4'd0 : byteenable),.csr_wdata(writedata),
        .csr_rdata(value),.csr_error(error),.loaded(rom_loaded),.failed(rom_failed),
        .received(unused_received),.written(unused_written),.observed_crc(unused_crc),
        .read_address(rom_address),.read_data(rom_data)
    );
    always @(posedge clk) begin
        if (!reset_n) begin
            busy<=0;readdata<=0;readdatavalid<=0;writeresponsevalid<=0;response<=0;
        end else begin
            busy<=accept;
            readdatavalid<=accept && read;
            writeresponsevalid<=accept && write;
            response<=accept && error ? 2'b10 : 2'b00;
            if (accept && read) readdata<=value;
        end
    end
endmodule
