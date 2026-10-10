// SPDX-License-Identifier: GPL-2.0-or-later
// Text-mode slice of the VIC-II on fixed 720p60 HDMI. The 320x200 picture is
// scaled by 4 horizontally and 3 vertically and centred in 1280x720. Sprites,
// bitmap modes and badlines are not implemented. The raster counter advances
// every 63 CPU enables and is not locked to the HDMI line.
`include "c64_font.vh"

module c64_vic (
    input  wire        clk_sys,
    input  wire        phi,
    input  wire        pixel_clk,
    input  wire        reset,
    input  wire        cs,
    input  wire        we,
    input  wire [5:0]  addr,
    input  wire [7:0]  din,
    output reg  [7:0]  dout,
    input  wire        color_cs,
    input  wire [9:0]  color_addr,
    input  wire [7:0]  color_din,
    input  wire        color_we,
    output reg  [7:0]  color_dout,
    input  wire [1:0]  bank,
    output wire [15:0] ram_addr,
    input  wire [7:0]  ram_data,
    output wire [7:0]  red,
    output wire [7:0]  green,
    output wire [7:0]  blue,
    output wire        de,
    output wire        hsync,
    output wire        vsync,
    output reg  [8:0]  raster
);
    reg [7:0] regs [0:46];
    reg [5:0] phi_div;
    integer i;

    wire [3:0] color_cpu_q;
    wire [3:0] color_video_q;

    wire [7:0] d011 = regs[6'h11];
    wire [7:0] d018 = regs[6'h18];
    wire [3:0] border = regs[6'h20][3:0];
    wire [3:0] back = regs[6'h21][3:0];

    always @* begin
        dout = 8'hFF;
        if (addr == 6'h11) dout = {raster[8], d011[6:0]};
        else if (addr == 6'h12) dout = raster[7:0];
        else if (addr <= 6'h2E) dout = regs[addr];
        color_dout = {4'h0, color_cpu_q};
    end

    always @(posedge clk_sys) begin
        if (reset) begin
            raster <= 9'd0;
            phi_div <= 6'd0;
            for (i = 0; i < 47; i = i + 1) regs[i] <= 8'h00;
            regs[6'h11] <= 8'h1B;
            regs[6'h16] <= 8'hC8;
            regs[6'h18] <= 8'h14;
            regs[6'h20] <= 8'h0E;
            regs[6'h21] <= 8'h06;
        end else begin
            if (we && cs && addr <= 6'h2E && addr != 6'h12)
                regs[addr] <= din;
            if (phi) begin
                if (phi_div == 6'd62) begin
                    phi_div <= 6'd0;
                    raster <= raster == 9'd262 ? 9'd0 : raster + 9'd1;
                end else
                    phi_div <= phi_div + 6'd1;
            end
        end
    end

    function automatic [23:0] palette;
        input [3:0] index;
        begin
            case (index)
                4'h0: palette = 24'h000000;
                4'h1: palette = 24'hFFFFFF;
                4'h2: palette = 24'hCC0000;
                4'h3: palette = 24'h00FFFF;
                4'h4: palette = 24'hCC44CC;
                4'h5: palette = 24'h00CC00;
                4'h6: palette = 24'h0000CC;
                4'h7: palette = 24'hEEEE77;
                4'h8: palette = 24'hDD8855;
                4'h9: palette = 24'h664400;
                4'hA: palette = 24'hFF7777;
                4'hB: palette = 24'h333333;
                4'hC: palette = 24'h777777;
                4'hD: palette = 24'hAAFF66;
                4'hE: palette = 24'h0088FF;
                default: palette = 24'hBBBBBB;
            endcase
        end
    endfunction

    reg [10:0] hpos;
    reg [9:0] vpos;
    reg [1:0] yscale;
    reg [2:0] grow;
    reg [4:0] crow;
    reg in_text;
    reg [7:0] code_q;
    reg [3:0] color_q;
    reg text_q, den_q;
    reg [2:0] sub_q, grow_q;
    reg [23:0] border_q, back_q;

    wire h_active = hpos >= 11'd260 && hpos < 11'd1540;
    wire v_active = vpos >= 10'd25 && vpos < 10'd745;
    wire [10:0] ax = hpos - 11'd260;
    wire [9:0] ay = vpos - 10'd25;
    wire text_line = in_text && ay >= 10'd60 && ay < 10'd660;
    wire [5:0] ccol = ax[10:5];
    wire [9:0] text_cell = {crow, 5'b0} + {2'b0, crow, 3'b0} + {4'b0, ccol};
    // crow*32 + crow*8 + column = crow*40 + column.
    wire [13:0] screen_off = {d018[7:4], 10'b0} + {4'b0, text_cell};
    assign ram_addr = {bank, screen_off};
    assign de = h_active && v_active;
    assign hsync = hpos < 11'd40;
    assign vsync = vpos < 10'd5;

    c64_color_ram color_ram (
        .clk_a(clk_sys), .addr_a(color_addr), .wdata_a(color_din[3:0]),
        .we_a(color_we && color_cs), .q_a(color_cpu_q),
        .clk_b(pixel_clk),
        .addr_b(text_cell < 10'd1000 ? text_cell : 10'd0), .q_b(color_video_q)
    );

    wire [7:0] bits = c64_glyph(code_q, grow_q);
    wire pixel_on = den_q && text_q && bits[~sub_q];
    wire [23:0] pixel = !de ? 24'h000000 :
                        pixel_on ? palette(color_q) :
                        (text_q && den_q) ? back_q : border_q;
    assign red = pixel[23:16];
    assign green = pixel[15:8];
    assign blue = pixel[7:0];

    always @(posedge pixel_clk) begin
        if (hpos == 11'd1649) begin
            hpos <= 11'd0;
            if (vpos == 10'd749) vpos <= 10'd0;
            else vpos <= vpos + 10'd1;
        end else
            hpos <= hpos + 11'd1;

        if (hpos == 11'd0) begin
            if (vpos == 10'd85) begin
                in_text <= 1'b1;
                yscale <= 2'd0;
                grow <= 3'd0;
                crow <= 5'd0;
            end else if (in_text) begin
                if (yscale == 2'd2) begin
                    yscale <= 2'd0;
                    if (grow == 3'd7) begin
                        grow <= 3'd0;
                        if (crow == 5'd24) in_text <= 1'b0;
                        else crow <= crow + 5'd1;
                    end else
                        grow <= grow + 3'd1;
                end else
                    yscale <= yscale + 2'd1;
            end
        end

        code_q <= ram_data;
        color_q <= color_video_q;
        text_q <= text_line && h_active;
        den_q <= d011[4];
        sub_q <= ax[4:2];
        grow_q <= grow;
        border_q <= palette(border);
        back_q <= palette(back);
    end

    initial begin
        hpos = 0;
        vpos = 0;
        in_text = 0;
    end
endmodule
