// SPDX-License-Identifier: GPL-2.0-or-later
// Text/bitmap slice of the VIC-II on fixed 720p60 HDMI. The 320x200 picture is
// scaled by 4 horizontally and 3 vertically and centred in 1280x720. Sprites and
// badlines are not implemented. The raster counter advances
// every 63 CPU enables over 312 PAL lines and is not locked to the HDMI line.

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
    output reg  [8:0]  raster,
    output wire        irq
);
    reg [7:0] regs [0:46];
    reg [5:0] phi_div;
    reg raster_pending, raster_fired;
    wire [8:0] raster_compare = {regs[6'h11][7], regs[6'h12]};
    wire line_end = phi && phi_div == 6'd62;
    wire [8:0] next_raster = raster == 9'd311 ? 9'd0 : raster + 9'd1;
    wire compare_write = cs && we && (addr == 6'h11 || addr == 6'h12);
    wire [8:0] written_compare = addr == 6'h11 ? {din[7], regs[6'h12]} :
                                                              {regs[6'h11][7], din};
    // One event per line. Line zero's automatic comparison is one Phi2 later.
    wire raster_event = (line_end && next_raster != 0 && next_raster == raster_compare) ||
        (!raster_fired && ((phi && raster == 0 && phi_div == 0 && raster_compare == 0) ||
                          (compare_write && written_compare == raster)));
    assign irq = raster_pending && regs[6'h1A][0];
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
        else if (addr == 6'h19) dout = {irq, 3'b111, 3'b000, raster_pending};
        else if (addr == 6'h1A) dout = {4'b1111, regs[6'h1A][3:0]};
        else if (addr <= 6'h2E) dout = regs[addr];
        color_dout = {4'h0, color_cpu_q};
    end

    always @(posedge clk_sys) begin
        if (reset) begin
            raster <= 9'd0;
            phi_div <= 6'd0;
            raster_pending <= 1'b0;
            raster_fired <= 1'b0;
            for (i = 0; i < 47; i = i + 1) regs[i] <= 8'h00;
            regs[6'h11] <= 8'h1B;
            regs[6'h16] <= 8'hC8;
            regs[6'h18] <= 8'h14;
            regs[6'h20] <= 8'h0E;
            regs[6'h21] <= 8'h06;
        end else begin
            if (we && cs && addr <= 6'h2E && addr != 6'h19)
                regs[addr] <= din;
            if (line_end) raster_fired <= 1'b0;
            if (we && cs && addr == 6'h19 && din[0]) raster_pending <= 1'b0;
            if (raster_event) begin
                raster_pending <= 1'b1;
                raster_fired <= 1'b1;
            end
            if (phi) begin
                if (phi_div == 6'd62) begin
                    phi_div <= 6'd0;
                    raster <= raster == 9'd311 ? 9'd0 : raster + 9'd1;
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
    reg [23:0] border_q;

    wire h_active = hpos >= 11'd260 && hpos < 11'd1540;
    wire v_active = vpos >= 10'd25 && vpos < 10'd745;
    wire [10:0] ax = hpos - 11'd260;
    wire [9:0] ay = vpos - 10'd25;
    wire [5:0] ccol = ax[10:5];
    wire [9:0] text_cell = {crow, 5'b0} + {2'b0, crow, 3'b0} + {4'b0, ccol};
    // crow*32 + crow*8 + column = crow*40 + column.
    wire [13:0] screen_off = {d018[7:4], 10'b0} + {4'b0, text_cell};
    wire [15:0] text_addr = {bank, d018[3:1], code_q, grow};
    wire [15:0] bitmap_addr = {bank, d018[3], text_cell, grow};
    wire [15:0] glyph_addr = d011[5] ? bitmap_addr : text_addr;
    // One registered RAM port suffices: fetch matrix on phase 0, latch code
    // on phase 1, fetch glyph on phase 2, latch glyph on phase 3. A cell spans
    // 32 HDMI clocks. Delay video coordinates by four clocks to match data.
    assign ram_addr = ax[4:0] == 5'd2 ? glyph_addr : {bank, screen_off};
    assign de = out_h_active && out_v_active;
    assign hsync = hdelay[3] < 11'd40;
    assign vsync = vdelay[3] < 10'd5;

    c64_color_ram color_ram (
        .clk_a(clk_sys), .addr_a(color_addr), .wdata_a(color_din[3:0]),
        .we_a(color_we && color_cs), .q_a(color_cpu_q),
        .clk_b(pixel_clk),
        .addr_b(text_cell < 10'd1000 ? text_cell : 10'd0), .q_b(color_video_q)
    );

    reg [7:0] glyph_q, matrix_q;
    reg [3:0] color_fetch_q;
    reg [10:0] hdelay [0:3];
    reg [9:0] vdelay [0:3];
    reg tdelay [0:3];
    wire out_h_active = hdelay[3] >= 11'd260 && hdelay[3] < 11'd1540;
    wire out_v_active = vdelay[3] >= 10'd25 && vdelay[3] < 10'd745;
    wire [10:0] out_ax = hdelay[3] - 11'd260;
    wire out_text = tdelay[3] && vdelay[3] >= 10'd85 && vdelay[3] < 10'd685;
    wire hires_on = glyph_q[~out_ax[4:2]];
    wire [1:0] pair = (glyph_q >> ((3-out_ax[4:3])*2)) & 2'b11;
    wire bitmap_mode = d011[5];
    wire multicolor_mode = regs[6'h16][4] && (bitmap_mode || color_q[3]);
    reg [3:0] ink;
    always @* begin
        if (multicolor_mode) begin
            case (pair)
                2'd0: ink=back;
                2'd1: ink=bitmap_mode?matrix_q[7:4]:regs[6'h22][3:0];
                2'd2: ink=bitmap_mode?matrix_q[3:0]:regs[6'h23][3:0];
                default: ink=bitmap_mode?color_q:{1'b0,color_q[2:0]};
            endcase
        end else if (bitmap_mode) ink=hires_on?matrix_q[7:4]:matrix_q[3:0];
        else ink=hires_on?color_q:back;
    end
    wire [23:0] pixel = !de ? 24'h000000 :
        (out_text && d011[4]) ? palette(ink) : border_q;
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

        hdelay[0] <= hpos; vdelay[0] <= vpos; tdelay[0] <= in_text;
        for (integer p=1; p<4; p=p+1) begin
            hdelay[p] <= hdelay[p-1]; vdelay[p] <= vdelay[p-1]; tdelay[p] <= tdelay[p-1];
        end
        if (ax[4:0] == 5'd1) begin
            code_q <= ram_data;
            color_fetch_q <= color_video_q;
        end
        if (ax[4:0] == 5'd3) begin glyph_q <= ram_data; color_q <= color_fetch_q; matrix_q <= code_q; end
        border_q <= palette(border);
    end

    initial begin
        for (integer p=0; p<4; p=p+1) begin
            hdelay[p]=0; vdelay[p]=0; tdelay[p]=0;
        end
        glyph_q=0;
        hpos = 0;
        vpos = 0;
        in_text = 0;
    end
endmodule
