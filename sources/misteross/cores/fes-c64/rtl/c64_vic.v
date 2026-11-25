// SPDX-License-Identifier: GPL-2.0-or-later
// VIC-II text/bitmap sequencer with native matrix/glyph DMA and CPU BA.
// Native colors pass through complete-frame buffers into fixed 720p60 HDMI.
// Light-pen capture and exact mode-transition/subpixel quirks remain open.

`include "c64_timing.vh"

module c64_vic #(parameter integer NTSC = 0) (
    input  wire        clk_sys,
    input  wire        phi,
    input  wire [5:0] cycle_clock,
    input  wire [7:0] cpu_bus_data,
    output wire ba_n,
    input  wire        pixel_clk,
    input  wire        reset,
    input  wire        cs,
    input  wire        we,
    input wire reading, read_sample,
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
    localparam CYCLE_BITS = `C64_CYCLE_BITS(NTSC);
    localparam [8:0] LAST_LINE = `C64_LINES_PER_FRAME(NTSC) - 1;
    reg [CYCLE_BITS-1:0] phi_div;
    reg raster_fired;
    wire [8:0] raster_compare = {regs[6'h11][7], regs[6'h12]};
    wire line_end = phi && phi_div == `C64_CYCLES_PER_LINE(NTSC) - 1;
    wire [8:0] next_raster = raster == LAST_LINE ? 9'd0 : raster + 9'd1;
    // The R8 guest raster counter still reads 262 on the first cycle after
    // the beam wraps. VICE x64sc's vicii_cycle_start_of_frame clears it on
    // cycle 2. The dot/capture coordinates continue from cycle 1 as usual.
    reg hold_last_line;
    wire [8:0] guest_raster = hold_last_line ? LAST_LINE : raster;
    wire guest_line_end = (line_end && (!NTSC || next_raster != 0)) ||
        (phi && hold_last_line);
    wire compare_write = cs && we && (addr == 6'h11 || addr == 6'h12);
    wire [8:0] written_compare = addr == 6'h11 ? {din[7], regs[6'h12]} :
                                                              {regs[6'h11][7], din};
    // One event per line. Line zero's automatic comparison is one Phi2 later.
    wire line_zero_match = phi && raster == 0 && phi_div == 0 && raster_compare == 0;
    wire raster_event = (line_end && next_raster != 0 && next_raster == raster_compare) ||
        (line_zero_match && (!raster_fired || hold_last_line)) ||
        (!raster_fired && compare_write && written_compare == guest_raster);

    integer i;

    wire [3:0] color_cpu_q;
    wire [3:0] color_video_q;

    wire [7:0] d011 = regs[6'h11];
    wire [7:0] d018 = regs[6'h18];
    wire [3:0] border = regs[6'h20][3:0];

    always @* begin
        dout = 8'hFF;
        if (addr == 6'h11) dout = {guest_raster[8], d011[6:0]};
        else if (addr == 6'h12) dout = guest_raster[7:0];
        else if (addr == 6'h19) dout = collision_status;
        else if (addr == 6'h1e) dout = ss_mask;
        else if (addr == 6'h1f) dout = bg_mask;
        else if (addr == 6'h1A) dout = {4'b1111, regs[6'h1A][3:0]};
        else if (addr == 6'h16) dout = regs[addr] | 8'hc0;
        else if (addr == 6'h18) dout = regs[addr] | 8'h01;
        else if (addr >= 6'h20 && addr <= 6'h2e) dout = regs[addr] | 8'hf0;
        else if (addr <= 6'h2E) dout = regs[addr];
        color_dout = {4'h0, color_cpu_q};
    end

    always @(posedge clk_sys) begin
        if (reset) begin
            raster <= 9'd0;
            hold_last_line <= 1'b0;
            phi_div <= 0;
            raster_fired <= 1'b0;
            for (i = 0; i < 47; i = i + 1) regs[i] <= 8'h00;
            regs[6'h11] <= 8'h1B;
            regs[6'h16] <= 8'hC8;
            regs[6'h18] <= 8'h14;
            regs[6'h20] <= 8'h0E;
            regs[6'h21] <= 8'h06;
        end else begin
            if (we && cs && addr <= 6'h2E && addr != 6'h19 && addr != 6'h13 && addr != 6'h14 && addr != 6'h1e && addr != 6'h1f)
                regs[addr] <= din;
            if (phi) hold_last_line <= NTSC && line_end && next_raster == 0;
            if (guest_line_end) raster_fired <= 1'b0;
            if (raster_event) begin
                raster_fired <= 1'b1;
            end
            if (line_end) begin
                phi_div <= 0;
                raster <= next_raster;
            end else if (phi)
                phi_div <= phi_div + 1'b1;
        end
    end

    // Colodore PAL, common VIC-II luma revision, brightness/contrast/color 50/100/50.
    // Philip Timmermann, Calculating the color palette of the VIC II (Feb 2017):
    // https://www.pepto.de/projects/colorvic/ . PAL 2.8 -> display 2.2 gamma.
    // NTSC uses VICE data/C64/pepto-ntsc-sony.vpl (Sony CXA2025AS US decoder).
    // Fixed RGB reference only; composite chroma/scanline effects are not applied.
    function automatic [23:0] palette;
        input [3:0] index;
        begin
            case (index)
                4'h0: palette = 24'h000000;
                4'h1: palette = 24'hFFFFFF;
                4'h2: palette = NTSC ? 24'h7C352B : 24'h813338;
                4'h3: palette = NTSC ? 24'h5AA6B1 : 24'h75CEC8;
                4'h4: palette = NTSC ? 24'h694185 : 24'h8E3C97;
                4'h5: palette = NTSC ? 24'h5D8643 : 24'h56AC4D;
                4'h6: palette = NTSC ? 24'h212E78 : 24'h2E2C9B;
                4'h7: palette = NTSC ? 24'hCFBE6F : 24'hEDF171;
                4'h8: palette = NTSC ? 24'h894A26 : 24'h8E5029;
                4'h9: palette = NTSC ? 24'h5B3300 : 24'h553800;
                4'hA: palette = NTSC ? 24'hAF6459 : 24'hC46C71;
                4'hB: palette = NTSC ? 24'h434343 : 24'h4A4A4A;
                4'hC: palette = NTSC ? 24'h6B6B6B : 24'h7B7B7B;
                4'hD: palette = NTSC ? 24'hA0CB84 : 24'hA9FF9F;
                4'hE: palette = NTSC ? 24'h5665B3 : 24'h706DEB;
                default: palette = NTSC ? 24'h959595 : 24'hB2B2B2;
            endcase
        end
    endfunction

    wire [CYCLE_BITS-1:0] native_cycle = phi_div + 1'b1;
    wire first_phase = cycle_clock == 0;
    wire second_phase = cycle_clock == 26;
    wire fetch_sample = cycle_clock == 4 || cycle_clock == 30;
    wire request, matrix_request, native_valid, matrix_ba_n, sprite_request;
    wire [15:0] graphics_address, sprite_address;
    wire [63:0] sprite_y;
    wire [71:0] sprite_x;wire [31:0] sprite_colors;
    wire [191:0] sprite_data;wire [7:0] sprite_display;
    wire [3:0] sprite_ink;
    for(genvar n=0;n<8;n=n+1)begin : sprite_positions
        assign sprite_y[n*8+:8]=regs[n*2+1];
        assign sprite_x[n*9+:9]={regs[6'h10][n],regs[n*2]};
        assign sprite_colors[n*4+:4]=regs[6'h27+n][3:0];
    end
    assign ram_addr=sprite_request?sprite_address:graphics_address;
    wire [9:0] native_color_address;
    wire [7:0] native_glyph, native_matrix;
    wire [3:0] native_color;
    c64_vic_fetch #(.NTSC(NTSC)) fetch (
        .clk(clk_sys),.reset(reset),.first_phase(first_phase),.second_phase(second_phase),
        .sample(fetch_sample),.raster(raster),.cycle(native_cycle),.d011(d011),.d018(d018),
        .bank(bank),.cpu_data(cpu_bus_data),.ram_data(ram_data),.color_data(color_video_q),
        .badline(),.ba_n(matrix_ba_n),.aec_n(),.request(request),.matrix_request(matrix_request),
        .address(graphics_address),.color_address(native_color_address),.graphics_valid(native_valid),
        .graphics_data(native_glyph),.graphics_matrix(native_matrix),.graphics_color(native_color),
        .vc(),.vcbase(),.rc(),.display()
    );
    c64_vic_sprite_fetch #(.NTSC(NTSC)) sprites (
        .clk(clk_sys),.reset(reset),.phi1(first_phase),.phi2(second_phase),
        .cycle(native_cycle),.raster(raster),.enabled(regs[6'h15]),.expanded(regs[6'h17]),
        .ypos(sprite_y),.expansion_write(cs&&we&&addr==6'h17),.expansion_value(din),
        .other_ba_n(matrix_ba_n),.sample(fetch_sample),.ram_data(ram_data),
        .cpu_data(cpu_bus_data),.d018(d018),.bank(bank),.request(sprite_request),
        .address(sprite_address),.pointers(),.sprite_data(sprite_data),.dma(),.display(sprite_display),.exp_flop(),
        .counters(),.bases(),.ba_n(ba_n),.aec_n()
    );
`ifdef FES_SIMULATION
    always @(posedge clk_sys)
        if(!reset&&request&&sprite_request)$fatal(1,"VIC matrix/glyph and sprite memory requests overlap");
`endif
    c64_color_ram color_ram (
        .clk_a(clk_sys),.addr_a(color_addr),.wdata_a(color_din[3:0]),.we_a(color_we),.q_a(color_cpu_q),
        .clk_b(clk_sys),.addr_b(native_color_address),.q_b(color_video_q)
    );
    reg [7:0] next_glyph, next_matrix, glyph, matrix;
    reg [3:0] next_color, cell_color;
    reg frame_started, vertical_border, set_vertical_border, main_border;
    reg [2:0] subpixel;
    // Each dot is prepared one SYS edge before its strobe. Keep dot 4
    // aligned with the shared second bus phase at SYS26 in both models.
    localparam DOT1_PREP = NTSC ? 6 : 7;
    localparam DOT3_PREP = NTSC ? 19 : 20;
    localparam DOT5_PREP = NTSC ? 32 : 33;
    localparam DOT6_PREP = NTSC ? 38 : 39;
    localparam DOT7_PREP = NTSC ? 45 : 46;
    reg pixel_strobe;
    always @* begin
        pixel_strobe=1; subpixel=0;
        case(cycle_clock)
            1:subpixel=0; DOT1_PREP+1:subpixel=1; 14:subpixel=2; DOT3_PREP+1:subpixel=3;
            27:subpixel=4; DOT5_PREP+1:subpixel=5; DOT6_PREP+1:subpixel=6; DOT7_PREP+1:subpixel=7;
            default:pixel_strobe=0;
        endcase
    end
    // The NTSC visible lower border spans raster wrap; publish after line 11.
    localparam integer CAPTURE_WRAP_LINES = NTSC ? 12 : 0;
    wire native_frame_start = first_phase && raster == CAPTURE_WRAP_LINES && native_cycle == 1;
    wire lower_edge = raster == (d011[3] ? `C64_BORDER_BOTTOM_25 : `C64_BORDER_BOTTOM_24);
    wire upper_edge = raster == (d011[3] ? `C64_BORDER_TOP_25 : `C64_BORDER_TOP_24) && d011[4];
    // Sample each border transition one SYS edge before its dot strobe.
    // CSEL1 edges are X24/344; CSEL0 edges are X31/335.
    wire left_edge = native_cycle == `C64_BORDER_LEFT_CYCLE &&
        (regs[6'h16][3] ? first_phase : cycle_clock == DOT7_PREP);
    wire right_edge = regs[6'h16][3] ? (first_phase && native_cycle == `C64_BORDER_RIGHT_40) :
        (cycle_clock == DOT7_PREP && native_cycle == `C64_BORDER_RIGHT_38);
    always @(posedge clk_sys) begin
        if(reset)begin
            frame_started<=0; vertical_border<=1;set_vertical_border<=1;main_border<=1;
            next_glyph<=0;next_matrix<=0;next_color<=0;glyph<=0;matrix<=0;cell_color<=0;
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
                glyph<=next_glyph;matrix<=next_matrix;cell_color<=next_color;
            end
        end
    end
    wire [3:0] graphics_ink;
    wire graphics_foreground,graphics_background0,sprite_background0;
    wire graphics_visible = native_cycle >= `C64_GLYPH_FIRST + 1 &&
        native_cycle <= `C64_GLYPH_LAST + 1 && !vertical_border;
    // Graphics go blank outside the forty-byte window; matrix/color
    // metadata retain the last visible byte until a later eligible load.
    reg [7:0] last_graphics_matrix;
    reg [3:0] last_graphics_color;
    always @(posedge clk_sys)begin
        if(reset)begin last_graphics_matrix<=0;last_graphics_color<=0;end
        else if(pixel_strobe&&subpixel==7&&graphics_visible)begin
            last_graphics_matrix<=matrix;last_graphics_color<=cell_color;
        end
    end
    wire [3:0] ink=(main_border||vertical_border)?border:sprite_ink;
    wire ink_background0=!(main_border||vertical_border)&&sprite_background0;
    c64_vic_graphics_pixels graphics(
        .clk(clk_sys),.reset(reset),.tick(pixel_strobe),.dot(subpixel),
        .glyph(graphics_visible?glyph:8'd0),.matrix(graphics_visible?matrix:last_graphics_matrix),
        .cell_color(graphics_visible?cell_color:last_graphics_color),.d011(d011),.d016(regs[6'h16]),
        .background({regs[6'h24][3:0],regs[6'h23][3:0],regs[6'h22][3:0],regs[6'h21][3:0]}),
        .latch_scroll(native_cycle >= `C64_MATRIX_FIRST && native_cycle <= `C64_MATRIX_LAST && !vertical_border),.color(graphics_ink),.foreground(graphics_foreground),
        .background0(graphics_background0),.color_q(),.foreground_q()
    );
    wire [7:0] ss_event,bg_event,ss_mask,bg_mask,collision_status;
    wire [3:0] collision_flags;
    reg event_sample;
    reg [7:0] ss_acc,bg_acc;
    always @(posedge clk_sys)begin
        if(reset)begin event_sample<=0;ss_acc<=0;bg_acc<=0;end
        else begin
            event_sample<=pixel_strobe;
            if(event_sample)begin ss_acc<=ss_acc|ss_event;bg_acc<=bg_acc|bg_event;end
            if(first_phase)begin ss_acc<=0;bg_acc<=0;end
        end
    end
    c64_vic_collisions collision(
        .clk(clk_sys),.reset(reset),.group_tick(first_phase),
        .read_sample(cs&&reading&&read_sample&&(addr==6'h1e||addr==6'h1f)),
        .read_bg(addr==6'h1f),.ack(cs&&we&&addr==6'h19),.ack_mask(din[3:0]),
        .other_events({3'd0,raster_event}),.enable(regs[6'h1a][3:0]),
        .ss_events(ss_acc),.bg_events(bg_acc),.ss_mask(ss_mask),.bg_mask(bg_mask),
        .flags(collision_flags),.read_value(),.status(collision_status),.irq(irq)
    );
    // The flags and sprite X describe the same native cycle.
    // Explicit bit decodes keep the prepared events in logic rather than a ROM.
    wire [7:0] pointer_event,row_event;
    for(genvar n=0;n<8;n=n+1)begin : sprite_cycle_events
        localparam [CYCLE_BITS-1:0] POINTER_CYCLE = `C64_SPRITE_POINTER(NTSC,n);
        localparam [CYCLE_BITS-1:0] ROW_CYCLE = `C64_SPRITE_ROW(NTSC,n);
        assign pointer_event[n]=native_cycle==POINTER_CYCLE;
        assign row_event[n]=native_cycle==ROW_CYCLE;
    end
    // Prepare the predictable beam coordinate and sprite cycle events one SYS
    // edge before each dot. Guest-controlled inputs retain their dot sampling.
    reg pixel_prepare;
    reg [2:0] prepare_dot;
    always @* begin
        pixel_prepare=1; prepare_dot=0;
        case(cycle_clock)
            0:prepare_dot=0; DOT1_PREP:prepare_dot=1; 13:prepare_dot=2; DOT3_PREP:prepare_dot=3;
            26:prepare_dot=4; DOT5_PREP:prepare_dot=5; DOT6_PREP:prepare_dot=6; DOT7_PREP:prepare_dot=7;
            default:pixel_prepare=0;
        endcase
    end
    // x64sc uses bits 8:3 of the Phi1 X coordinate as the sprite trigger
    // group. The 6567R8 has 65 cycles and 64 distinct groups: both cycles
    // 62 and 63 select X=384..391 before the counter advances again.
    localparam [8:0] LINE_START_X = NTSC == 2 ? 9'd412 : (NTSC ? 9'd408 : 9'd400);
    wire [CYCLE_BITS-1:0] beam_cycle = NTSC == 1 && native_cycle >= 63 ?
        native_cycle - 1'b1 : native_cycle;
    wire [8:0] prepare_x = native_cycle >= `C64_X_ZERO_CYCLE ?
        9'((beam_cycle - `C64_X_ZERO_CYCLE)*8) + {6'd0,prepare_dot} :
        LINE_START_X + 9'((native_cycle-1)*8) + {6'd0,prepare_dot};
    reg [8:0] sprite_dot_x_q;
    reg [7:0] sprite_pointer_q, sprite_row_q;
    reg sprite_pending_update_q;
    always @(posedge clk_sys) begin
        if(reset)begin
            sprite_dot_x_q<=0;
            // Cycle 1 fetches sprite 3's pointer on PAL and row on NTSC.
            sprite_pointer_q <= NTSC == 1 ? 0 : 8'h08;
            sprite_row_q <= NTSC == 1 ? 8'h08 : 0;
            sprite_pending_update_q<=0;
        end else if(pixel_prepare)begin
            sprite_dot_x_q<=prepare_x;
            sprite_pointer_q<=pointer_event; sprite_row_q<=row_event;
            sprite_pending_update_q<=native_cycle==`C64_SPRITE_POINTER(NTSC,0);
        end
    end
    // Final DMA byte is sampled at SYS30, after dot4 (SYS27).
    // Capture at dot5 (SYS34 PAL, SYS33 NTSC), before row halt releases at dot7.
    c64_vic_sprite_pixels #(.ROW_DATA_DOT(5)) pixel_sprites(
        .clk(clk_sys),.reset(reset),.tick(pixel_strobe),.dot(subpixel),.xpos(sprite_dot_x_q),
        .positions(sprite_x),.data(sprite_data),.pending_update(sprite_pending_update_q),
        .display_bits(sprite_display),.pointer_cycle(sprite_pointer_q),.row_cycle(sprite_row_q),
        .priority_bits(regs[6'h1b]),.multicolor_bits(regs[6'h1c]),.expand_bits(regs[6'h1d]),
        .foreground(graphics_foreground),.background0(graphics_background0),.background_color(graphics_ink),
        .multi0(regs[6'h25][3:0]),.multi1(regs[6'h26][3:0]),.colors(sprite_colors),
        .color(sprite_ink),.color_background0(sprite_background0),.color_q(),.ss_q(ss_event),.bg_q(bg_event),.active(),.pending(),.halt(),
        .ex_flop(),.mc_flop(),.shifts(),.pixels()
    );
    // VICE's normal border window for the 6567R8 is 384 x 247. Raster
    // lines 28..262 followed by 0..11 form one contiguous picture.
    // See vice/src/viciisc/vicii-timing.h, VICII_NTSC_NORMAL_*.
    localparam integer CAPTURE_FIRST_LINE = NTSC ? 28 : `C64_PAL_CAPTURE_FIRST_LINE;
    wire wrapped_line = NTSC && raster < CAPTURE_WRAP_LINES;
    wire visible_line = wrapped_line || (raster >= CAPTURE_FIRST_LINE &&
        (NTSC || raster <= `C64_PAL_CAPTURE_LAST_LINE));
    wire visible_native = visible_line && native_cycle >= `C64_CAPTURE_FIRST_CYCLE &&
        native_cycle <= `C64_CAPTURE_LAST_CYCLE;
    wire [8:0] native_y = wrapped_line ? 9'(raster + `C64_LINES_PER_FRAME(NTSC) - CAPTURE_FIRST_LINE) :
                                         9'(raster - CAPTURE_FIRST_LINE);
    wire [8:0] write_x=9'((native_cycle-`C64_CAPTURE_FIRST_CYCLE)*8)+{6'd0,subpixel};
    wire [16:0] write_address=17'(native_y*384)+{8'd0,write_x};

    // PAL6569R3 resolves D020/D021 three dots after selecting the pixel source.
    // Keep direct colors and the complete original framebuffer transaction
    // together; equal-valued direct cell/sprite colors are not palette sources.
    // Shifting on blank dots drains the last visible pixels before publication.
    reg [2:0] color_valid,color_background0,color_border;
    reg [16:0] color_address[0:2];
    reg [3:0] color_direct[0:2];
    reg frame_write_valid;
    reg [16:0] frame_write_address;
    reg [3:0] frame_write_ink;
    always @(posedge clk_sys) begin
        if(reset)begin
            color_valid<=0; color_background0<=0; color_border<=0;
            for(integer stage=0;stage<3;stage=stage+1)begin
                color_address[stage]<=0; color_direct[stage]<=0;
            end
            frame_write_valid<=0; frame_write_address<=0; frame_write_ink<=0;
        end else begin
            // The existing one-SYS write packet follows color resolution.
            // Graphics shifts, foreground and collisions retain their dot edge.
            frame_write_valid<=pixel_strobe&&color_valid[2];
            if(pixel_strobe)begin
                color_valid<={color_valid[1:0],visible_native};
                color_background0<={color_background0[1:0],ink_background0};
                color_border<={color_border[1:0],main_border||vertical_border};
                color_address[0]<=write_address;
                color_direct[0]<=ink;
                for(integer stage=1;stage<3;stage=stage+1)begin
                    color_address[stage]<=color_address[stage-1];
                    color_direct[stage]<=color_direct[stage-1];
                end
                if(color_valid[2])begin
                    frame_write_address<=color_address[2];
                    frame_write_ink<=color_border[2]?regs[6'h20][3:0]:
                        color_background0[2]?regs[6'h21][3:0]:color_direct[2];
                end
            end
        end
    end

    // Active-first CTA 720p60 coordinates, shared with the session plane.
    reg [10:0] hpos=0,hpos_q=0;
    reg [9:0] vpos=0,vpos_q=0;
    reg [2:0] xphase=0;
    // Source rows per 720p active line, reduced to the smallest integers.
    localparam integer Y_NUM = NTSC == 2 ? 246 : (NTSC ? 247 : 17);
    localparam integer Y_DEN = NTSC ? 720 : 45;
    localparam integer Y_BITS = $clog2(Y_DEN);
    reg [Y_BITS-1:0] yphase=0;
    reg [8:0] display_x=0,display_y=0;
    wire [16:0] read_address=17'(display_y*384)+{8'd0,display_x};
    wire [3:0] frame_color;
    c64_frame_store frame_store (
        .clk_sys(clk_sys),.pixel_clk(pixel_clk),.reset(reset),
        .write_enable(!reset&&frame_write_valid),.write_address(frame_write_address),.write_data(frame_write_ink),
        .frame_done(native_frame_start&&frame_started),.frame_start(hpos==0&&vpos==0),
        .read_address(read_address),.read_data(frame_color),.write_page(),.read_page(),.pending()
    );
    always @(posedge pixel_clk)begin
        hpos_q<=hpos;vpos_q<=vpos;
        if(hpos==1649)begin hpos<=0;vpos<=vpos==749?0:vpos+1'b1;end
        else hpos<=hpos+1'b1;
        // Both models have 384 source pixels across the 960x720 viewport.
        if(hpos==159)begin xphase<=0;display_x<=0;end
        else if(hpos>=160&&hpos<1119)begin
            if(xphase>=3)begin xphase<=xphase-3'd3;display_x<=display_x+1'b1;end
            else xphase<=xphase+3'd2;
        end
        if(hpos==0)begin
            if(vpos==0)begin yphase<=0;display_y<=0;end
            else if(vpos>0&&vpos<720)begin
                if (yphase >= Y_BITS'(Y_DEN-Y_NUM)) begin
                    yphase <= yphase - Y_BITS'(Y_DEN-Y_NUM);
                    display_y <= display_y + 1'b1;
                end else
                    yphase <= yphase + Y_BITS'(Y_NUM);
            end
        end
    end
    assign de=hpos_q<1280&&vpos_q<720;
    assign hsync=hpos_q>=1390&&hpos_q<1430;
    assign vsync=vpos_q>=725&&vpos_q<730;
    wire text_window=hpos_q>=160&&hpos_q<1120;
    wire [23:0] rgb=!de?24'd0:(text_window?palette(frame_color):24'd0);
    assign red=rgb[23:16];assign green=rgb[15:8];assign blue=rgb[7:0];
endmodule
