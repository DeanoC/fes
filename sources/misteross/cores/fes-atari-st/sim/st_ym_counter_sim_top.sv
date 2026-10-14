// SPDX-License-Identifier: GPL-2.0-or-later
module st_ym_counter_sim_top (
    input wire clk, reset_n, chip_ce, address_write, data_write,
    input wire [7:0] data,
    output wire [14:0] levels
);
    wire [7:0] unused_read, unused_pcm;
    zonx_ay #(.YM2149(1)) dut (
        .clk(clk), .reset_n(reset_n), .chip_ce(chip_ce),
        .address_write(address_write), .data_write(data_write), .data(data),
        .read_data(unused_read), .pcm(unused_pcm), .ym_levels(levels)
    );
endmodule
