// SPDX-License-Identifier: GPL-2.0-or-later
// Bounded TMS9918-compatible video path for the FES ColecoVision slice.
//
// The first bringup implements Graphics I name/pattern/color tables, a bounded
// Graphics II sprite path, the control/data ports, a 16 KiB VRAM aperture, and
// VBlank/collision/overflow status. The raster is deliberately exposed in the
// logical 256x192 domain; the video shell owns the 720p timing and scaling.

module coleco_vdp (
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
    output reg  [1:0] raster_pixel,
    output reg        raster_blank,
    output reg        status_collision,
    output reg        status_overflow,
    output reg  [4:0] status_fifth_index,
    output wire       irq_n
);
    localparam [13:0] VRAM_LAST = 14'h3fff;

`ifdef FES_COLECO_OSS
`define FES_COLECO_REGISTERED_VDP
`elsif QUARTUS
`define FES_COLECO_REGISTERED_VDP
`endif

`ifndef FES_COLECO_REGISTERED_VDP
    (* ramstyle = "M10K" *) reg [7:0] vram [0:16383];
`else
    // Quartus 17.0 and the OSS mapper cannot keep the CPU port plus four raster
    // reads on one inferred memory. Four coherent read copies keep each
    // lookup on an explicit dual-port M10K shape; CPU writes are broadcast to
    // all copies.
    wire [7:0] vram_cpu_read;
    wire [7:0] vram_cpu_read_pattern;
    wire [7:0] vram_cpu_read_color;
    wire [7:0] vram_name_read;
    wire [7:0] vram_pattern_read;
    wire [7:0] vram_color_read;
    wire [7:0] vram_cpu_read_sprite;
    wire [7:0] vram_sprite_read;
    reg [7:0] oss_scan_x;
    reg [8:0] oss_scan_y;
    reg [7:0] oss_launch_x;
    reg [8:0] oss_launch_y;
    reg       oss_launch_valid;
    reg [7:0] oss_name_coord_x;
    reg [8:0] oss_name_coord_y;
    reg       oss_name_valid;
    reg [7:0] oss_pattern_coord_x;
    reg [8:0] oss_pattern_coord_y;
    reg       oss_pattern_valid;
    // Sprite evaluation uses a fourth coherent VRAM copy. It walks the SAT
    // and the selected pattern row serially while the current line renders
    // from one of two small line buffers.
    reg [13:0] sprite_vram_address;
    // Each line bank is a packed 4-bit M10K entry: pixel[1:0], occupied[2],
    // and visible[3]. Port A supplies a registered renderer read/write and
    // port B supplies the registered raster read.
    wire [3:0] sprite_line_render_a_read;
    wire [3:0] sprite_line_render_b_read;
    wire [3:0] sprite_line_display_a_read;
    wire [3:0] sprite_line_display_b_read;
    reg        sprite_display_bank;
    reg        sprite_build_bank;
    reg [7:0]  sprite_display_y;
    reg [7:0]  sprite_ready_y;
    reg        sprite_ready_bank;
    reg        sprite_ready_collision;
    reg        sprite_ready_overflow;
    reg [4:0]  sprite_ready_fifth_index;
    reg        sprite_pending_valid;
    reg        sprite_pending_bank;
    reg [7:0]  sprite_pending_y;
    reg        sprite_pending_collision;
    reg        sprite_pending_overflow;
    reg [4:0]  sprite_pending_fifth_index;
    reg [3:0]  sprite_eval_state;
    reg [4:0]  sprite_eval_index;
    reg [2:0]  sprite_attr_byte;
    reg [7:0]  sprite_attr_y;
    reg [7:0]  sprite_attr_x;
    reg [7:0]  sprite_attr_pattern;
    reg [7:0]  sprite_attr_color;
    reg [7:0]  sprite_eval_target_y;
    reg [4:0]  sprite_source_row;
    reg [5:0]  sprite_visible_count;
    reg        sprite_build_collision;
    reg        sprite_build_overflow;
    reg [4:0]  sprite_build_fifth_index;
    reg [1:0]  sprite_pattern_byte;
    reg [7:0]  sprite_pattern_left;
    reg [7:0]  sprite_pattern_right;
    reg [7:0]  sprite_clear_address;
    reg [4:0]  sprite_render_col;
    reg        sprite_render_rep;
    function integer sprite_pixel_x;
        input [7:0] attr_x;
        input       early_clock;
        input integer column;
        input integer repeat_index;
        input       magnified;
        integer base_x;
        begin
            if (early_clock)
                base_x = $signed({1'b0, attr_x}) - 32;
            else
                base_x = attr_x;
            sprite_pixel_x = base_x + column * (magnified ? 2 : 1) + repeat_index;
        end
    endfunction
`endif
    reg [7:0] vdp_reg [0:7];
    reg [7:0] control_first;
    reg       control_latch;
    reg [13:0] vram_addr;
    reg [7:0] vram_read_q;
    reg [13:0] prefetch_addr;
    reg [1:0] prefetch_pending;
    reg read_seen;
    reg [7:0] read_result;
    reg       status_vblank;
    // Coleco connects the VDP's active-low interrupt to Z80 NMI, not INT.
    // Enabling IE with a frame already pending must assert immediately.
    assign irq_n = !(status_vblank && vdp_reg[1][5]);

    integer init_index;
    initial begin
        control_first = 8'h00;
        control_latch = 1'b0;
        vram_addr = 14'h0000;
        vram_read_q = 8'hff;
        prefetch_addr = 14'h0000;
        prefetch_pending = 2'b00;
        read_seen = 1'b0;
        read_result = 8'hff;
        status_vblank = 1'b0;
        status_collision = 1'b0;
        status_overflow = 1'b0;
        status_fifth_index = 5'h00;
        raster_x = 8'h00;
        raster_y = 9'h000;
