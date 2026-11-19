// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum ROM filled at launch (fes.fabric.firmware-upload): 16, 32, 48 or
// 64 KiB by build model, in the page order of spectrum_rom. Uploaded byte
// pairs always start at an even address, so the image is two byte lanes with
// one write port each. A read has the two registered stages of spectrum_rom:
// data follows its address by two clocks. The contents power up undefined and
// the firmware front end keeps the machine held until an image is committed.
// No Sinclair or Amstrad ROM bytes are in this repository or the package.
`include "spectrum_model.vh"
module fes_spectrum_firmware_rom (
    input  wire        clk,
    input  wire [15:0] address,
    output reg  [7:0]  data,
    input  wire [15:0] write_addr,
    input  wire [15:0] write_data,
    input  wire [1:0]  write_enable
);
`ifdef SPECTRUM_ROM_64K
    localparam integer KIB = 64;
`elsif SPECTRUM_ROM_48K
    localparam integer KIB = 48;
`elsif SPECTRUM_ROM_32K
    localparam integer KIB = 32;
`else
    localparam integer KIB = 16;
`endif
    localparam integer AW = KIB > 32 ? 16 : KIB > 16 ? 15 : 14;
    // 48 KiB is three pages; the Pentagon never addresses a fourth.
    wire [15:0] used = address & 16'((1 << AW) - 1);
    (* ramstyle = "M20K" *) reg [7:0] even [0:(1 << (AW - 1)) - 1];
    (* ramstyle = "M20K" *) reg [7:0] odd [0:(1 << (AW - 1)) - 1];
    reg [7:0] even_q, odd_q;
    reg lane;
    always @(posedge clk) begin
        if (write_enable[0]) even[write_addr[AW-1:1]] <= write_data[7:0];
        if (write_enable[1]) odd[write_addr[AW-1:1]] <= write_data[15:8];
        even_q <= even[used[AW-1:1]];
        odd_q <= odd[used[AW-1:1]];
        lane <= used[0];
        data <= lane ? odd_q : even_q;
    end
endmodule
