// SPDX-License-Identifier: GPL-2.0-or-later
// Dormant 32 KiB store lent by the shell to a claiming expansion module.
// Reset does not clear the array: visibility is controlled by the module.
module coleco_expansion_ram (
    input wire clk,
    input wire [14:0] address,
    input wire [7:0] write_data,
    input wire write_enable,
    output reg [7:0] read_data
);
    reg [7:0] memory [0:32767];
    always @(posedge clk) begin
        if (write_enable) memory[address] <= write_data;
        read_data <= memory[address];
    end
endmodule
