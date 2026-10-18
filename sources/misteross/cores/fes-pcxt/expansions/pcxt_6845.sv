// SPDX-License-Identifier: GPL-2.0-or-later
// MC6845-compatible non-interlaced address/timing subset. No HD6845 extensions.
module pcxt_6845 (
    input wire clk, reset, char_ce,
    input wire select_write, data_write,
    input wire [7:0] wdata,
    output reg [7:0] rdata,
    input wire light_pen,
    output wire [13:0] ma,
    output wire [4:0] ra,
    output wire display_enable, hsync, vsync, cursor,
    output wire line_end, frame_end,
    output wire unsupported_interlace
);
    reg [4:0] index;
    reg [7:0] registers [0:15];
    reg [13:0] pen_address, row_address;
    reg [7:0] horizontal;
    reg [6:0] row;
    reg [4:0] scan, adjust, vs_left, hs_left;
    reg adjusting;
    reg [5:0] frames;
    wire [13:0] start_address = {registers[12][5:0], registers[13]};
    wire [13:0] cursor_address = {registers[14][5:0], registers[15]};
    assign ma = row_address + {6'b0, horizontal};
    assign ra = scan;
    assign display_enable = horizontal < registers[1] && row < registers[6][6:0] && !adjusting;
    assign hsync = hs_left != 0;
    assign vsync = vs_left != 0;
    assign line_end = horizontal == registers[0];
    assign frame_end = line_end && (adjusting ? adjust == registers[5][4:0]-1'b1 :
                           row == registers[4][6:0] && scan == registers[9][4:0] && registers[5][4:0] == 0);
    assign unsupported_interlace = registers[8][0]; // IBM's standard 02h is non-interlaced.
    wire cursor_blink = registers[10][6:5] == 0 ||
                        (registers[10][6] && !(registers[10][5] ? frames[5] : frames[4]));
    assign cursor = display_enable && ma == cursor_address && cursor_blink &&
                    scan >= registers[10][4:0] && scan <= registers[11][4:0];
    integer i;
    // MC6845 R0..R13 are write-only; cursor and light-pen addresses read back.
    always @* begin
        rdata = 8'hff;
        case (index)
            14: rdata = {2'b0, registers[14][5:0]};
            15: rdata = registers[15];
            16: rdata = {2'b0, pen_address[13:8]};
            17: rdata = pen_address[7:0];
            default: begin end
        endcase
    end
    always @(posedge clk) begin
        if (reset) begin
            index <= 0; pen_address <= 0; row_address <= 0;
            horizontal <= 0; row <= 0; scan <= 0; adjust <= 0;
            adjusting <= 0; frames <= 0; hs_left <= 0; vs_left <= 0;
            for (i=0; i<16; i=i+1) registers[i] <= 0;
        end else begin
            if (select_write) index <= wdata[4:0];
            if (data_write && index < 16) begin
                case (index)
                    4,6,7: registers[index[3:0]] <= {1'b0, wdata[6:0]};
                    5,9,11: registers[index[3:0]] <= {3'b0, wdata[4:0]};
                    10: registers[index[3:0]] <= {1'b0, wdata[6:0]};
                    12,14: registers[index[3:0]] <= {2'b0, wdata[5:0]};
                    default: registers[index[3:0]] <= wdata;
                endcase
            end
            if (light_pen) pen_address <= ma;
            if (char_ce) begin
                if (hs_left != 0) hs_left <= hs_left - 1'b1;
                if ((line_end ? 8'b0 : horizontal + 1'b1) == registers[2])
                    hs_left <= {1'b0, registers[3][3:0]};
                if (line_end) begin
                    horizontal <= 0;
                    if (vs_left != 0) vs_left <= vs_left - 1'b1;
                    if (frame_end) begin
                        row <= 0; scan <= 0; adjust <= 0; adjusting <= 0;
                        row_address <= start_address; frames <= frames + 1'b1;
                        if (registers[7][6:0] == 0) vs_left <= 16;
                    end else if (adjusting) adjust <= adjust + 1'b1;
                    else if (scan == registers[9][4:0]) begin
                        scan <= 0;
                        row_address <= row_address + {6'b0, registers[1]};
                        if (row == registers[4][6:0]) begin adjusting <= 1; adjust <= 0; end
                        else begin
                            row <= row + 1'b1;
                            if (row + 1'b1 == registers[7][6:0]) vs_left <= 16;
                        end
                    end else scan <= scan + 1'b1;
                end else horizontal <= horizontal + 1'b1;
            end
        end
    end
endmodule
