// SPDX-License-Identifier: GPL-3.0-or-later
// A CPU-written text plane for a running native C64. The 48x16 character
// cells use RGB palette colors and do not start an FPGA DDR master.
module fes_de25_c64_picker (
    input wire clk, reset_n,
    input wire [6:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output reg [31:0] readdata,
    output reg readdatavalid, writeresponsevalid,
    output wire [1:0] response,
    input wire [27:0] machine,
    output reg [27:0] video
);
    localparam [8:0] WORDS = 9'd192;
    localparam [6:0] STATUS = 7'h74, DATA = 7'h78, COMMAND = 7'h7c;
    // Pixel clock is also the Avalon slave clock. Two banks share a single
    // dual-port 32-bit RAM; an incomplete bank is never selected.
    (* ramstyle = "M20K" *) reg [31:0] cells [0:511];
    reg [8:0] count;
    reg active, writing, pending, hide, showing, fault;
    reg [15:0] published;
    reg [27:0] d1, d2;
    reg [10:0] h;
    reg [9:0] v;
    reg seen_vs;
    wire vs_rise = machine[26] && !d1[26];
    wire de_rise = machine[24] && !d1[24];
    wire [10:0] hx = vs_rise || de_rise || h == 11'd1649 ? 11'd0 : h + 11'd1;
    wire [9:0] vy = vs_rise ? 10'd725 : h == 11'd1649 ?
                    (v == 10'd749 ? 10'd0 : v + 10'd1) : v;
    wire [10:0] x = hx - 11'd256;
    wire [9:0] y = vy - 10'd224;
    wire inside_panel = hx >= 11'd256 && x < 11'd768 && vy >= 10'd224 && y < 10'd256;
    wire [7:0] row_words = {1'b0, y[7:4], 3'b000} + {2'b00, y[7:4], 2'b00};
    wire [7:0] cell_word = row_words + {3'b000, x[9:6]};
    reg [31:0] glyphs;
    reg [1:0] lane;
    reg [2:0] bit_x, bit_y;
    reg plane;
    wire [7:0] character = glyphs[{lane, 3'b000} +: 8];
    wire store = write && !read && byteenable == 4'hf &&
                 address == DATA && writing && !pending && !fault && count < WORDS;
    assign response = 2'b00;

    // Printable ASCII from the public-domain font8x8 basic glyphs used by
    // FogCast's debug renderer. Bit zero is the leftmost pixel.
    function automatic [63:0] glyph(input [6:0] ch);
        case (ch)
            7'd32: glyph = 64'h0000000000000000;
            7'd33: glyph = 64'h00180018183C3C18;
            7'd34: glyph = 64'h0000000000243636;
            7'd35: glyph = 64'h0036367F367F3636;
            7'd36: glyph = 64'h000C1F301E033E0C;
            7'd37: glyph = 64'h0063660C18336300;
            7'd38: glyph = 64'h006E333B6E1C361C;
            7'd39: glyph = 64'h0000000000030606;
            7'd40: glyph = 64'h00180C0606060C18;
            7'd41: glyph = 64'h00060C1818180C06;
            7'd42: glyph = 64'h0000663CFF3C6600;
            7'd43: glyph = 64'h00000C0C3F0C0C00;
            7'd44: glyph = 64'h060C0C0000000000;
            7'd45: glyph = 64'h000000003F000000;
            7'd46: glyph = 64'h000C0C0000000000;
            7'd47: glyph = 64'h000103060C183060;
            7'd48: glyph = 64'h003E676F7B73633E;
            7'd49: glyph = 64'h003F0C0C0C0C0E0C;
            7'd50: glyph = 64'h003F33061C30331E;
            7'd51: glyph = 64'h001E33301C30331E;
            7'd52: glyph = 64'h0078307F33363C38;
            7'd53: glyph = 64'h001E3330301F033F;
            7'd54: glyph = 64'h001E33331F03061C;
            7'd55: glyph = 64'h000C0C0C1830333F;
            7'd56: glyph = 64'h001E33331E33331E;
            7'd57: glyph = 64'h000E18303E33331E;
            7'd58: glyph = 64'h000C0C00000C0C00;
            7'd59: glyph = 64'h060C0C00000C0C00;
            7'd60: glyph = 64'h00180C0603060C18;
            7'd61: glyph = 64'h00003F00003F0000;
            7'd62: glyph = 64'h00060C1830180C06;
            7'd63: glyph = 64'h000C000C1830331E;
            7'd64: glyph = 64'h001E037B7B7B633E;
            7'd65: glyph = 64'h0033333F33331E0C;
            7'd66: glyph = 64'h003F66663E66663F;
            7'd67: glyph = 64'h003C66030303663C;
            7'd68: glyph = 64'h001F36666666361F;
            7'd69: glyph = 64'h007F46161E16467F;
            7'd70: glyph = 64'h000F06161E16467F;
            7'd71: glyph = 64'h007C66730303663C;
            7'd72: glyph = 64'h003333333F333333;
            7'd73: glyph = 64'h001E0C0C0C0C0C1E;
            7'd74: glyph = 64'h001E333330303078;
            7'd75: glyph = 64'h006766361E366667;
            7'd76: glyph = 64'h007F66460606060F;
            7'd77: glyph = 64'h006363636B7F7763;
            7'd78: glyph = 64'h006363737B6F6763;
            7'd79: glyph = 64'h001C36636363361C;
            7'd80: glyph = 64'h000F06063E66663F;
            7'd81: glyph = 64'h00381E3B3333331E;
            7'd82: glyph = 64'h006766363E66663F;
            7'd83: glyph = 64'h001E33380E07331E;
            7'd84: glyph = 64'h001E0C0C0C0C2D3F;
            7'd85: glyph = 64'h003F333333333333;
            7'd86: glyph = 64'h000C1E3333333333;
            7'd87: glyph = 64'h0063777F6B636363;
            7'd88: glyph = 64'h0063361C1C366363;
            7'd89: glyph = 64'h001E0C0C1E333333;
            7'd90: glyph = 64'h007F664C1831637F;
            7'd91: glyph = 64'h001E06060606061E;
            7'd92: glyph = 64'h00406030180C0603;
            7'd93: glyph = 64'h001E18181818181E;
            7'd94: glyph = 64'h0000000063361C08;
            7'd95: glyph = 64'hFF00000000000000;
            7'd96: glyph = 64'h0000000000180C0C;
            7'd97: glyph = 64'h006E333E301E0000;
            7'd98: glyph = 64'h003B66663E060607;
            7'd99: glyph = 64'h001E3303331E0000;
            7'd100: glyph = 64'h006E33333e303038;
            7'd101: glyph = 64'h001E033f331E0000;
            7'd102: glyph = 64'h000F06060f06361C;
            7'd103: glyph = 64'h1F303E33336E0000;
            7'd104: glyph = 64'h006766666E360607;
            7'd105: glyph = 64'h001E0C0C0C0E000C;
            7'd106: glyph = 64'h1E33333030300030;
            7'd107: glyph = 64'h0067361E36660607;
            7'd108: glyph = 64'h001E0C0C0C0C0C0E;
            7'd109: glyph = 64'h00636B7F7F330000;
            7'd110: glyph = 64'h00333333331F0000;
            7'd111: glyph = 64'h001E3333331E0000;
            7'd112: glyph = 64'h0F063E66663B0000;
            7'd113: glyph = 64'h78303E33336E0000;
            7'd114: glyph = 64'h000F06666E3B0000;
            7'd115: glyph = 64'h001F301E033E0000;
            7'd116: glyph = 64'h00182C0C0C3E0C08;
            7'd117: glyph = 64'h006E333333330000;
            7'd118: glyph = 64'h000C1E3333330000;
            7'd119: glyph = 64'h00367F7F6B630000;
            7'd120: glyph = 64'h0063361C36630000;
            7'd121: glyph = 64'h1F303E3333330000;
            7'd122: glyph = 64'h003F260C193F0000;
            7'd123: glyph = 64'h00380C0C070C0C38;
            7'd124: glyph = 64'h0018181800181818;
            7'd125: glyph = 64'h00070C0C380C0C07;
            7'd126: glyph = 64'h0000000000003B6E;

            default: glyph = 64'h007e424242427e00;
        endcase
    endfunction
    wire [63:0] bitmap = glyph(character[6:0]);
    wire ink = bitmap[{bit_y, bit_x}];
    always @(posedge clk) begin
        if (store) cells[{!active, count[7:0]}] <= writedata;
        glyphs <= cells[{active, cell_word}];
        lane <= x[5:4];
        bit_x <= x[3:1];
        bit_y <= y[3:1];
        plane <= seen_vs && showing && inside_panel && machine[24];
        d1 <= machine;
        h <= hx;
        v <= vy;
        d2 <= {d1[27:24], plane ? (ink ? 24'hffffff :
                                     character[7] ? 24'h3c5991 : 24'h151f32) : d1[23:0]};
        video <= d2;
        readdatavalid <= read && !write;
        writeresponsevalid <= write && !read;
        if (read) begin
            case (address)
                STATUS: readdata <= 32'h43504931; // CPI1
                COMMAND: readdata <= {published, 13'd0, fault, pending, showing};
                default: readdata <= 0;
            endcase
        end
        if (!reset_n) begin
            h <= 11'd1649; v <= 10'd749; seen_vs <= 0;
            d1 <= 0; d2 <= 0; video <= 0; plane <= 0;
            count <= 0; active <= 0; writing <= 0; pending <= 0; hide <= 0;
            showing <= 0; fault <= 0; published <= 0;
            readdatavalid <= 0; writeresponsevalid <= 0; readdata <= 0;
        end else begin
            if (vs_rise) begin
                seen_vs <= 1;
                if (pending) begin
                    if (!hide) active <= !active;
                    showing <= !hide;
                    pending <= 0;
                    published <= published + 1'b1;
                end
            end
            if (write && !read && !fault) begin
                if (store) count <= count + 1'b1;
                else if (address == COMMAND && byteenable == 4'hf && !pending) begin
                    case (writedata)
                        32'd1: if (!writing) begin writing <= 1; count <= 0; end else fault <= 1;
                        32'd2: if (writing && count == WORDS) begin
                            writing <= 0; pending <= 1; hide <= 0;
                        end else fault <= 1;
                        32'd3: if (!writing) begin pending <= 1; hide <= 1; end else fault <= 1;
                        default: fault <= 1;
                    endcase
                end else fault <= 1;
            end
        end
    end
endmodule
