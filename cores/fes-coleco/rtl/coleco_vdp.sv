// SPDX-License-Identifier: GPL-2.0-or-later
// Bounded TMS9918-compatible video path for the FES ColecoVision slice.
//
// The first bringup implements Graphics I name/pattern/color tables, the
// control/data ports, a 16 KiB VRAM aperture, and the VBlank status bit.  The
// raster is deliberately exposed in the logical 256x192 domain; the video
// shell owns the 720p timing and scaling.

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
    output reg        status_collision
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
    // Quartus 17.0 and the OSS mapper cannot keep the CPU port plus the three
    // raster reads on one inferred memory. Three coherent read copies keep
    // each lookup on an explicit dual-port M10K shape; CPU writes are
    // broadcast to all copies.
    wire [7:0] vram_cpu_read;
    wire [7:0] vram_cpu_read_pattern;
    wire [7:0] vram_cpu_read_color;
    wire [7:0] vram_name_read;
    wire [7:0] vram_pattern_read;
    wire [7:0] vram_color_read;
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
`endif
    reg [7:0] vdp_reg [0:7];
    reg [7:0] control_first;
    reg       control_latch;
    reg [13:0] vram_addr;
    reg [7:0] vram_read_q;
    reg       status_vblank;

    integer init_index;
    initial begin
        control_first = 8'h00;
        control_latch = 1'b0;
        vram_addr = 14'h0000;
        vram_read_q = 8'hff;
        status_vblank = 1'b0;
        status_collision = 1'b0;
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
    wire bus_read = cpu_ce && !cpu_iorq_n && !cpu_rd_n;
    wire control_port = cpu_a == 8'hbf;
    wire data_port = cpu_a == 8'hbe;
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

    // Each copy has one CPU/data port and one raster port. Broadcast writes
    // preserve identical contents while allowing the three independent
    // Graphics I lookups to remain explicit dual-port memories.
    coleco_dpram #(
        .ADDRWIDTH(14),
        .NUMWORDS(16384)
    ) vram_name_block (
        .clock(clk),
        .address_a(vram_addr),
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
        .address_a(vram_addr),
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
        .address_a(vram_addr),
        .data_a(cpu_din),
        .wren_a(bus_write && data_port),
        .q_a(vram_cpu_read_color),
        .address_b(oss_color_address_w[13:0]),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(vram_color_read)
    );
`endif

    always @(posedge clk) begin
        if (reset) begin
            control_first <= 8'h00;
            control_latch <= 1'b0;
            vram_addr <= 14'h0000;
            vram_read_q <= 8'hff;
            status_vblank <= 1'b0;
            status_collision <= 1'b0;
`ifdef FES_COLECO_REGISTERED_VDP
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
            if (bus_write && control_port) begin
                if (!control_latch) begin
                    control_first <= cpu_din;
                    control_latch <= 1'b1;
                end else begin
                    control_latch <= 1'b0;
                    if (cpu_din[7])
                        vdp_reg[cpu_din[2:0]] <= control_first;
                    else
                        vram_addr <= {cpu_din[5:0], control_first};
                end
            end

            if (bus_write && data_port) begin
`ifndef FES_COLECO_REGISTERED_VDP
                vram[vram_addr] <= cpu_din;
`endif
                vram_addr <= (vram_addr == VRAM_LAST) ? 14'h0000 :
                             vram_addr + 1'b1;
                control_latch <= 1'b0;
            end

            if (bus_read && data_port) begin
`ifndef FES_COLECO_REGISTERED_VDP
                vram_read_q <= vram[vram_addr];
`endif
                vram_addr <= (vram_addr == VRAM_LAST) ? 14'h0000 :
                             vram_addr + 1'b1;
                control_latch <= 1'b0;
            end

            if (bus_read && control_port) begin
                status_vblank <= 1'b0;
                status_collision <= 1'b0;
                control_latch <= 1'b0;
            end

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
    end
`endif

    always @* begin
        cpu_dout = 8'hff;
        if (bus_read) begin
            if (data_port)
`ifdef FES_COLECO_REGISTERED_VDP
                cpu_dout = vram_cpu_read;
`else
                cpu_dout = vram_read_q;
`endif
            else if (control_port)
                cpu_dout = {status_vblank, status_collision, 6'b0};
        end
    end
endmodule
