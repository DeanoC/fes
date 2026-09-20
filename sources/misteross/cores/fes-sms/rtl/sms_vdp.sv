// SPDX-License-Identifier: GPL-2.0-or-later
// Master System VDP wrapper with the shared TMS9918 path and an SMS Mode 4 path.

module sms_vdp (
    input  wire       clk,
    input  wire       reset,
    input  wire       cpu_ce,
    input  wire       cpu_iorq_n,
    input  wire       cpu_rd_n,
    input  wire       cpu_wr_n,
    input  wire [7:0] cpu_a,
    input  wire [7:0] cpu_din,
    output wire [7:0] cpu_dout,
    input  wire       raster_ce,
    output wire [7:0] raster_x,
    output wire [8:0] raster_y,
    output wire [5:0] raster_color,
    output wire       raster_blank,
    output wire       status_collision,
    output wire       status_overflow,
    output wire [4:0] status_fifth_index,
    output wire       irq_n
);
    wire [7:0] legacy_cpu_dout;
    wire [7:0] legacy_raster_x;
    wire [8:0] legacy_raster_y;
    wire [3:0] legacy_raster_pixel;
    wire legacy_raster_blank;
    wire legacy_status_collision;
    wire legacy_status_overflow;
    wire [4:0] legacy_status_fifth_index;
    wire legacy_irq_n;

    wire [7:0] mode4_cpu_dout;
    wire [7:0] mode4_raster_x;
    wire [8:0] mode4_raster_y;
    wire [5:0] mode4_raster_color;
    wire mode4_raster_blank;
    wire mode4_status_collision;
    wire mode4_status_overflow;
    wire mode4_irq_n;
    wire mode4_active;

    coleco_vdp legacy_vdp (
        .clk(clk),
        .reset(reset),
        .cpu_ce(cpu_ce),
        .cpu_iorq_n(cpu_iorq_n),
        .cpu_rd_n(cpu_rd_n),
        .cpu_wr_n(cpu_wr_n),
        .cpu_a(cpu_a),
        .cpu_din(cpu_din),
        .cpu_dout(legacy_cpu_dout),
        .raster_ce(raster_ce),
        .raster_x(legacy_raster_x),
        .raster_y(legacy_raster_y),
        .raster_pixel(legacy_raster_pixel),
        .raster_blank(legacy_raster_blank),
        .status_collision(legacy_status_collision),
        .status_overflow(legacy_status_overflow),
        .status_fifth_index(legacy_status_fifth_index),
        .irq_n(legacy_irq_n)
    );

    sms_mode4_vdp mode4_vdp (
        .clk(clk),
        .reset(reset),
        .cpu_ce(cpu_ce),
        .cpu_iorq_n(cpu_iorq_n),
        .cpu_rd_n(cpu_rd_n),
        .cpu_wr_n(cpu_wr_n),
        .cpu_a(cpu_a),
        .cpu_din(cpu_din),
        .cpu_dout(mode4_cpu_dout),
        .raster_ce(raster_ce),
        .raster_x(mode4_raster_x),
        .raster_y(mode4_raster_y),
        .raster_color(mode4_raster_color),
        .raster_blank(mode4_raster_blank),
        .status_collision(mode4_status_collision),
        .status_overflow(mode4_status_overflow),
        .irq_n(mode4_irq_n),
        .mode4_active(mode4_active)
    );

    assign cpu_dout = mode4_active ? mode4_cpu_dout : legacy_cpu_dout;
    assign raster_x = mode4_active ? mode4_raster_x : legacy_raster_x;
    assign raster_y = mode4_active ? mode4_raster_y : legacy_raster_y;
    function [5:0] legacy_color;
        input [3:0] code;
        begin
            // The same fixed TMS palette, quantized to SMS two-bit RGB lanes.
            case (code)
                2: legacy_color=6'h1c; 3: legacy_color=6'h1d;
                4: legacy_color=6'h35; 5: legacy_color=6'h35;
                6: legacy_color=6'h17; 7: legacy_color=6'h3d;
                8: legacy_color=6'h17; 9: legacy_color=6'h17;
                10: legacy_color=6'h1f; 11: legacy_color=6'h2f;
                12: legacy_color=6'h08; 13: legacy_color=6'h27;
                14: legacy_color=6'h3f; 15: legacy_color=6'h3f;
                default: legacy_color=6'h00;
            endcase
        end
    endfunction
    assign raster_color = mode4_active ? mode4_raster_color : legacy_color(legacy_raster_pixel);
    assign raster_blank = mode4_active ? mode4_raster_blank : legacy_raster_blank;
    assign status_collision = mode4_active ? mode4_status_collision :
                              legacy_status_collision;
    assign status_overflow = mode4_active ? mode4_status_overflow :
                             legacy_status_overflow;
    assign status_fifth_index = mode4_active ? 5'h00 : legacy_status_fifth_index;
    assign irq_n = mode4_active ? mode4_irq_n : legacy_irq_n;
