// SPDX-License-Identifier: GPL-2.0-or-later
// Open cartridge for one FES C64 socket.
// MODE 0 is the ROM socket: /EXROM pulled, 8 KiB signature at $8000.
// MODE 1 is the I/O socket: scratch at $DE00 and id $C6 at $DE01.
`include "c64_bus.vh"

module cart #(
    parameter integer MODE = 0
) (
    input  wire FPGA_CLK1_50,
    input  wire [`C64_BUS_REQ-1:0] plug_addr,
    output wire [`C64_BUS_RSP-1:0] plug_rdata
);
    wire clk = FPGA_CLK1_50;
    wire [15:0] addr = plug_addr[`C64_BUS_A];
    wire [7:0] wdata = plug_addr[`C64_BUS_D];
    wire bus_read = plug_addr[`C64_BUS_READ];
    wire strobe = plug_addr[`C64_BUS_STROBE];
    wire bus_reset = plug_addr[`C64_BUS_RESET];
    wire roml = plug_addr[`C64_BUS_ROML];
    wire io1 = plug_addr[`C64_BUS_IO1];

    reg [7:0] scratch = 8'h00;
    always @(posedge clk) begin
        if (bus_reset)
            scratch <= 8'h00;
        else if (strobe && !bus_read && io1 && addr[7:0] == 8'h00)
            scratch <= wdata;
    end

    function automatic [7:0] rom_byte;
        input [7:0] offset;
        begin
            case (offset)
                8'h00: rom_byte = "F";
                8'h01: rom_byte = "E";
                8'h02: rom_byte = "S";
                8'h03: rom_byte = "1";
                default: rom_byte = 8'hFF;
            endcase
        end
    endfunction

    wire rom_mode = MODE == 0;
    wire io_mode = MODE == 1;
    // Never drive a write cycle. Keep the response a decoded read, not a
    // direct request-to-response alias through the physical socket buffers.
    wire drive = bus_read && (rom_mode ? roml : io_mode && io1);
    wire [7:0] data = rom_mode ? rom_byte(addr[7:0]) :
                      addr[7:0] == 8'h00 ? scratch :
                      addr[7:0] == 8'h01 ? 8'hC6 : 8'hFF;
    assign plug_rdata = {15'd0, 1'b0, rom_mode, 1'b0, 1'b0, drive, data};
endmodule
