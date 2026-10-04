// SPDX-License-Identifier: GPL-2.0-or-later
// fes.riscv system: the original RV32I CPU, 32 KiB of M10K RAM holding the
// firmware, a 160x120 RGB332 framebuffer shown 6x inside the fixed 720p
// raster, and a small memory-mapped I/O block. Everything runs on the
// 74.25 MHz pixel clock.
//
//   0x0000_0000  32 KiB RAM (firmware image at 0, reset vector 0)
//   0x1000_0000  19,200-byte framebuffer, one RGB332 byte per pixel
//   0x2000_0000  I/O registers (word access):
//     +0x00 R  gamepad buttons        +0x04 R  frame counter
//     +0x08 R  mtime low              +0x0c R  mtime high
//     +0x10 RW mtimecmp low           +0x14 RW mtimecmp high
//     +0x18 RW vblank flag (set each frame; any write clears it)
//     +0x1c RW vblank interrupt enable (external interrupt)
//     +0x20 R  identification "RISC"  +0x24 RW software interrupt
//   anything else faults.
module fes_riscv_system #(
    parameter FIRMWARE_LANE0 = "cores/fes-riscv/firmware/firmware.lane0.hex",
    parameter FIRMWARE_LANE1 = "cores/fes-riscv/firmware/firmware.lane1.hex",
    parameter FIRMWARE_LANE2 = "cores/fes-riscv/firmware/firmware.lane2.hex",
    parameter FIRMWARE_LANE3 = "cores/fes-riscv/firmware/firmware.lane3.hex"
) (
    input  wire        pixel_clk,
    input  wire        exec_reset,
    input  wire [7:0]  buttons,
    output wire [23:0] hdmi_rgb,
    output wire        hdmi_de,
    output wire        hdmi_hs,
    output wire        hdmi_vs,
    output wire        cpu_step,      // observation
    output wire [31:0] cpu_pc
);
    localparam integer FB_WORDS = 160 * 120 / 4;
    localparam [31:0] IDENTIFICATION = 32'h5249_5343;   // "RISC"

    // Fixed raster. Only its timing is used; pixels come from the framebuffer.
    wire [9:0] playfield_x, playfield_y;
    wire playfield_active, raster_de, raster_hs, raster_vs, frame_tick;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_video_720p raster (
        .pixel_clk(pixel_clk), .game_red(8'd0), .game_green(8'd0), .game_blue(8'd0),
        .playfield_x(playfield_x), .playfield_y(playfield_y), .playfield_active(playfield_active),
        .red(), .green(), .blue(), .de(raster_de), .hsync(raster_hs), .vsync(raster_vs),
        .frame_tick(frame_tick)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    // CPU and bus.
    wire bus_valid, bus_instr;
    wire [31:0] bus_addr, bus_wdata;
    wire [3:0] bus_wstrb;
    reg ready_q;
    reg [1:0] sel_q;            // 0 RAM, 1 framebuffer, 2 I/O, 3 fault
    reg [31:0] mmio_q;
    reg [63:0] mtime, mtimecmp;
    reg [31:0] frame_count;
    reg vblank_flag, vblank_ie, soft_irq, timer_irq_q;
    wire [31:0] ram_q, fb_q, fb_video_q;
    wire [31:0] bus_rdata = sel_q == 2'd0 ? ram_q : sel_q == 2'd1 ? fb_q : mmio_q;
    wire bus_error = ready_q && sel_q == 2'd3;

    /* verilator lint_off PINCONNECTEMPTY */
    fes_rv32_cpu cpu (
        .clk(pixel_clk), .reset(exec_reset),
        .bus_valid(bus_valid), .bus_instr(bus_instr), .bus_addr(bus_addr),
        .bus_wdata(bus_wdata), .bus_wstrb(bus_wstrb), .bus_ready(ready_q),
        .bus_error(bus_error), .bus_rdata(bus_rdata),
        .irq_external(vblank_flag && vblank_ie), .irq_timer(timer_irq_q),
        .irq_software(soft_irq), .mtime(mtime),
        .step(cpu_step), .retired(), .step_pc(), .trap(), .trap_cause(),
        .wb_valid(), .wb_rd(), .wb_data(), .debug_pc(cpu_pc)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    // Every request takes two cycles: the address is decoded and the
    // memories read in the first, data and ready return in the second.
    wire issue = bus_valid && !ready_q;
    wire ram_sel = bus_addr[31:15] == 17'd0;
    wire fb_sel = bus_addr[31:15] == {4'h1, 13'd0} && bus_addr[14:2] < FB_WORDS[12:0];
    wire mmio_sel = bus_addr[31:6] == {4'h2, 22'd0};
    wire [3:0] ram_we = {4{issue && ram_sel}} & bus_wstrb;
    wire [3:0] fb_we = {4{issue && fb_sel}} & bus_wstrb;
    wire mmio_write = issue && mmio_sel && bus_wstrb != 4'd0;

    // Raster pipeline: address, memory read, lane select and colour expansion.
    wire [7:0] fb_x = playfield_x[8:1];
    wire [6:0] fb_y = playfield_y[7:1];
    reg [14:0] v_addr_q;
    reg [1:0] v_lane_q;
    reg [7:0] v_pixel_q;
    reg [2:0] v_active_d;
    reg [3:0] v_de_d, v_hs_d, v_vs_d;
    reg [23:0] v_rgb_q;
    wire [23:0] v_expanded = {v_pixel_q[7:5], v_pixel_q[7:5], v_pixel_q[7:6],
                              v_pixel_q[4:2], v_pixel_q[4:2], v_pixel_q[4:3],
                              {4{v_pixel_q[1:0]}}};

    genvar lane;
    generate
        for (lane = 0; lane < 4; lane = lane + 1) begin : lanes
            /* verilator lint_off PINCONNECTEMPTY */
            fes_riscv_lane_ram #(.DEPTH(8192), .ADDR_WIDTH(13),
                .INIT_FILE(lane == 0 ? FIRMWARE_LANE0 : lane == 1 ? FIRMWARE_LANE1 :
                           lane == 2 ? FIRMWARE_LANE2 : FIRMWARE_LANE3)) ram (
                .clk(pixel_clk), .a_addr(bus_addr[14:2]), .a_wdata(bus_wdata[8*lane +: 8]),
                .a_we(ram_we[lane]), .a_q(ram_q[8*lane +: 8]), .b_addr(13'd0), .b_q()
            );
            /* verilator lint_on PINCONNECTEMPTY */
            fes_riscv_lane_ram #(.DEPTH(FB_WORDS), .ADDR_WIDTH(13)) framebuffer (
                .clk(pixel_clk), .a_addr(bus_addr[14:2]), .a_wdata(bus_wdata[8*lane +: 8]),
                .a_we(fb_we[lane]), .a_q(fb_q[8*lane +: 8]), .b_addr(v_addr_q[14:2]),
                .b_q(fb_video_q[8*lane +: 8])
            );
        end
    endgenerate
    wire [7:0] v_video_byte = v_lane_q == 2'd0 ? fb_video_q[7:0] : v_lane_q == 2'd1 ? fb_video_q[15:8] :
                              v_lane_q == 2'd2 ? fb_video_q[23:16] : fb_video_q[31:24];

    always @(posedge pixel_clk) begin
        ready_q <= issue;
        if (issue) begin
            sel_q <= ram_sel ? 2'd0 : fb_sel ? 2'd1 : mmio_sel ? 2'd2 : 2'd3;
            case (bus_addr[5:2])
                4'd0: mmio_q <= {24'd0, buttons};
                4'd1: mmio_q <= frame_count;
                4'd2: mmio_q <= mtime[31:0];
                4'd3: mmio_q <= mtime[63:32];
                4'd4: mmio_q <= mtimecmp[31:0];
                4'd5: mmio_q <= mtimecmp[63:32];
                4'd6: mmio_q <= {31'd0, vblank_flag};
                4'd7: mmio_q <= {31'd0, vblank_ie};
                4'd8: mmio_q <= IDENTIFICATION;
                4'd9: mmio_q <= {31'd0, soft_irq};
                default: mmio_q <= 32'd0;
            endcase
        end
        mtime <= mtime + 64'd1;
        timer_irq_q <= mtime >= mtimecmp;
        if (frame_tick) begin
            frame_count <= frame_count + 32'd1;
            vblank_flag <= 1'b1;
        end
        if (mmio_write) begin
            case (bus_addr[5:2])
                4'd4: mtimecmp[31:0] <= bus_wdata;
                4'd5: mtimecmp[63:32] <= bus_wdata;
                4'd6: if (!frame_tick) vblank_flag <= 1'b0;
                4'd7: vblank_ie <= bus_wdata[0];
                4'd9: soft_irq <= bus_wdata[0];
                default: ;
            endcase
        end
        if (exec_reset) begin
            ready_q <= 1'b0;
            sel_q <= 2'd3;
            mtime <= 64'd0;
            mtimecmp <= {64{1'b1}};
            timer_irq_q <= 1'b0;
            frame_count <= 32'd0;
            vblank_flag <= 1'b0;
            vblank_ie <= 1'b0;
            soft_irq <= 1'b0;
        end

        // Raster: address (1), memory read (2), lane select (3) and colour
        // expansion (4) run four stages behind the shared raster counters;
        // the sync signals are delayed to match.
        v_addr_q <= {1'b0, fb_y, 7'd0} + {3'd0, fb_y, 5'd0} + {7'd0, fb_x};
        v_lane_q <= v_addr_q[1:0];
        v_pixel_q <= v_video_byte;
        v_active_d <= {v_active_d[1:0], playfield_active};
        v_de_d <= {v_de_d[2:0], raster_de};
        v_hs_d <= {v_hs_d[2:0], raster_hs};
        v_vs_d <= {v_vs_d[2:0], raster_vs};
        v_rgb_q <= v_active_d[2] ? v_expanded : 24'd0;
    end

    assign hdmi_rgb = v_rgb_q;
    assign hdmi_de = v_de_d[3];
    assign hdmi_hs = v_hs_d[3];
    assign hdmi_vs = v_vs_d[3];
endmodule