`ifdef FES_COLECO_REGISTERED_VDP
        oss_scan_x = 8'h00;
        oss_scan_y = 9'h000;
        oss_launch_x = 8'h00;
        oss_launch_y = 9'h000;
        oss_launch_valid = 1'b0;
        oss_name_coord_x = 8'h00;
        oss_name_coord_y = 9'h000;
        oss_name_valid = 1'b0;
        oss_pattern_coord_x = 8'h00;
        oss_pattern_coord_y = 9'h000;
        oss_pattern_valid = 1'b0;
`endif
        for (init_index = 0; init_index < 8; init_index = init_index + 1)
            vdp_reg[init_index] = 8'h00;
    end

    wire bus_write = cpu_ce && !cpu_iorq_n && !cpu_wr_n;
    wire read_active = !cpu_iorq_n && !cpu_rd_n;
    wire bus_read = cpu_ce && read_active && !read_seen;
    wire control_port = cpu_a == 8'hbf;
    wire data_port = cpu_a == 8'hbe;
    wire [13:0] cpu_vram_addr = (bus_write && data_port) ? vram_addr : prefetch_addr;
    wire [13:0] name_base = {vdp_reg[2][3:0], 10'b0};
    wire [13:0] color_base = {vdp_reg[3], 6'b0};
    wire [13:0] pattern_base = {vdp_reg[4][2:0], 11'b0};

`ifdef FES_COLECO_REGISTERED_VDP
    wire [31:0] oss_name_address_w = {18'b0, name_base} +
                                     ({27'b0, oss_launch_y[7:3]} << 5) +
                                     {27'b0, oss_launch_x[7:3]};
    wire [31:0] oss_pattern_address_w = {18'b0, pattern_base} +
                                        ({24'b0, vram_name_read} << 3) +
                                        {29'b0, oss_name_coord_y[2:0]};
    wire [31:0] oss_color_address_w = {18'b0, color_base} +
                                      {24'b0, vram_name_read};

    localparam [3:0] SPRITE_IDLE         = 4'd0;
    localparam [3:0] SPRITE_CLEAR        = 4'd1;
    localparam [3:0] SPRITE_ATTR_REQ     = 4'd2;
    localparam [3:0] SPRITE_ATTR_WAIT    = 4'd3;
    localparam [3:0] SPRITE_PATTERN_REQ  = 4'd4;
    localparam [3:0] SPRITE_PATTERN_WAIT = 4'd5;
    localparam [3:0] SPRITE_RENDER_READ  = 4'd6;
    localparam [3:0] SPRITE_RENDER_WRITE = 4'd7;
    localparam [3:0] SPRITE_FINISH       = 4'd8;

    wire [13:0] sprite_sat_base = {vdp_reg[5][6:0], 7'b0};
    wire [13:0] sprite_pattern_base = {vdp_reg[6][2:0], 11'b0};
    wire sprite_display_line_valid = oss_pattern_valid &&
                                     (sprite_display_y == oss_pattern_coord_y[7:0]);
    wire [3:0] sprite_display_line_data = sprite_display_bank ?
                                          sprite_line_display_b_read :
                                          sprite_line_display_a_read;
    wire [1:0] sprite_display_pixel = (sprite_display_line_valid &&
                                       sprite_display_line_data[3]) ?
                                       sprite_display_line_data[1:0] : 2'd0;
    wire [8:0] sprite_height_w = (vdp_reg[1][1] ? 9'd16 : 9'd8) <<
                                  (vdp_reg[1][0] ? 1 : 0);
    // TMS9918 treats E1..FF as signed negative Y positions; E0 and below
    // remain unsigned, with D0 reserved as the SAT terminator.
    wire signed [9:0] sprite_top_w = (sprite_attr_y >= 8'he1) ?
                                      $signed({2'b11, sprite_attr_y}) + 10'sd1 :
                                      $signed({2'b00, sprite_attr_y}) + 10'sd1;
    wire signed [10:0] sprite_line_delta_w =
        $signed({3'b000, sprite_eval_target_y}) - sprite_top_w;
    wire sprite_attr_visible_w = (sprite_line_delta_w >= 0) &&
                                 (sprite_line_delta_w < sprite_height_w);
    wire [4:0] sprite_source_row_w =
        sprite_line_delta_w >>> (vdp_reg[1][0] ? 1 : 0);
    wire [4:0] sprite_render_last_col_w = vdp_reg[1][1] ? 5'd15 : 5'd7;
    wire       sprite_render_last_rep_w = vdp_reg[1][0];
    wire signed [10:0] sprite_render_pixel_x_w =
        $signed(sprite_pixel_x(sprite_attr_x, sprite_attr_color[7],
                               sprite_render_col, sprite_render_rep,
                               vdp_reg[1][0]));
    wire [7:0] sprite_render_address_w = sprite_render_pixel_x_w[7:0];
    wire       sprite_render_in_range_w =
        (sprite_render_col <= sprite_render_last_col_w) &&
        (sprite_render_rep <= sprite_render_last_rep_w) &&
        (sprite_render_pixel_x_w >= 0) &&
        (sprite_render_pixel_x_w < 256);
    wire       sprite_render_pattern_bit_w =
        (sprite_render_col < 5'd8) ?
        sprite_pattern_left[7 - sprite_render_col] :
        sprite_pattern_right[15 - sprite_render_col];
    wire [3:0] sprite_render_line_data = sprite_build_bank ?
                                         sprite_line_render_b_read :
                                         sprite_line_render_a_read;
    wire       sprite_render_occupied_w = sprite_render_line_data[2];
    wire       sprite_render_existing_pixel_w = sprite_render_line_data[3];
    wire [1:0] sprite_render_pixel_value_w =
        (sprite_attr_color[3:0] == 4'h1) ? 2'd1 : 2'd2;
    wire [3:0] sprite_render_write_data_w =
        (!sprite_render_existing_pixel_w && (sprite_attr_color[3:0] != 4'h0)) ?
        {1'b1, 1'b1, sprite_render_pixel_value_w} :
        {sprite_render_line_data[3], 1'b1, sprite_render_line_data[1:0]};
    wire [7:0] sprite_line_address_a_w =
        (sprite_eval_state == SPRITE_CLEAR) ? sprite_clear_address :
        sprite_render_address_w;
    wire sprite_line_clear_w = sprite_eval_state == SPRITE_CLEAR;
    wire sprite_line_render_write_w =
        (sprite_eval_state == SPRITE_RENDER_WRITE) &&
        sprite_render_in_range_w && sprite_render_pattern_bit_w &&
        (!sprite_render_existing_pixel_w || !sprite_render_occupied_w);
    wire sprite_line_write_w = sprite_line_clear_w || sprite_line_render_write_w;

    // Port A is read during SPRITE_RENDER_READ and written during the
    // following SPRITE_RENDER_WRITE phase. Port B remains the raster read, so
    // the old metadata and the visible pixel stay in one coherent entry.
    coleco_video_dpram #(
        .DATAWIDTH(4),
        .ADDRWIDTH(8),
        .NUMWORDS(256)
    ) sprite_pixel_ram_a (
        .clock_a(clk),
        .address_a(sprite_line_address_a_w),
        .data_a(sprite_line_clear_w ? 4'h0 : sprite_render_write_data_w),
        .wren_a(sprite_line_write_w && !sprite_build_bank),
        .q_a(sprite_line_render_a_read),
        .clock_b(clk),
        .address_b(oss_name_coord_x),
        .data_b(4'h0),
        .wren_b(1'b0),
        .q_b(sprite_line_display_a_read)
    );

    coleco_video_dpram #(
        .DATAWIDTH(4),
        .ADDRWIDTH(8),
        .NUMWORDS(256)
    ) sprite_pixel_ram_b (
        .clock_a(clk),
        .address_a(sprite_line_address_a_w),
        .data_a(sprite_line_clear_w ? 4'h0 : sprite_render_write_data_w),
        .wren_a(sprite_line_write_w && sprite_build_bank),
        .q_a(sprite_line_render_b_read),
        .clock_b(clk),
        .address_b(oss_name_coord_x),
        .data_b(4'h0),
        .wren_b(1'b0),
        .q_b(sprite_line_display_b_read)
    );

    // One registered read port is shared by the SAT and pattern-row walker.
    // Each request state is followed by a wait state because both OSS M10K
    // and Quartus altsyncram return the addressed byte on the next edge.
    always @* begin
        sprite_vram_address = 14'h0000;
        if (sprite_eval_state == SPRITE_ATTR_REQ)
            sprite_vram_address = sprite_sat_base +
                                  {sprite_eval_index, 2'b00} + sprite_attr_byte;
        else if (sprite_eval_state == SPRITE_PATTERN_REQ) begin
            if (vdp_reg[1][1])
                sprite_vram_address = sprite_pattern_base +
                                      {sprite_attr_pattern[7:2], 5'b00000} +
                                      (sprite_pattern_byte ? 14'd16 : 14'd0) +
                                      {9'b0, sprite_source_row};
            else
                sprite_vram_address = sprite_pattern_base +
                                      {sprite_attr_pattern, 3'b000} +
                                      sprite_source_row[2:0];
        end
    end

    // Each copy has one CPU/data port and one raster port. Broadcast writes
    // preserve identical contents while allowing the three independent
    // Graphics I lookups and the serial sprite walker to remain explicit
    // dual-port memories.
    coleco_dpram #(
        .ADDRWIDTH(14),
        .NUMWORDS(16384)
    ) vram_name_block (
        .clock(clk),
        .address_a(cpu_vram_addr),
        .data_a(cpu_din),
        .wren_a(bus_write && data_port),
        .q_a(vram_cpu_read),
        .address_b(oss_name_address_w[13:0]),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(vram_name_read)
    );

    coleco_dpram #(
        .ADDRWIDTH(14),
        .NUMWORDS(16384)
    ) vram_pattern_block (
        .clock(clk),
        .address_a(cpu_vram_addr),
        .data_a(cpu_din),
        .wren_a(bus_write && data_port),
        .q_a(vram_cpu_read_pattern),
        .address_b(oss_pattern_address_w[13:0]),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(vram_pattern_read)
    );

    coleco_dpram #(
        .ADDRWIDTH(14),
        .NUMWORDS(16384)
    ) vram_color_block (
        .clock(clk),
        .address_a(cpu_vram_addr),
        .data_a(cpu_din),
        .wren_a(bus_write && data_port),
        .q_a(vram_cpu_read_color),
        .address_b(oss_color_address_w[13:0]),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(vram_color_read)
    );

    coleco_dpram #(
        .ADDRWIDTH(14),
        .NUMWORDS(16384)
    ) vram_sprite_block (
        .clock(clk),
        .address_a(cpu_vram_addr),
        .data_a(cpu_din),
        .wren_a(bus_write && data_port),
        .q_a(vram_cpu_read_sprite),
        .address_b(sprite_vram_address),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(vram_sprite_read)
    );
`endif

`ifdef FES_COLECO_REGISTERED_VDP
    // Build the next scanline in a bank not currently being displayed. The
    // serial walker is deliberately faster than the ~4K system clocks in a
    // logical line: four SAT bytes and up to two pattern bytes per sprite fit
    // comfortably while keeping VRAM access in one explicit M10K port.
    always @(posedge clk) begin
        if (reset) begin
            sprite_ready_y <= 8'hff;
            sprite_ready_bank <= 1'b0;
            sprite_ready_collision <= 1'b0;
            sprite_ready_overflow <= 1'b0;
            sprite_ready_fifth_index <= 5'h00;
            sprite_eval_state <= SPRITE_IDLE;
            sprite_eval_index <= 5'h00;
            sprite_attr_byte <= 3'h00;
            sprite_attr_y <= 8'h00;
            sprite_attr_x <= 8'h00;
            sprite_attr_pattern <= 8'h00;
            sprite_attr_color <= 8'h00;
            sprite_eval_target_y <= 8'h00;
            sprite_source_row <= 5'h00;
            sprite_visible_count <= 6'h00;
            sprite_build_collision <= 1'b0;
            sprite_build_overflow <= 1'b0;
            sprite_build_fifth_index <= 5'h00;
            sprite_pattern_byte <= 2'h00;
            sprite_pattern_left <= 8'h00;
            sprite_pattern_right <= 8'h00;
            sprite_render_col <= 5'h00;
            sprite_render_rep <= 1'b0;
            sprite_clear_address <= 8'h00;
        end else begin
            case (sprite_eval_state)
                SPRITE_IDLE: begin
                    // Prime line zero after reset, then stay one line ahead.
                    if (!sprite_pending_valid &&
                        ((sprite_ready_y == 8'hff && oss_scan_y == 9'd0) ||
                        (sprite_ready_y == sprite_display_y &&
                         oss_scan_y < 9'd191 &&
                         sprite_ready_y != oss_scan_y[7:0] + 8'd1) ||
                        (sprite_ready_y == sprite_display_y &&
                         oss_scan_y == 9'd261 && sprite_ready_y != 8'h00))) begin
                        if (sprite_ready_y == 8'hff)
                            sprite_eval_target_y <= 8'h00;
                        else if (sprite_display_y == 8'd191)
                            sprite_eval_target_y <= 8'h00;
                        else
                            sprite_eval_target_y <= sprite_display_y + 8'd1;
                        sprite_eval_index <= 5'h00;
                        sprite_attr_byte <= 3'h00;
                        sprite_visible_count <= 6'h00;
                        sprite_build_collision <= 1'b0;
                        sprite_build_overflow <= 1'b0;
                        sprite_build_fifth_index <= 5'h00;
                        sprite_render_col <= 5'h00;
                        sprite_render_rep <= 1'b0;
                        sprite_clear_address <= 8'h00;
                        sprite_eval_state <= SPRITE_CLEAR;
                    end
                end

                SPRITE_CLEAR: begin
                    if (sprite_clear_address == 8'hff) begin
                        sprite_eval_state <= SPRITE_ATTR_REQ;
                    end else begin
                        sprite_clear_address <= sprite_clear_address + 1'b1;
                    end
                end

                SPRITE_ATTR_REQ: begin
                    sprite_eval_state <= SPRITE_ATTR_WAIT;
                end

                SPRITE_ATTR_WAIT: begin
                    case (sprite_attr_byte)
                        3'd0: sprite_attr_y <= vram_sprite_read;
                        3'd1: sprite_attr_x <= vram_sprite_read;
                        3'd2: sprite_attr_pattern <= vram_sprite_read;
                        default: begin
                            sprite_attr_color <= vram_sprite_read;
                            if (sprite_attr_y == 8'hd0) begin
                                sprite_ready_y <= sprite_eval_target_y;
                                sprite_ready_bank <= sprite_build_bank;
                                sprite_ready_collision <= sprite_build_collision;
                                sprite_ready_overflow <= sprite_build_overflow;
                                sprite_ready_fifth_index <= sprite_build_fifth_index;
                                sprite_eval_state <= SPRITE_IDLE;
                            end else if (sprite_attr_visible_w) begin
                                if (sprite_visible_count >= 6'd4) begin
                                    if (!sprite_build_overflow) begin
                                        sprite_build_overflow <= 1'b1;
                                        sprite_build_fifth_index <= sprite_eval_index;
                                    end
                                    if (sprite_eval_index == 5'd31) begin
                                        sprite_ready_y <= sprite_eval_target_y;
                                        sprite_ready_bank <= sprite_build_bank;
                                        sprite_ready_collision <= sprite_build_collision;
                                        sprite_ready_overflow <= 1'b1;
                                        sprite_ready_fifth_index <= sprite_build_overflow ?
                                                                    sprite_build_fifth_index : sprite_eval_index;
                                        sprite_eval_state <= SPRITE_IDLE;
                                    end else begin
                                        sprite_eval_index <= sprite_eval_index + 1'b1;
                                        sprite_attr_byte <= 3'h00;
                                        sprite_eval_state <= SPRITE_ATTR_REQ;
                                    end
                                end else begin
                                    sprite_visible_count <= sprite_visible_count + 1'b1;
                                    // Color zero is visually transparent but
                                    // still participates in sprite collision
                                    // and the four-sprites-per-line count.
                                    sprite_source_row <= sprite_source_row_w;
                                    sprite_pattern_byte <= 2'h00;
                                    sprite_eval_state <= SPRITE_PATTERN_REQ;
                                end
                            end else if (sprite_eval_index == 5'd31) begin
                                sprite_ready_y <= sprite_eval_target_y;
                                sprite_ready_bank <= sprite_build_bank;
                                sprite_ready_collision <= sprite_build_collision;
                                sprite_ready_overflow <= sprite_build_overflow;
                                sprite_ready_fifth_index <= sprite_build_fifth_index;
                                sprite_eval_state <= SPRITE_IDLE;
                            end else begin
                                sprite_eval_index <= sprite_eval_index + 1'b1;
                                sprite_attr_byte <= 3'h00;
                                sprite_eval_state <= SPRITE_ATTR_REQ;
                            end
                        end
                    endcase
                    if (sprite_attr_byte != 3'd3) begin
                        sprite_attr_byte <= sprite_attr_byte + 1'b1;
                        sprite_eval_state <= SPRITE_ATTR_REQ;
                    end
                end

                SPRITE_PATTERN_REQ: begin
                    sprite_eval_state <= SPRITE_PATTERN_WAIT;
                end

                SPRITE_PATTERN_WAIT: begin
                    if (sprite_pattern_byte == 2'h0) begin
                        sprite_pattern_left <= vram_sprite_read;
                        if (vdp_reg[1][1]) begin
                            sprite_pattern_byte <= 2'h1;
                            sprite_eval_state <= SPRITE_PATTERN_REQ;
                        end else begin
                            sprite_render_col <= 5'h00;
                            sprite_render_rep <= 1'b0;
                            sprite_eval_state <= SPRITE_RENDER_READ;
                        end
                    end else begin
                        sprite_pattern_right <= vram_sprite_read;
                        sprite_render_col <= 5'h00;
                        sprite_render_rep <= 1'b0;
                        sprite_eval_state <= SPRITE_RENDER_READ;
                    end
                end

                SPRITE_RENDER_READ: begin
                    // Port A returns the old line entry on the following edge.
                    // Hold the source pixel coordinates for the paired write
                    // phase so collision and priority decisions use the same
                    // entry that was read.
                    sprite_eval_state <= SPRITE_RENDER_WRITE;
                end

                SPRITE_RENDER_WRITE: begin
                    if (sprite_render_in_range_w &&
                        sprite_render_pattern_bit_w) begin
                        if (sprite_render_occupied_w)
                            sprite_build_collision <= 1'b1;
                    end

                    if (sprite_render_rep == sprite_render_last_rep_w) begin
                        sprite_render_rep <= 1'b0;
                        if (sprite_render_col == sprite_render_last_col_w) begin
                            if (sprite_eval_index == 5'd31) begin
                                sprite_eval_state <= SPRITE_FINISH;
                            end else begin
                                sprite_eval_index <= sprite_eval_index + 1'b1;
                                sprite_attr_byte <= 3'h00;
                                sprite_eval_state <= SPRITE_ATTR_REQ;
                            end
                        end else begin
                            sprite_render_col <= sprite_render_col + 1'b1;
                        end
                    end else begin
                        sprite_render_rep <= sprite_render_rep + 1'b1;
                    end
                end

                SPRITE_FINISH: begin
                    sprite_ready_y <= sprite_eval_target_y;
                    sprite_ready_bank <= sprite_build_bank;
                    sprite_ready_collision <= sprite_build_collision;
                    sprite_ready_overflow <= sprite_build_overflow;
                    sprite_ready_fifth_index <= sprite_build_fifth_index;
                    sprite_eval_state <= SPRITE_IDLE;
                end

                default: sprite_eval_state <= SPRITE_IDLE;
            endcase
        end
    end
`endif

    always @(posedge clk) begin
        if (reset) begin
            control_first <= 8'h00;
            control_latch <= 1'b0;
            vram_addr <= 14'h0000;
            vram_read_q <= 8'hff;
            prefetch_addr <= 14'h0000;
            prefetch_pending <= 2'b00;
            read_seen <= 1'b0;
            read_result <= 8'hff;
            status_vblank <= 1'b0;
            status_collision <= 1'b0;
            status_overflow <= 1'b0;
            status_fifth_index <= 5'h00;
`ifdef FES_COLECO_REGISTERED_VDP
            sprite_display_bank <= 1'b0;
            sprite_build_bank <= 1'b1;
            sprite_display_y <= 8'hff;
            sprite_pending_valid <= 1'b0;
            sprite_pending_bank <= 1'b0;
            sprite_pending_y <= 8'h00;
            sprite_pending_collision <= 1'b0;
            sprite_pending_overflow <= 1'b0;
            sprite_pending_fifth_index <= 5'h00;
            oss_scan_x <= 8'h00;
            oss_scan_y <= 9'h000;
            oss_launch_x <= 8'h00;
            oss_launch_y <= 9'h000;
            oss_launch_valid <= 1'b0;
            oss_name_coord_x <= 8'h00;
            oss_name_coord_y <= 9'h000;
            oss_name_valid <= 1'b0;
            oss_pattern_coord_x <= 8'h00;
            oss_pattern_coord_y <= 9'h000;
            oss_pattern_valid <= 1'b0;
`else
            raster_x <= 8'h00;
            raster_y <= 9'h000;
`endif
            for (init_index = 0; init_index < 8; init_index = init_index + 1)
                vdp_reg[init_index] <= 8'h00;
        end else begin
            // One side effect per held CPU IN, with a stable pre-side-effect
            // return byte until RD/IORQ deasserts (TV80 samples it later).
            if (!read_active)
                read_seen <= 1'b0;
            else if (bus_read) begin
                read_seen <= 1'b1;
                read_result <= data_port ? vram_read_q :
                               control_port ? {status_vblank, status_overflow,
                                               status_collision, status_fifth_index} : 8'hff;
            end

            // Both actual FPGA RAM lanes have registered addresses. Launch
            // the address, allow the RAM edge, then collect its returned byte.
            // This is read-ahead state, independent of the next CPU IN timing.
            prefetch_pending <= {1'b0, prefetch_pending[1]};
            if (prefetch_pending[0]) begin
`ifdef FES_COLECO_REGISTERED_VDP
                vram_read_q <= vram_cpu_read;
`else
                vram_read_q <= vram[prefetch_addr];
`endif
            end
            if (bus_write && control_port) begin
                if (!control_latch) begin
                    control_first <= cpu_din;
                    control_latch <= 1'b1;
                end else begin
                    control_latch <= 1'b0;
                    if (cpu_din[7])
                        vdp_reg[cpu_din[2:0]] <= control_first;
                    else begin
                        prefetch_pending <= 2'b00;
                        vram_addr <= {cpu_din[5:0], control_first};
                        if (!cpu_din[6]) begin
                            prefetch_addr <= {cpu_din[5:0], control_first};
                            prefetch_pending <= 2'b10;
                            vram_addr <= {cpu_din[5:0], control_first} + 14'd1;
                        end
                    end
                end
            end

            if (bus_write && data_port) begin
`ifndef FES_COLECO_REGISTERED_VDP
                vram[vram_addr] <= cpu_din;
`endif
                vram_addr <= (vram_addr == VRAM_LAST) ? 14'h0000 :
                             vram_addr + 1'b1;
                control_latch <= 1'b0;
                vram_read_q <= cpu_din;
                prefetch_pending <= 2'b00;
            end

            if (bus_read && data_port) begin
                prefetch_addr <= vram_addr;
                prefetch_pending <= 2'b10;
                vram_addr <= (vram_addr == VRAM_LAST) ? 14'h0000 :
                             vram_addr + 1'b1;
                control_latch <= 1'b0;
            end

            if (bus_read && control_port) begin
                status_vblank <= 1'b0;
                status_collision <= 1'b0;
                status_overflow <= 1'b0;
                status_fifth_index <= 5'h00;
                control_latch <= 1'b0;
            end

`ifdef FES_COLECO_REGISTERED_VDP
            // Request publication from registered raster coordinates rather
            // than gating this compare with raster_ce.  raster_ce is launched
            // on the system-clock falling edge; keeping it out of this
            // request path avoids turning the sprite publication register into
            // a new half-cycle timing path.  The request remains held until
            // the registered lookup pipeline reaches the same line below.
            if (!sprite_pending_valid &&
                sprite_ready_y != 8'hff &&
                sprite_ready_y == oss_scan_y[7:0] &&
                sprite_display_y != oss_scan_y[7:0]) begin
                sprite_pending_valid <= 1'b1;
                sprite_pending_bank <= sprite_ready_bank;
                sprite_pending_y <= sprite_ready_y;
                sprite_pending_collision <= sprite_ready_collision;
                sprite_pending_overflow <= sprite_ready_overflow;
                sprite_pending_fifth_index <= sprite_ready_fifth_index;
            end
`endif

            if (raster_ce) begin
`ifdef FES_COLECO_REGISTERED_VDP
                oss_launch_x <= oss_scan_x;
                oss_launch_y <= oss_scan_y;
                oss_launch_valid <= 1'b1;
                if (oss_scan_x == 8'd255) begin
                    oss_scan_x <= 8'h00;
                    if (oss_scan_y == 9'd261)
                        oss_scan_y <= 9'h000;
                    else
                        oss_scan_y <= oss_scan_y + 1'b1;
                end else begin
                    oss_scan_x <= oss_scan_x + 1'b1;
                end

                // Set VBlank once per frame, after the final active line.
                if (oss_scan_x == 8'd255 && oss_scan_y == 9'd191)
                    status_vblank <= 1'b1;

                // Advance the two-cycle registered raster lookup pipeline.
                oss_name_coord_x <= oss_launch_x;
                oss_name_coord_y <= oss_launch_y;
                oss_name_valid <= oss_launch_valid;
                oss_pattern_coord_x <= oss_name_coord_x;
                oss_pattern_coord_y <= oss_name_coord_y;
                oss_pattern_valid <= oss_name_valid;
`else
                if (!raster_blank) begin
                    if (sprite_collision_comb)
                        status_collision <= 1'b1;
                    if (sprite_overflow_comb &&
                        (!status_vblank || (bus_read && control_port))) begin
                        status_overflow <= 1'b1;
                        if (!status_overflow || (bus_read && control_port))
                            status_fifth_index <= sprite_fifth_index_comb;
                    end
                end
                if (raster_x == 8'd255) begin
                    raster_x <= 8'h00;
                    if (raster_y == 9'd261)
                        raster_y <= 9'h000;
                    else
                        raster_y <= raster_y + 1'b1;
                end else begin
                    raster_x <= raster_x + 1'b1;
                end

                // Set VBlank once per frame, after the final active line.
                if (raster_x == 8'd255 && raster_y == 9'd191)
                    status_vblank <= 1'b1;
`endif
            end

`ifdef FES_COLECO_REGISTERED_VDP
            if (!raster_ce) begin
                // Keep the lookup pipeline aligned with a held raster launch
                // while the machine's slower VDP enable is inactive.
                oss_name_coord_x <= oss_launch_x;
                oss_name_coord_y <= oss_launch_y;
                oss_name_valid <= oss_launch_valid;
                oss_pattern_coord_x <= oss_name_coord_x;
                oss_pattern_coord_y <= oss_name_coord_y;
                oss_pattern_valid <= oss_name_valid;
                if (sprite_pending_valid &&
                    sprite_pending_y == oss_pattern_coord_y[7:0]) begin
                    sprite_display_bank <= sprite_pending_bank;
                    sprite_build_bank <= ~sprite_pending_bank;
                    sprite_display_y <= sprite_pending_y;
                    sprite_pending_valid <= 1'b0;
                    if (sprite_pending_collision)
                        status_collision <= 1'b1;
                    if (sprite_pending_overflow &&
                        (!status_vblank || (bus_read && control_port))) begin
                        status_overflow <= 1'b1;
                        if (!status_overflow || (bus_read && control_port))
                            status_fifth_index <= sprite_pending_fifth_index;
                    end
                end
            end
`endif
        end
    end

    integer name_index;
    integer pattern_index;
    integer color_index;
    reg [7:0] tile_name;
    reg [7:0] pattern_byte;
    reg [7:0] color_byte;
    reg       pattern_bit;

`ifndef FES_COLECO_REGISTERED_VDP
    reg [1:0] sprite_pixel_comb;
    reg       sprite_occupied_comb;
    reg       sprite_collision_comb;
    reg       sprite_overflow_comb;
    reg [4:0] sprite_fifth_index_comb;
    integer sprite_scan_index_comb;
    integer sprite_visible_count_comb;
    integer sprite_scan_y_comb;
    integer sprite_scan_x_comb;
    integer sprite_scan_pattern_comb;
    integer sprite_scan_color_comb;
    integer sprite_scan_top_comb;
    integer sprite_scan_height_comb;
    integer sprite_scan_row_comb;
    integer sprite_scan_base_x_comb;
    integer sprite_scan_scale_comb;
    integer sprite_scan_width_comb;
    integer sprite_scan_column_comb;
    integer sprite_scan_repeat_comb;
    integer sprite_scan_pixel_x_comb;
    reg [7:0] sprite_scan_pattern_left_comb;
    reg [7:0] sprite_scan_pattern_right_comb;

    // The default simulation lane can inspect the complete VRAM array in one
    // combinational expression. It is an executable oracle for the registered
    // line walker below; synthesis never uses this path.
    always @* begin
        sprite_pixel_comb = 2'd0;
        sprite_occupied_comb = 1'b0;
        sprite_collision_comb = 1'b0;
        sprite_overflow_comb = 1'b0;
        sprite_fifth_index_comb = 5'h00;
        sprite_visible_count_comb = 0;
        sprite_scan_y_comb = raster_y;
        sprite_scan_x_comb = raster_x;
        sprite_scan_top_comb = 0;
        sprite_scan_pattern_comb = 0;
        sprite_scan_color_comb = 0;
        sprite_scan_row_comb = 0;
        sprite_scan_base_x_comb = 0;
        sprite_scan_pixel_x_comb = 0;
        sprite_scan_height_comb = vdp_reg[1][1] ? 16 : 8;
        sprite_scan_scale_comb = vdp_reg[1][0] ? 2 : 1;
        sprite_scan_width_comb = sprite_scan_height_comb;
        sprite_scan_pattern_left_comb = 8'h00;
        sprite_scan_pattern_right_comb = 8'h00;
        if (sprite_scan_y_comb < 192) begin
            for (sprite_scan_index_comb = 0;
                 sprite_scan_index_comb < 32;
                 sprite_scan_index_comb = sprite_scan_index_comb + 1) begin
                sprite_scan_y_comb = vram[({vdp_reg[5][6:0], 7'b0} +
                                           (sprite_scan_index_comb * 4)) & 14'h3fff];
                if (sprite_scan_y_comb == 8'hd0) begin
                    sprite_scan_index_comb = 32;
                end else begin
                    if (sprite_scan_y_comb >= 8'he1)
                        sprite_scan_top_comb = sprite_scan_y_comb - 255;
                    else
                        sprite_scan_top_comb = sprite_scan_y_comb + 1;
                    // Keep the negative-Y case in non-negative arithmetic.  An
                    // integer temporary becomes an unsigned Verilator signal in
                    // this combinational loop, so comparing raster_y directly to
                    // a negative top would make F9..FF disappear from line zero.
                    if (((sprite_scan_y_comb >= 8'he1) &&
                         (sprite_scan_y_comb +
                          sprite_scan_height_comb * sprite_scan_scale_comb > 255) &&
                         (raster_y < sprite_scan_y_comb +
                                    sprite_scan_height_comb * sprite_scan_scale_comb - 255)) ||
                        ((sprite_scan_y_comb < 8'he1) &&
                         (raster_y >= sprite_scan_y_comb + 1) &&
                         (raster_y < sprite_scan_y_comb + 1 +
                                    sprite_scan_height_comb * sprite_scan_scale_comb))) begin
                        if (sprite_visible_count_comb < 4) begin
                            sprite_visible_count_comb = sprite_visible_count_comb + 1;
                            sprite_scan_x_comb = vram[({vdp_reg[5][6:0], 7'b0} +
                                                      (sprite_scan_index_comb * 4) + 1) & 14'h3fff];
                            sprite_scan_pattern_comb = vram[({vdp_reg[5][6:0], 7'b0} +
                                                             (sprite_scan_index_comb * 4) + 2) & 14'h3fff];
                            sprite_scan_color_comb = vram[({vdp_reg[5][6:0], 7'b0} +
                                                           (sprite_scan_index_comb * 4) + 3) & 14'h3fff];
                            if (sprite_scan_y_comb >= 8'he1)
                                sprite_scan_row_comb =
                                    (raster_y + 255 - sprite_scan_y_comb) /
                                    sprite_scan_scale_comb;
                            else
                                sprite_scan_row_comb =
                                    (raster_y - sprite_scan_y_comb - 1) /
                                    sprite_scan_scale_comb;
                            if (vdp_reg[1][1]) begin
                                sprite_scan_pattern_left_comb = vram[
                                    ({vdp_reg[6][2:0], 11'b0} +
                                     ((sprite_scan_pattern_comb & 252) * 8) +
                                     sprite_scan_row_comb) & 14'h3fff];
                                sprite_scan_pattern_right_comb = vram[
                                    ({vdp_reg[6][2:0], 11'b0} +
                                     ((sprite_scan_pattern_comb & 252) * 8) +
                                     16 + sprite_scan_row_comb) & 14'h3fff];
                            end else begin
                                sprite_scan_pattern_left_comb = vram[
                                    ({vdp_reg[6][2:0], 11'b0} +
                                     (sprite_scan_pattern_comb * 8) +
                                     sprite_scan_row_comb) & 14'h3fff];
                                sprite_scan_pattern_right_comb = 8'h00;
                            end
                            sprite_scan_base_x_comb = (sprite_scan_color_comb & 128) ?
                                                      sprite_scan_x_comb - 32 :
                                                      sprite_scan_x_comb;
                            for (sprite_scan_column_comb = 0;
                                 sprite_scan_column_comb < sprite_scan_width_comb;
                                 sprite_scan_column_comb = sprite_scan_column_comb + 1) begin
                                if ((sprite_scan_column_comb < 8) ?
                                    sprite_scan_pattern_left_comb[7 - sprite_scan_column_comb] :
                                    sprite_scan_pattern_right_comb[15 - sprite_scan_column_comb]) begin
                                    for (sprite_scan_repeat_comb = 0;
                                         sprite_scan_repeat_comb < sprite_scan_scale_comb;
                                         sprite_scan_repeat_comb = sprite_scan_repeat_comb + 1) begin
                                        sprite_scan_pixel_x_comb = sprite_scan_base_x_comb +
                                                                   sprite_scan_column_comb *
                                                                   sprite_scan_scale_comb +
                                                                   sprite_scan_repeat_comb;
                                        if (sprite_scan_pixel_x_comb >= 0 &&
                                            sprite_scan_pixel_x_comb < 256 &&
                                            sprite_scan_pixel_x_comb == raster_x) begin
                                            if (sprite_occupied_comb)
                                                sprite_collision_comb = 1'b1;
                                            if (!sprite_occupied_comb)
                                                sprite_occupied_comb = 1'b1;
                                            if (sprite_pixel_comb == 0 &&
                                                (sprite_scan_color_comb & 15) != 0)
                                                sprite_pixel_comb =
                                                    (sprite_scan_color_comb & 15) == 1 ? 2'd1 : 2'd2;
                                        end
                                    end
                                end
                            end
                        end else if (!sprite_overflow_comb) begin
                            sprite_overflow_comb = 1'b1;
                            sprite_fifth_index_comb = sprite_scan_index_comb[4:0];
                        end
                    end
                end
            end
        end
    end

    // The default simulation path keeps the simple combinational fetch. The
    // Quartus and OSS paths use an explicit registered-memory pipeline.
    always @* begin
        raster_blank = raster_y >= 9'd192;
        raster_pixel = 2'd0;
        name_index = 0;
        pattern_index = 0;
        color_index = 0;
        tile_name = 8'h00;
        pattern_byte = 8'h00;
        color_byte = 8'h00;
        pattern_bit = 1'b0;

        if (!raster_blank) begin
            name_index = ({18'b0, name_base} +
                          ({27'b0, raster_y[7:3]} << 5) +
                          {27'b0, raster_x[7:3]}) & 32'h00003fff;
            tile_name = vram[name_index];
            pattern_index = ({18'b0, pattern_base} +
                             ({24'b0, tile_name} << 3) +
                             {29'b0, raster_y[2:0]}) & 32'h00003fff;
            color_index = ({18'b0, color_base} + {24'b0, tile_name}) &
                          32'h00003fff;
            pattern_byte = vram[pattern_index];
            color_byte = vram[color_index];
            pattern_bit = pattern_byte[7 - raster_x[2:0]];
            if (pattern_bit)
                raster_pixel = (color_byte[7:4] == 4'h0) ? 2'd1 : 2'd2;
            if (sprite_pixel_comb != 0)
                raster_pixel = sprite_pixel_comb;
        end
    end
`else
    // Mistral's M10K mapping requires registered reads. The name lookup is
    // followed by pattern/color lookups, so the output coordinate is delayed
    // with the data and remains aligned for the video shell.
    always @* begin
        raster_x = oss_pattern_coord_x;
        raster_y = oss_pattern_coord_y;
        raster_blank = !oss_pattern_valid || oss_pattern_coord_y >= 9'd192;
        raster_pixel = 2'd0;
        if (oss_pattern_valid && !raster_blank &&
            vram_pattern_read[7 - oss_pattern_coord_x[2:0]]) begin
            raster_pixel = (vram_color_read[7:4] == 4'h0) ? 2'd1 : 2'd2;
        end
        if (oss_pattern_valid && !raster_blank && sprite_display_pixel != 0)
            raster_pixel = sprite_display_pixel;
    end
`endif

    always @* begin
        cpu_dout = 8'hff;
        if (read_active) begin
            if (read_seen)
                cpu_dout = read_result;
            else if (data_port)
                cpu_dout = vram_read_q;
            else if (control_port)
                cpu_dout = {status_vblank, status_overflow,
                            status_collision, status_fifth_index};
        end
    end
endmodule
