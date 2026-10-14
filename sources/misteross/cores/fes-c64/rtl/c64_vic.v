// SPDX-License-Identifier: GPL-2.0-or-later
// PAL text/bitmap sequencer with native matrix/glyph DMA and CPU BA.
// Native colors pass through complete-frame buffers into fixed 720p60 HDMI.
// Sprite DMA, collision/lightpen and exact mode-transition quirks remain open.

module c64_vic (
    input  wire        clk_sys,
    input  wire        phi,
    input  wire [5:0] cycle_clock,
    input  wire [7:0] cpu_bus_data,
    output wire ba_n,
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

    wire [5:0] native_cycle = phi_div + 6'd1;
    wire first_phase = cycle_clock == 0;
    wire second_phase = cycle_clock == 26;
    wire fetch_sample = cycle_clock == 4 || cycle_clock == 30;
    wire request, matrix_request, native_valid;
    wire [9:0] native_color_address;
    wire [7:0] native_glyph, native_matrix;
    wire [3:0] native_color;
    c64_vic_fetch fetch (
        .clk(clk_sys),.reset(reset),.first_phase(first_phase),.second_phase(second_phase),
        .sample(fetch_sample),.raster(raster),.cycle(native_cycle),.d011(d011),.d018(d018),
        .bank(bank),.cpu_data(cpu_bus_data),.ram_data(ram_data),.color_data(color_video_q),
        .badline(),.ba_n(ba_n),.aec_n(),.request(request),.matrix_request(matrix_request),
        .address(ram_addr),.color_address(native_color_address),.graphics_valid(native_valid),
        .graphics_data(native_glyph),.graphics_matrix(native_matrix),.graphics_color(native_color),
        .vc(),.vcbase(),.rc(),.display()
    );
    c64_color_ram color_ram (
        .clk_a(clk_sys),.addr_a(color_addr),.wdata_a(color_din[3:0]),.we_a(color_we),.q_a(color_cpu_q),
        .clk_b(clk_sys),.addr_b(native_color_address),.q_b(color_video_q)
    );
    reg [7:0] next_glyph, next_matrix, glyph, matrix, previous_glyph, previous_matrix;
    reg [3:0] next_color, cell_color, previous_color;
    reg frame_started, vertical_border, set_vertical_border, main_border;
    reg [2:0] subpixel;
    reg pixel_strobe;
    always @* begin
        pixel_strobe=1; subpixel=0;
        case(cycle_clock)
            1:subpixel=0; 8:subpixel=1; 14:subpixel=2; 21:subpixel=3;
            27:subpixel=4; 34:subpixel=5; 40:subpixel=6; 47:subpixel=7;
            default:pixel_strobe=0;
        endcase
    end
    wire native_frame_start = first_phase && raster==0 && native_cycle==1;
    wire lower_edge = raster==(d011[3]?9'd251:9'd247);
    wire upper_edge = raster==(d011[3]?9'd51:9'd55) && d011[4];
    wire left_edge = first_phase && native_cycle==(regs[6'h16][3]?6'd17:6'd18);
    wire right_edge = first_phase && native_cycle==(regs[6'h16][3]?6'd57:6'd56);
    always @(posedge clk_sys) begin
        if(reset)begin
            frame_started<=0; vertical_border<=1;set_vertical_border<=1;main_border<=1;
            next_glyph<=0;next_matrix<=0;next_color<=0;glyph<=0;matrix<=0;cell_color<=0;
            previous_glyph<=0;previous_matrix<=0;previous_color<=0;
        end else begin
            if(native_frame_start)frame_started<=1;
            if(upper_edge)begin vertical_border<=0;set_vertical_border<=0;end
            if(lower_edge)set_vertical_border<=1;
            if(first_phase && native_cycle==1)vertical_border<=set_vertical_border;
            if(left_edge)begin
                vertical_border<=lower_edge || set_vertical_border;
                if(!lower_edge && !set_vertical_border)main_border<=0;
            end
            if(right_edge)main_border<=1;
            if(native_valid)begin next_glyph<=native_glyph;next_matrix<=native_matrix;next_color<=native_color;end
            if(first_phase)begin
                previous_glyph<=glyph;previous_matrix<=matrix;previous_color<=cell_color;
                glyph<=next_glyph;matrix<=next_matrix;cell_color<=next_color;
            end
        end
    end
    wire [2:0] xscroll=regs[6'h16][2:0];
    wire from_previous=subpixel<xscroll;
    wire [2:0] bit_number=subpixel-xscroll;
    wire [7:0] pixel_glyph=from_previous?previous_glyph:glyph;
    wire [7:0] pixel_matrix=from_previous?previous_matrix:matrix;
    wire [3:0] pixel_color=from_previous?previous_color:cell_color;
    wire on_pixel=pixel_glyph[~bit_number];
    wire [1:0] pair=(pixel_glyph >> ((3-bit_number[2:1])*2))&2'b11;
    wire bitmap=d011[5],ecm=d011[6],mcm=regs[6'h16][4];
    wire multicolor=mcm&&(bitmap||pixel_color[3]);
    reg [3:0] ink;
    always @* begin
        if(ecm&&(bitmap||mcm))ink=0;
        else if(multicolor)begin
            case(pair)
                0:ink=back;
                1:ink=bitmap?pixel_matrix[7:4]:regs[6'h22][3:0];
                2:ink=bitmap?pixel_matrix[3:0]:regs[6'h23][3:0];
                3:ink=bitmap?pixel_color:{1'b0,pixel_color[2:0]};
            endcase
        end else if(bitmap)ink=on_pixel?pixel_matrix[7:4]:pixel_matrix[3:0];
        else ink=on_pixel?pixel_color:ecm?regs[6'h21+{4'd0,pixel_matrix[7:6]}][3:0]:back;
        if(from_previous && native_cycle==17)ink=back;
        if(main_border || vertical_border)ink=border;
    end
    wire visible_native=raster>=51&&raster<=250&&native_cycle>=17&&native_cycle<=56;
    wire [7:0] native_y=8'(raster-9'd51);
    // Convert the native raster window into a 320x200 indexed-color frame.
    wire [8:0] write_x=9'((native_cycle-6'd17)*8)+{6'd0,subpixel};
    wire [15:0] write_address=16'(native_y*320)+{7'd0,write_x};

    reg [10:0] hpos=0,hpos_q=0;
    reg [9:0] vpos=0,vpos_q=0;
    reg [1:0] yscale=0;
    reg [7:0] display_y=0;
    wire [10:0] ax=hpos-11'd260;
    wire [15:0] read_address=16'(display_y*320)+{7'd0,ax[10:2]};
    wire [3:0] frame_color;
    c64_frame_store frame_store (
        .clk_sys(clk_sys),.pixel_clk(pixel_clk),.reset(reset),
        .write_enable(!reset&&visible_native&&pixel_strobe),.write_address(write_address),.write_data(ink),
        .frame_done(native_frame_start&&frame_started),.frame_start(hpos==0&&vpos==0),
        .read_address(read_address),.read_data(frame_color),.write_page(),.read_page(),.pending()
    );
    always @(posedge pixel_clk)begin
        hpos_q<=hpos;vpos_q<=vpos;
        if(hpos==1649)begin hpos<=0;vpos<=vpos==749?0:vpos+1'b1;end
        else hpos<=hpos+1'b1;
        if(hpos==0)begin
            if(vpos==85)begin yscale<=0;display_y<=0;end
            else if(vpos>85&&vpos<=685)begin
                if(yscale==2)begin yscale<=0;display_y<=display_y+1'b1;end
                else yscale<=yscale+1'b1;
            end
        end
    end
    assign de=hpos_q>=260&&hpos_q<1540&&vpos_q>=25&&vpos_q<745;
    assign hsync=hpos_q<40;
    assign vsync=vpos_q<5;
    wire text_window=vpos_q>=85&&vpos_q<685;
    wire [23:0] rgb=!de?24'd0:palette(text_window?frame_color:border);
    assign red=rgb[23:16];assign green=rgb[15:8];assign blue=rgb[7:0];
endmodule
