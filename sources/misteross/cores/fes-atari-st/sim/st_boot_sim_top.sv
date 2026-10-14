// SPDX-License-Identifier: GPL-3.0-or-later
// Reusable 520ST system: storage and video/audio sockets remain external.
module st_boot_sim_top #(parameter integer MFP_WAIT_STATES = 0) (
    input wire clk_sys, reset,
    output wire rom_req,
    output wire [17:1] rom_addr,
    input wire [15:0] rom_rdata,
    input wire rom_ready,
    output wire ram_req,
    output wire [18:1] ram_addr,
    output wire [15:0] ram_wdata,
    output wire [1:0] ram_byte_enable,
    output wire ram_write,
    input wire [15:0] ram_rdata,
    input wire ram_ready,
    output wire exp_req,
    output wire [23:1] exp_addr,
    output wire [15:0] exp_wdata,
    output wire [1:0] exp_byte_enable,
    output wire exp_write,
    output wire [2:0] exp_fc,
    output wire exp_reset, exp_phi1, exp_phi2,
    input wire exp_ack, exp_berr,
    input wire exp_present,
    input wire [15:0] exp_rdata,
    input wire [2:0] exp_irq,
    output wire irq_ack,
    output wire [2:0] irq_level,
    input wire [143:0] keyboard,
    input wire [15:0] controller_buttons,
    input wire monochrome,
    input wire mouse_valid,
    input wire signed [15:0] mouse_dx, mouse_dy,
    input wire [1:0] mouse_buttons,
    output wire mouse_ready,
    output wire signed [15:0] audio_pcm,
    output wire audio_valid,
    input wire media_ready,
    input wire [31:0] media_size,
    output wire media_req,
    output wire [19:0] media_addr,
    input wire [7:0] media_data,
    input wire media_valid,
    output wire dma_req,
    output wire [23:0] dma_addr,
    output wire [15:0] dma_wdata,
    output wire [1:0] dma_byte_enable,
    input wire dma_ready,
    output wire [23:0] screen_base,
    output wire [1:0] resolution,
    output wire [143:0] palette,
    output wire [7:0] sync_mode,
    output wire [23:0] debug_addr,
    output wire debug_bus_error, debug_overlay, debug_halted,
    output wire [31:0] debug_pc,
    output wire [23:0] debug_fault_address,
    output wire [2:0] debug_fault_fc,
    output wire debug_fault_write,
    output wire [7:0] debug_fdc_status, debug_fdc_track, debug_fdc_sector, debug_fdc_head,
    output wire debug_io_req,
    output wire [7:0] debug_irq_vector,
    output wire debug_palette_write,
    output wire [23:0] debug_palette_address,
    output wire [15:0] debug_palette_data,
    output wire [11:0] debug_palette_color,
    output wire [1:0] debug_palette_lanes,
    output wire [8:0] debug_native_line,
    output wire [31:0] debug_horizontal_phase,
    output wire native_display,
    output wire [8:0] native_line,
    output wire capture_req,
    output wire [18:1] capture_addr,
    input wire capture_ready,
    input wire [15:0] capture_data,
    output wire capture_pixel,
    output wire [8:0] capture_x, capture_y, capture_rgb, capture_height,
    output wire [31:0] capture_frames, capture_underruns,
    output wire vblank, hblank
);
    wire [8:0] native_cycle;
    wire native_pixel_ce;
    st_system #(.MFP_WAIT_STATES(MFP_WAIT_STATES)) system (
        .cold_reset(reset), .media_frozen(1'b0),
        .media_write_req(), .media_write_addr(), .media_write_data(),
        .media_write_ready(1'b0), .media_write_busy(), .media_changed(),
        .dma_write(), .dma_rdata(16'd0), .*
    );
    wire [8:0] unused_capture_rgb, unused_capture_border;
    wire unused_capture_valid;
    wire [31:0] unused_capture_skipped;
    // This wrapper observes the production native capture with model RAM.
    // Its consumer clock is the system clock; it is not an HDMI/CDC oracle.
    st_native_low_video capture (
        .clk_sys(clk_sys), .clk_pixel(clk_sys), .reset_sys(reset), .reset_pixel(reset),
        .hold(reset), .native_vblank(vblank), .native_display(native_display), .native_line(native_line),
        .native_cycle(native_cycle), .native_pixel_ce(native_pixel_ce),
        .sync_mode(sync_mode), .screen_base(screen_base), .resolution(resolution), .palette(palette),
        .memory_req(capture_req), .memory_addr(capture_addr), .memory_ready(capture_ready), .memory_data(capture_data),
        .output_sof(vblank), .output_address(17'd0), .output_rgb(unused_capture_rgb),
        .output_raster_rgb(), .output_raster_border(), .output_pal(),
        .output_border_row(9'd0), .output_border_x(9'd0), .output_border_line_start(vblank), .output_border_ce(1'b0),
        .output_border(unused_capture_border), .output_valid(unused_capture_valid), .output_height(),
        .debug_frames(capture_frames), .debug_skipped(unused_capture_skipped), .debug_underruns(capture_underruns)
    );
    assign capture_pixel = capture.write_pixel;
    assign capture_x = capture.sample_x;
    assign capture_y = capture.row;
    assign capture_rgb = capture.sample_rgb;
    assign capture_height = capture.height[capture.write_bank];
    assign debug_fdc_status = system.io.floppy.fdc_status;
    assign debug_fdc_track = system.io.floppy.track_reg;
    assign debug_fdc_sector = system.io.floppy.sector_reg;
    assign debug_fdc_head = system.io.floppy.head_track;
    assign debug_fault_address = system.machine.address;
    assign debug_fault_fc = system.machine.function_code;
    assign debug_fault_write = system.machine.writing;
    // Observe the internal IO completion edge, including unchanged palette
    // writes. These ports exist only on this simulation wrapper.
    assign debug_palette_write = !reset && system.machine.state == 2'd1 &&
        system.machine.target == 3'd2 && system.machine.writing &&
        system.machine.palette_access && system.machine.io_completion && !system.machine.cpu_as_n &&
        system.machine.timeout_halves != 8'd128;
    assign debug_palette_address = system.machine.address;
    assign debug_palette_data = system.machine.write_data;
    assign debug_palette_color = system.machine.palette_wdata[11:0] & 12'h777;
    assign debug_palette_lanes = system.machine.lanes;
    assign debug_irq_vector = system.irq_vector;
    assign debug_io_req = system.bus_req;
    assign debug_native_line = system.io.native_line;
    assign debug_horizontal_phase = {23'd0, system.io.horizontal_cycle};
    // Simulation-only observability; no upstream CPU bytes are changed.
    assign debug_pc = {system.machine.cpu.cpu.excUnit.PcH,
                       system.machine.cpu.cpu.excUnit.PcL};
endmodule