endmodule

module sms_mode4_vdp (
    input  wire       clk,
    input  wire       reset,
    input  wire       cpu_ce,
    input  wire       cpu_iorq_n,
    input  wire       cpu_rd_n,
    input  wire       cpu_wr_n,
    input  wire [7:0] cpu_a,
    input  wire [7:0] cpu_din,
    output reg  [7:0] cpu_dout,
    input  wire       raster_ce,
    output reg  [7:0] raster_x,
    output reg  [8:0] raster_y,
    output reg  [5:0] raster_color,
    output wire       raster_blank,
    output reg        status_collision,
    output reg        status_overflow,
    output wire       irq_n,
    output wire       mode4_active
);
    localparam [4:0] BUILD_IDLE              = 5'd0;
    localparam [4:0] BG_NAME_LOW_REQUEST     = 5'd1;
    localparam [4:0] BG_NAME_LOW_WAIT        = 5'd2;
    localparam [4:0] BG_NAME_HIGH_REQUEST    = 5'd3;
    localparam [4:0] BG_NAME_HIGH_WAIT       = 5'd4;
    localparam [4:0] BG_PLANE0_REQUEST       = 5'd5;
    localparam [4:0] BG_PLANE0_WAIT          = 5'd6;
    localparam [4:0] BG_PLANE1_REQUEST       = 5'd7;
    localparam [4:0] BG_PLANE1_WAIT          = 5'd8;
    localparam [4:0] BG_PLANE2_REQUEST       = 5'd9;
    localparam [4:0] BG_PLANE2_WAIT          = 5'd10;
    localparam [4:0] BG_PLANE3_REQUEST       = 5'd11;
    localparam [4:0] BG_PLANE3_WAIT          = 5'd12;
    localparam [4:0] BG_PIXEL_WRITE          = 5'd13;
    localparam [4:0] SPRITE_Y_REQUEST        = 5'd14;
    localparam [4:0] SPRITE_Y_WAIT           = 5'd15;
    localparam [4:0] SPRITE_TEST             = 5'd16;
    localparam [4:0] SPRITE_X_REQUEST        = 5'd17;
    localparam [4:0] SPRITE_X_WAIT           = 5'd18;
    localparam [4:0] SPRITE_TILE_REQUEST     = 5'd19;
    localparam [4:0] SPRITE_TILE_WAIT        = 5'd20;
    localparam [4:0] SPRITE_PLANE0_REQUEST   = 5'd21;
    localparam [4:0] SPRITE_PLANE0_WAIT      = 5'd22;
    localparam [4:0] SPRITE_PLANE1_REQUEST   = 5'd23;
    localparam [4:0] SPRITE_PLANE1_WAIT      = 5'd24;
    localparam [4:0] SPRITE_PLANE2_REQUEST   = 5'd25;
    localparam [4:0] SPRITE_PLANE2_WAIT      = 5'd26;
    localparam [4:0] SPRITE_PLANE3_REQUEST   = 5'd27;
    localparam [4:0] SPRITE_PLANE3_WAIT      = 5'd28;
    localparam [4:0] SPRITE_PIXEL_READ       = 5'd29;
    localparam [4:0] SPRITE_PIXEL_WRITE      = 5'd30;
    localparam [4:0] BUILD_FINISH            = 5'd31;

    reg [7:0] vdp_reg [0:10];
    reg [5:0] cram [0:31];
    reg [7:0] control_first;
    reg control_latch;
    reg [1:0] command_code;
    reg [13:0] vram_addr;
    reg [13:0] prefetch_addr;
    reg [1:0] prefetch_pending;
    reg [7:0] vram_read_q;
    reg read_seen;
    reg [7:0] read_result;
    reg status_vblank;
    reg line_pending;
    reg [7:0] line_counter;

    reg [4:0] build_state;
    reg [7:0] build_target_y;
    reg [7:0] build_x;
    reg [15:0] bg_descriptor;
    reg [7:0] bg_plane0;
    reg [7:0] bg_plane1;
    reg [7:0] bg_plane2;
    reg [7:0] bg_plane3;
    reg build_bank;
    reg display_bank;
    reg [7:0] display_y;
    reg ready_valid;
    reg ready_bank;
    reg [7:0] ready_y;
    reg ready_collision;
    reg ready_overflow;
    reg build_collision;
    reg build_overflow;

    reg [5:0] sprite_index;
    reg [3:0] sprite_visible_count;
    reg [7:0] sprite_y;
    reg [7:0] sprite_x;
    reg [7:0] sprite_tile;
    reg [4:0] sprite_source_row;
    reg [7:0] sprite_plane0;
    reg [7:0] sprite_plane1;
    reg [7:0] sprite_plane2;
    reg [7:0] sprite_plane3;
    reg [3:0] sprite_column;
    reg sprite_repeat;
    reg signed [9:0] sprite_screen_x;

    wire bus_write = cpu_ce && !cpu_iorq_n && !cpu_wr_n;
    wire read_active = !cpu_iorq_n && !cpu_rd_n;
    wire bus_read = cpu_ce && read_active && !read_seen;
    wire control_port = cpu_a == 8'hbf;
    wire data_port = cpu_a == 8'hbe;
    wire vram_data_write = bus_write && data_port && command_code != 2'b11;
    wire cram_data_write = bus_write && data_port && command_code == 2'b11;
    wire [13:0] cpu_vram_address = vram_data_write ? vram_addr : prefetch_addr;
    wire [7:0] vram_cpu_read;
    wire [7:0] vram_render_read;
    reg [13:0] render_vram_address;

    wire [13:0] name_table_base = {vdp_reg[2][3:1], 11'b0};
    wire [13:0] sprite_table_base = {vdp_reg[5][6:1], 8'b0};
    wire [8:0] bg_y_sum = {1'b0, build_target_y} + {1'b0, vdp_reg[9]};
    wire [7:0] bg_scrolled_y = bg_y_sum >= 9'd224 ? bg_y_sum - 9'd224 :
                               bg_y_sum[7:0];
    wire [7:0] bg_source_y = (vdp_reg[0][7] && build_x >= 8'd192) ?
                             build_target_y : bg_scrolled_y;
    wire [7:0] bg_source_x = (vdp_reg[0][6] && build_target_y < 8'd16) ?
                             build_x : build_x - vdp_reg[8];
    wire [13:0] bg_name_address = name_table_base +
                                  ({9'b0, bg_source_y[7:3]} << 6) +
                                  ({9'b0, bg_source_x[7:3]} << 1);
    wire [8:0] bg_tile_index = {bg_descriptor[8], bg_descriptor[7:0]};
    wire [2:0] bg_pattern_row = bg_descriptor[10] ?
                                3'd7 - bg_source_y[2:0] : bg_source_y[2:0];
    wire [13:0] bg_pattern_address = ({5'b0, bg_tile_index} << 5) +
                                     ({11'b0, bg_pattern_row} << 2);
    wire [2:0] bg_pattern_bit = bg_descriptor[9] ? bg_source_x[2:0] :
                                3'd7 - bg_source_x[2:0];
    wire [3:0] bg_color_index = {bg_plane3[bg_pattern_bit],
                                 bg_plane2[bg_pattern_bit],
                                 bg_plane1[bg_pattern_bit],
                                 bg_plane0[bg_pattern_bit]};
    wire [6:0] bg_line_data = (vdp_reg[0][5] && build_x < 8'd8) ? 7'h00 :
                              {1'b0, bg_descriptor[12], bg_descriptor[11],
                               bg_color_index};

    wire sprite_zoom = vdp_reg[1][0];
    wire sprite_tall = vdp_reg[1][1];
    wire [5:0] sprite_height = sprite_tall ? (sprite_zoom ? 6'd32 : 6'd16) :
                               (sprite_zoom ? 6'd16 : 6'd8);
    wire signed [9:0] sprite_top = sprite_y >= 8'he0 ?
                                   $signed({2'b11, sprite_y}) + 10'sd1 :
                                   $signed({2'b00, sprite_y}) + 10'sd1;
    wire signed [10:0] sprite_delta = $signed({3'b000, build_target_y}) -
                                      sprite_top;
    wire sprite_visible = sprite_delta >= 0 && sprite_delta < sprite_height;
    wire [4:0] sprite_row = sprite_delta >>> (sprite_zoom ? 1 : 0);
    wire [8:0] sprite_tile_index = {vdp_reg[6][2],
                                    sprite_tall ? {sprite_tile[7:1], 1'b0} :
                                                  sprite_tile};
    wire [13:0] sprite_pattern_address = ({5'b0, sprite_tile_index} << 5) +
                                         ({9'b0, sprite_source_row} << 2);
    wire [2:0] sprite_pattern_bit = 3'd7 - sprite_column[2:0];
    wire [3:0] sprite_color_index = {sprite_plane3[sprite_pattern_bit],
                                     sprite_plane2[sprite_pattern_bit],
                                     sprite_plane1[sprite_pattern_bit],
                                     sprite_plane0[sprite_pattern_bit]};
    wire sprite_pixel_in_range = sprite_screen_x >= 0 && sprite_screen_x < 256;

    reg [7:0] line_address_a;
    reg [6:0] line_data_a;
    reg line_write_a;
    wire [6:0] line_a_build_read;
    wire [6:0] line_b_build_read;
    wire [6:0] line_a_display_read;
    wire [6:0] line_b_display_read;
    wire [6:0] build_line_read = build_bank ? line_b_build_read :
                                             line_a_build_read;
    wire [6:0] display_line_read = display_bank ? line_b_display_read :
                                                 line_a_display_read;
    wire sprite_can_replace = !build_line_read[6] &&
                              (!build_line_read[5] || build_line_read[3:0] == 4'h0);
    wire [6:0] sprite_line_data = sprite_can_replace ?
                                  {1'b1, 1'b0, 1'b1, sprite_color_index} :
                                  {1'b1, build_line_read[5:0]};
    wire [4:0] display_palette_index = display_line_read[3:0] == 4'h0 ?
                                       {1'b1, vdp_reg[7][3:0]} :
                                       {display_line_read[4], display_line_read[3:0]};

    assign mode4_active = vdp_reg[0][2];
    assign raster_blank = raster_y >= 9'd192;
    assign irq_n = !((status_vblank && vdp_reg[1][5]) ||
                     (line_pending && vdp_reg[0][4]));

    coleco_dpram #(
        .ADDRWIDTH(14),
        .NUMWORDS(16384)
    ) mode4_vram (
        .clock(clk),
        .address_a(cpu_vram_address),
        .data_a(cpu_din),
        .wren_a(vram_data_write),
        .q_a(vram_cpu_read),
        .address_b(render_vram_address),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(vram_render_read)
    );

    coleco_video_dpram #(
        .DATAWIDTH(7),
        .ADDRWIDTH(8),
        .NUMWORDS(256)
    ) mode4_line_a (
        .clock_a(clk),
        .address_a(line_address_a),
        .data_a(line_data_a),
        .wren_a(line_write_a && !build_bank),
        .q_a(line_a_build_read),
        .clock_b(clk),
        .address_b(raster_x),
        .data_b(7'h00),
        .wren_b(1'b0),
        .q_b(line_a_display_read)
    );

    coleco_video_dpram #(
        .DATAWIDTH(7),
        .ADDRWIDTH(8),
        .NUMWORDS(256)
    ) mode4_line_b (
        .clock_a(clk),
        .address_a(line_address_a),
        .data_a(line_data_a),
        .wren_a(line_write_a && build_bank),
        .q_a(line_b_build_read),
        .clock_b(clk),
        .address_b(raster_x),
        .data_b(7'h00),
        .wren_b(1'b0),
        .q_b(line_b_display_read)
    );

    always @* begin
        render_vram_address = 14'h0000;
        case (build_state)
            BG_NAME_LOW_REQUEST, BG_NAME_LOW_WAIT:
                render_vram_address = bg_name_address;
            BG_NAME_HIGH_REQUEST, BG_NAME_HIGH_WAIT:
                render_vram_address = bg_name_address + 14'd1;
            BG_PLANE0_REQUEST, BG_PLANE0_WAIT:
                render_vram_address = bg_pattern_address;
            BG_PLANE1_REQUEST, BG_PLANE1_WAIT:
                render_vram_address = bg_pattern_address + 14'd1;
            BG_PLANE2_REQUEST, BG_PLANE2_WAIT:
                render_vram_address = bg_pattern_address + 14'd2;
            BG_PLANE3_REQUEST, BG_PLANE3_WAIT:
                render_vram_address = bg_pattern_address + 14'd3;
            SPRITE_Y_REQUEST, SPRITE_Y_WAIT:
                render_vram_address = sprite_table_base + sprite_index;
            SPRITE_X_REQUEST, SPRITE_X_WAIT:
                render_vram_address = sprite_table_base + 14'h0080 +
                                      ({8'b0, sprite_index} << 1);
            SPRITE_TILE_REQUEST, SPRITE_TILE_WAIT:
                render_vram_address = sprite_table_base + 14'h0081 +
                                      ({8'b0, sprite_index} << 1);
            SPRITE_PLANE0_REQUEST, SPRITE_PLANE0_WAIT:
                render_vram_address = sprite_pattern_address;
            SPRITE_PLANE1_REQUEST, SPRITE_PLANE1_WAIT:
                render_vram_address = sprite_pattern_address + 14'd1;
            SPRITE_PLANE2_REQUEST, SPRITE_PLANE2_WAIT:
                render_vram_address = sprite_pattern_address + 14'd2;
            SPRITE_PLANE3_REQUEST, SPRITE_PLANE3_WAIT:
                render_vram_address = sprite_pattern_address + 14'd3;
            default: render_vram_address = 14'h0000;
        endcase
    end

    always @* begin
        line_address_a = build_x;
        line_data_a = bg_line_data;
        line_write_a = build_state == BG_PIXEL_WRITE;
        if (build_state == SPRITE_PIXEL_READ ||
            build_state == SPRITE_PIXEL_WRITE) begin
            line_address_a = sprite_screen_x[7:0];
            line_data_a = sprite_line_data;
            line_write_a = build_state == SPRITE_PIXEL_WRITE &&
                           sprite_pixel_in_range && sprite_color_index != 4'h0;
        end
    end

    integer init_index;
    initial begin
        control_first = 8'h00;
        control_latch = 1'b0;
        command_code = 2'b00;
        vram_addr = 14'h0000;
        prefetch_addr = 14'h0000;
        prefetch_pending = 2'b00;
        vram_read_q = 8'hff;
        read_seen = 1'b0;
        read_result = 8'hff;
        status_vblank = 1'b0;
        status_collision = 1'b0;
        status_overflow = 1'b0;
        line_pending = 1'b0;
        line_counter = 8'h00;
        raster_x = 8'h00;
        raster_y = 9'h000;
        build_state = BUILD_IDLE;
        build_target_y = 8'h00;
        build_x = 8'h00;
        bg_descriptor = 16'h0000;
        bg_plane0 = 8'h00;
        bg_plane1 = 8'h00;
        bg_plane2 = 8'h00;
        bg_plane3 = 8'h00;
        build_bank = 1'b1;
        display_bank = 1'b0;
        display_y = 8'hff;
        ready_valid = 1'b0;
        ready_bank = 1'b0;
        ready_y = 8'h00;
        ready_collision = 1'b0;
        ready_overflow = 1'b0;
        build_collision = 1'b0;
        build_overflow = 1'b0;
        sprite_index = 6'h00;
        sprite_visible_count = 4'h0;
        sprite_y = 8'h00;
        sprite_x = 8'h00;
        sprite_tile = 8'h00;
        sprite_source_row = 5'h00;
        sprite_plane0 = 8'h00;
        sprite_plane1 = 8'h00;
        sprite_plane2 = 8'h00;
        sprite_plane3 = 8'h00;
        sprite_column = 4'h0;
        sprite_repeat = 1'b0;
        sprite_screen_x = 10'sd0;
        for (init_index = 0; init_index < 11; init_index = init_index + 1)
            vdp_reg[init_index] = 8'h00;
        for (init_index = 0; init_index < 32; init_index = init_index + 1)
            cram[init_index] = 6'h00;
    end

    always @(posedge clk) begin
        if (reset) begin
            control_first <= 8'h00;
            control_latch <= 1'b0;
            command_code <= 2'b00;
            vram_addr <= 14'h0000;
            prefetch_addr <= 14'h0000;
            prefetch_pending <= 2'b00;
            vram_read_q <= 8'hff;
            read_seen <= 1'b0;
            read_result <= 8'hff;
            status_vblank <= 1'b0;
            status_collision <= 1'b0;
            status_overflow <= 1'b0;
            line_pending <= 1'b0;
            line_counter <= 8'h00;
            raster_x <= 8'h00;
            raster_y <= 9'h000;
            build_state <= BUILD_IDLE;
            build_target_y <= 8'h00;
            build_x <= 8'h00;
            build_bank <= 1'b1;
            display_bank <= 1'b0;
            display_y <= 8'hff;
            ready_valid <= 1'b0;
            build_collision <= 1'b0;
            build_overflow <= 1'b0;
            for (init_index = 0; init_index < 11; init_index = init_index + 1)
                vdp_reg[init_index] <= 8'h00;
            for (init_index = 0; init_index < 32; init_index = init_index + 1)
                cram[init_index] <= 6'h00;
        end else begin
            if (!read_active)
                read_seen <= 1'b0;
            else if (bus_read) begin
                read_seen <= 1'b1;
                read_result <= data_port ? vram_read_q :
                               control_port ? {status_vblank, status_overflow,
                                               status_collision, 5'b00000} : 8'hff;
            end

            prefetch_pending <= {1'b0, prefetch_pending[1]};
            if (prefetch_pending[0])
                vram_read_q <= vram_cpu_read;

            if (bus_write && control_port) begin
                if (!control_latch) begin
                    control_first <= cpu_din;
                    control_latch <= 1'b1;
                end else begin
                    control_latch <= 1'b0;
                    command_code <= cpu_din[7:6];
                    if (cpu_din[7:6] == 2'b10) begin
                        if (cpu_din[3:0] <= 4'd10)
                            vdp_reg[cpu_din[3:0]] <= control_first;
                    end else begin
                        vram_addr <= {cpu_din[5:0], control_first};
                        prefetch_pending <= 2'b00;
                        if (cpu_din[7:6] == 2'b00) begin
                            prefetch_addr <= {cpu_din[5:0], control_first};
                            prefetch_pending <= 2'b10;
                            vram_addr <= {cpu_din[5:0], control_first} + 14'd1;
                        end
                    end
                end
            end

            if (bus_write && data_port) begin
                if (cram_data_write)
                    cram[vram_addr[4:0]] <= cpu_din[5:0];
                vram_addr <= vram_addr + 1'b1;
                control_latch <= 1'b0;
                if (!cram_data_write)
                    vram_read_q <= cpu_din;
                prefetch_pending <= 2'b00;
            end

            if (bus_read && data_port) begin
                prefetch_addr <= vram_addr;
                prefetch_pending <= 2'b10;
                vram_addr <= vram_addr + 1'b1;
                control_latch <= 1'b0;
            end

            if (bus_read && control_port) begin
                status_vblank <= 1'b0;
                status_collision <= 1'b0;
                status_overflow <= 1'b0;
                line_pending <= 1'b0;
                control_latch <= 1'b0;
            end

            if (ready_valid && ready_y == raster_y[7:0] &&
                raster_y < 9'd192 && display_y != raster_y[7:0]) begin
                display_bank <= ready_bank;
                build_bank <= ~ready_bank;
                display_y <= ready_y;
                ready_valid <= 1'b0;
                if (ready_collision)
                    status_collision <= 1'b1;
                if (ready_overflow)
                    status_overflow <= 1'b1;
            end

            if (raster_ce) begin
                if (raster_x == 8'd255) begin
                    raster_x <= 8'h00;
                    if (raster_y == 9'd261)
                        raster_y <= 9'h000;
                    else
                        raster_y <= raster_y + 1'b1;

                    if (raster_y == 9'd191)
                        status_vblank <= 1'b1;
                    if (raster_y < 9'd192) begin
                        if (line_counter == 8'h00) begin
                            line_counter <= vdp_reg[10];
                            line_pending <= 1'b1;
                        end else begin
                            line_counter <= line_counter - 1'b1;
                        end
                    end else begin
                        line_counter <= vdp_reg[10];
                    end
                end else begin
                    raster_x <= raster_x + 1'b1;
                end
            end

            case (build_state)
                BUILD_IDLE: begin
                    if (!ready_valid) begin
                        if (raster_y < 9'd192 && display_y != raster_y[7:0])
                            build_target_y <= raster_y[7:0];
                        else if (raster_y < 9'd191)
                            build_target_y <= raster_y[7:0] + 1'b1;
                        else
                            build_target_y <= 8'h00;
                        build_x <= 8'h00;
                        build_collision <= 1'b0;
                        build_overflow <= 1'b0;
                        build_state <= BG_NAME_LOW_REQUEST;
                    end
                end

                BG_NAME_LOW_REQUEST: build_state <= BG_NAME_LOW_WAIT;
                BG_NAME_LOW_WAIT: begin
                    bg_descriptor[7:0] <= vram_render_read;
                    build_state <= BG_NAME_HIGH_REQUEST;
                end
                BG_NAME_HIGH_REQUEST: build_state <= BG_NAME_HIGH_WAIT;
                BG_NAME_HIGH_WAIT: begin
                    bg_descriptor[15:8] <= vram_render_read;
                    build_state <= BG_PLANE0_REQUEST;
                end
                BG_PLANE0_REQUEST: build_state <= BG_PLANE0_WAIT;
                BG_PLANE0_WAIT: begin
                    bg_plane0 <= vram_render_read;
                    build_state <= BG_PLANE1_REQUEST;
                end
                BG_PLANE1_REQUEST: build_state <= BG_PLANE1_WAIT;
                BG_PLANE1_WAIT: begin
                    bg_plane1 <= vram_render_read;
                    build_state <= BG_PLANE2_REQUEST;
                end
                BG_PLANE2_REQUEST: build_state <= BG_PLANE2_WAIT;
                BG_PLANE2_WAIT: begin
                    bg_plane2 <= vram_render_read;
                    build_state <= BG_PLANE3_REQUEST;
                end
                BG_PLANE3_REQUEST: build_state <= BG_PLANE3_WAIT;
                BG_PLANE3_WAIT: begin
                    bg_plane3 <= vram_render_read;
                    build_state <= BG_PIXEL_WRITE;
                end
                BG_PIXEL_WRITE: begin
                    if (build_x == 8'hff) begin
                        sprite_index <= 6'h00;
                        sprite_visible_count <= 4'h0;
                        build_state <= SPRITE_Y_REQUEST;
                    end else begin
                        build_x <= build_x + 1'b1;
                        build_state <= BG_NAME_LOW_REQUEST;
                    end
                end

                SPRITE_Y_REQUEST: build_state <= SPRITE_Y_WAIT;
                SPRITE_Y_WAIT: begin
                    sprite_y <= vram_render_read;
                    build_state <= SPRITE_TEST;
                end
                SPRITE_TEST: begin
                    if (sprite_y == 8'hd0) begin
                        build_state <= BUILD_FINISH;
                    end else if (sprite_visible) begin
                        if (sprite_visible_count == 4'd8) begin
                            build_overflow <= 1'b1;
                            if (sprite_index == 6'd63)
                                build_state <= BUILD_FINISH;
                            else begin
                                sprite_index <= sprite_index + 1'b1;
                                build_state <= SPRITE_Y_REQUEST;
                            end
                        end else begin
                            sprite_visible_count <= sprite_visible_count + 1'b1;
                            sprite_source_row <= sprite_row;
                            build_state <= SPRITE_X_REQUEST;
                        end
                    end else if (sprite_index == 6'd63) begin
                        build_state <= BUILD_FINISH;
                    end else begin
                        sprite_index <= sprite_index + 1'b1;
                        build_state <= SPRITE_Y_REQUEST;
                    end
                end
                SPRITE_X_REQUEST: build_state <= SPRITE_X_WAIT;
                SPRITE_X_WAIT: begin
                    sprite_x <= vram_render_read;
                    build_state <= SPRITE_TILE_REQUEST;
                end
                SPRITE_TILE_REQUEST: build_state <= SPRITE_TILE_WAIT;
                SPRITE_TILE_WAIT: begin
                    sprite_tile <= vram_render_read;
                    build_state <= SPRITE_PLANE0_REQUEST;
                end
                SPRITE_PLANE0_REQUEST: build_state <= SPRITE_PLANE0_WAIT;
                SPRITE_PLANE0_WAIT: begin
                    sprite_plane0 <= vram_render_read;
                    build_state <= SPRITE_PLANE1_REQUEST;
                end
                SPRITE_PLANE1_REQUEST: build_state <= SPRITE_PLANE1_WAIT;
                SPRITE_PLANE1_WAIT: begin
                    sprite_plane1 <= vram_render_read;
                    build_state <= SPRITE_PLANE2_REQUEST;
                end
                SPRITE_PLANE2_REQUEST: build_state <= SPRITE_PLANE2_WAIT;
                SPRITE_PLANE2_WAIT: begin
                    sprite_plane2 <= vram_render_read;
                    build_state <= SPRITE_PLANE3_REQUEST;
                end
                SPRITE_PLANE3_REQUEST: build_state <= SPRITE_PLANE3_WAIT;
                SPRITE_PLANE3_WAIT: begin
                    sprite_plane3 <= vram_render_read;
                    sprite_column <= 4'h0;
                    sprite_repeat <= 1'b0;
                    sprite_screen_x <= $signed({2'b00, sprite_x}) -
                                       (vdp_reg[0][3] ? 10'sd8 : 10'sd0);
                    build_state <= SPRITE_PIXEL_READ;
                end
                SPRITE_PIXEL_READ: build_state <= SPRITE_PIXEL_WRITE;
                SPRITE_PIXEL_WRITE: begin
                    if (sprite_pixel_in_range && sprite_color_index != 4'h0 &&
                        build_line_read[6])
                        build_collision <= 1'b1;
                    if (sprite_zoom && !sprite_repeat) begin
                        sprite_repeat <= 1'b1;
                        sprite_screen_x <= sprite_screen_x + 1'b1;
                        build_state <= SPRITE_PIXEL_READ;
                    end else if (sprite_column == 4'd7) begin
                        if (sprite_index == 6'd63)
                            build_state <= BUILD_FINISH;
                        else begin
                            sprite_index <= sprite_index + 1'b1;
                            build_state <= SPRITE_Y_REQUEST;
                        end
                    end else begin
                        sprite_column <= sprite_column + 1'b1;
                        sprite_repeat <= 1'b0;
                        sprite_screen_x <= sprite_screen_x + 1'b1;
                        build_state <= SPRITE_PIXEL_READ;
                    end
                end

                BUILD_FINISH: begin
                    ready_valid <= 1'b1;
                    ready_bank <= build_bank;
                    ready_y <= build_target_y;
                    ready_collision <= build_collision;
                    ready_overflow <= build_overflow;
                    build_state <= BUILD_IDLE;
                end
            endcase
        end
    end

    always @* begin
        cpu_dout = 8'hff;
        if (read_active) begin
            if (read_seen)
                cpu_dout = read_result;
            else if (data_port)
                cpu_dout = vram_read_q;
            else if (control_port)
                cpu_dout = {status_vblank, status_overflow,
                            status_collision, 5'b00000};
        end
    end

    always @* begin
        raster_color = cram[{1'b1, vdp_reg[7][3:0]}];
        if (vdp_reg[1][6] && !raster_blank && display_y == raster_y[7:0] &&
            !(vdp_reg[0][5] && raster_x < 8'd8))
            raster_color = cram[display_palette_index];
    end
endmodule
