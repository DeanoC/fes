// SPDX-License-Identifier: GPL-2.0-or-later
// USB HID key state to the C64 keyboard matrix. columns_low is active-high
// for columns whose CIA pin is electrically low. rows_low is active-high for
// matrix rows that should read low.
module c64_keyboard (
    input  wire [143:0] rows,
    input  wire [7:0]   columns_low,
    output reg  [7:0]   rows_low
);
    function automatic down;
        input [7:0] usage;
        begin
            if (usage < 8'd128) down = rows[usage];
            else if (usage >= 8'hE0 && usage < 8'hE8) down = rows[128 + usage[2:0]];
            else down = 1'b0;
        end
    endfunction

    // PA selects a column; PB senses its rows, as in the stock KERNAL
    // unshifted table at $EB81. Ordinary HID keys use native C64 semantics.
    wire shift = down(8'hE1) | down(8'h50) | down(8'h52) |
                 down(8'h3B) | down(8'h3D) | down(8'h3F) | down(8'h41);
    wire [7:0] column0 = {down(8'h51) | down(8'h52),
        down(8'h3E) | down(8'h3F), down(8'h3C) | down(8'h3D),
        down(8'h3A) | down(8'h3B), down(8'h40) | down(8'h41),
        down(8'h4F) | down(8'h50), down(8'h28), down(8'h2A)};
    wire [7:0] column1 = {shift, down(8'h08), down(8'h16), down(8'h1D), down(8'h21), down(8'h04), down(8'h1A), down(8'h20)};
    wire [7:0] column2 = {down(8'h1B), down(8'h17), down(8'h09), down(8'h06), down(8'h23), down(8'h07), down(8'h15), down(8'h22)};
    wire [7:0] column3 = {down(8'h19), down(8'h18), down(8'h0B), down(8'h05), down(8'h25), down(8'h0A), down(8'h1C), down(8'h24)};
    wire [7:0] column4 = {down(8'h11), down(8'h12), down(8'h0E), down(8'h10), down(8'h27), down(8'h0D), down(8'h0C), down(8'h26)};
    wire [7:0] column5 = {down(8'h36), down(8'h2F), down(8'h34), down(8'h37), down(8'h2D), down(8'h0F), down(8'h13), down(8'h57)};
    wire [7:0] column6 = {down(8'h38), down(8'h4B), down(8'h2E), down(8'hE5), down(8'h4A), down(8'h33), down(8'h30), down(8'h31)};
    wire [7:0] column7 = {down(8'h29), down(8'h14), down(8'hE2) | down(8'hE6), down(8'h2C), down(8'h1F), down(8'hE0) | down(8'hE4), down(8'h35), down(8'h1E)};

    always @* begin
        rows_low = (column0 & {8{columns_low[0]}}) |
                   (column1 & {8{columns_low[1]}}) |
                   (column2 & {8{columns_low[2]}}) |
                   (column3 & {8{columns_low[3]}}) |
                   (column4 & {8{columns_low[4]}}) |
                   (column5 & {8{columns_low[5]}}) |
                   (column6 & {8{columns_low[6]}}) |
                   (column7 & {8{columns_low[7]}});
    end
endmodule
