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

    // One bit per matrix position that this slice maps. A is row 1, column 2.
    wire a_key = down(8'h04);
    wire ret = down(8'h28);
    wire del = down(8'h2A);
    wire space = down(8'h2C);
    wire stop = down(8'h29);
    wire lshift = down(8'hE1);
    wire rshift = down(8'hE5);
    wire ctrl = down(8'hE0);
    wire commodore = down(8'hE2);
    wire right = down(8'h4F);
    wire down_key = down(8'h51);
    wire f1 = down(8'h3A);

    always @* begin
        rows_low = 8'h00;
        if (columns_low[0] && del) rows_low[0] = 1'b1;
        if (columns_low[1] && ret) rows_low[0] = 1'b1;
        if (columns_low[2] && right) rows_low[0] = 1'b1;
        if (columns_low[3] && down_key) rows_low[0] = 1'b1;
        if (columns_low[4] && f1) rows_low[0] = 1'b1;
        if (columns_low[2] && a_key) rows_low[1] = 1'b1;
        if (columns_low[7] && lshift) rows_low[1] = 1'b1;
        if (columns_low[4] && rshift) rows_low[6] = 1'b1;
        if (columns_low[2] && ctrl) rows_low[7] = 1'b1;
        if (columns_low[4] && space) rows_low[7] = 1'b1;
        if (columns_low[5] && commodore) rows_low[7] = 1'b1;
        if (columns_low[7] && stop) rows_low[7] = 1'b1;
    end
endmodule
