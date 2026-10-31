// SPDX-License-Identifier: MIT
// pc386 machine without the CPU (docs/pc386-machine-design.md, phase M2): the
// AT core, the VGA with its capture writer, the 720p presenter and the
// display plane. The CPU's I/O, INTA, DMA and MMIO ports and DDR ports 1 and
// 2 are the boundary; the shell adds the CPU, fes_hps_ddr (port 0 for guest
// RAM, all three ports in clk) and the mailbox.
//
//   port 1  VRAM: VGA planar writes and reads, scanout line prefetch
//   port 2  plane reads (pc386_plane_bridge), presenter reads, capture writes,
//           the shell's reads and single beats (aux)
//
// port2_reset resets the port 2 arbiter alone, so the shell's aux master can
// use the port while the shell holds the rest of the machine in reset: the
// disk's CRC pass runs while execution is held.
// video is the plane's output in pixel_clk, ready for HDMI. The plane reports
// the picture rectangle (160, 0, 960, 720).
module pc386_machine #(
    parameter integer ISA_N = 1, ISA_D = 12,
    parameter integer PIT_N = 21, PIT_D = 880,
    parameter integer RTC_N = 256, RTC_D = 781250,
    parameter integer US_N = 1, US_D = 100,
    parameter integer DMA_N = 1, DMA_D = 24,
    parameter integer DOT0_N = 1007, DOT0_D = 4000,
    parameter integer DOT1_N = 14161, DOT1_D = 50000,
    parameter integer ATA_RESET_CLOCKS = 1000
) (
    input wire clk, reset, port2_reset,
    input wire pixel_clk, pixel_locked,
    // CPU I/O and interrupt acknowledge
    input wire io_valid, io_write,
    input wire [15:0] io_port,
    input wire [1:0] io_size,
    input wire [31:0] io_wdata,
    input wire inta_valid,
    output wire io_busy, io_done, inta_done,
    output wire [31:0] io_rdata,
    output wire [7:0] inta_vector,
    output wire intr, a20m,
    input wire cpu_shutdown,
    output wire flush_request,
    input wire flush_done,
    output wire cpu_reset,
    // CPU MMIO for A0000h-BFFFFh
    input wire mem_valid, mem_write,
    input wire [16:2] mem_address,
    input wire [3:0] mem_byteenable,
    input wire [31:0] mem_wdata,
    output wire mem_ready, mem_rvalid,
    output wire [31:0] mem_rdata,
    // CPU DMA port
    output wire dma_valid, dma_write,
    output wire [23:0] dma_address,
    output wire [7:0] dma_wdata,
    input wire dma_done,
    input wire [7:0] dma_rdata,
    input wire [3:1] dma_drq,
    output wire dma_ack_valid, dma_ack_write, dma_ack_tc,
    output wire [1:0] dma_ack_channel,
    output wire [7:0] dma_ack_wdata,
    input wire dma_ack_done,
    input wire [7:0] dma_ack_rdata,
    input wire [15:0] device_irq,
    output wire speaker, nmi_mask,
    // Sound-device port; the shell places pc386_audio beside this machine.
    output wire sound_access, sound_write, sound_adlib,
    output wire [3:0] sound_address,
    output wire [7:0] sound_wdata,
    input wire [7:0] sound_rdata,
    output wire post_valid,
    output wire [7:0] post_code,
    output wire debug_valid,
    output wire [15:0] debug_port,
    output wire [7:0] debug_byte,
    input wire key_valid,
    input wire [7:0] key_code,
    output wire key_ready,
    input wire mouse_valid,
    input wire signed [7:0] mouse_dx, mouse_dy,
    input wire [1:0] mouse_buttons,
    input wire [15:0] base_kib, extended_kib, above16m_64k,
    input wire [7:0] sb_base_high, sb_irq, sb_dma,
    output wire cmos_busy,
    input wire wall_valid,
    input wire [15:0] wall_year,
    input wire [7:0] wall_month, wall_day, wall_hour, wall_minute, wall_second, wall_weekday,
    input wire disk_present,
    input wire [31:0] disk_sectors,
    input wire [15:0] disk_cylinders,
    input wire [4:0] disk_heads,
    input wire [5:0] disk_spt,
    output wire disk_valid, disk_write,
    output wire [23:0] disk_address,
    output wire [63:0] disk_wdata,
    input wire disk_ready, disk_rvalid,
    input wire [63:0] disk_rdata,
    input wire [6:0] dirty_index,
    output wire [15:0] dirty_word,
    input wire disk_frozen,
    input wire disk_dirty_clear,
    output wire disk_dirty_clearing,
    output wire disk_writer_busy,
    output wire disk_fenced,
    output wire disk_written,
    output wire record_write,
    output wire [6:0] record_index,
    output wire [7:0] record_byte,
    output wire record_load,
    output wire [5:0] record_pair,
    input wire [15:0] record_word,
    // DDR port 1
    output wire [28:0] p1_address,
    output wire [7:0] p1_burstcount,
    output wire p1_read, p1_write,
    output wire [63:0] p1_writedata,
    output wire [7:0] p1_byteenable,
    input wire p1_waitrequest,
    input wire [63:0] p1_readdata,
    input wire p1_readdatavalid,
    // DDR port 2
    output wire [28:0] p2_address,
    output wire [7:0] p2_burstcount,
    output wire p2_read, p2_write,
    output wire [63:0] p2_writedata,
    output wire [7:0] p2_byteenable,
    input wire p2_waitrequest,
    input wire [63:0] p2_readdata,
    input wire p2_readdatavalid,
    // The shell's reads and single-beat writes on port 2 (read data on p2_readdata)
    input wire [28:0] aux_address,
    input wire [7:0] aux_burstcount,
    input wire aux_read, aux_write,
    input wire [63:0] aux_writedata,
    input wire [7:0] aux_byteenable,
    output wire aux_waitrequest,
    output wire aux_readdatavalid,
    // Display plane control (fes.video.session-display) and output
    input wire display_request,
    input wire [6:0] display_opcode,
    input wire [7:0] display_index,
    input wire [15:0] display_argument,
    output wire display_response_valid, display_response_error,
    output wire [15:0] display_response_data,
    output wire display_quiesced,
    output wire [27:0] video,
    // Trace and counters
    output wire frame_start, frame_done, start_latched,
    output wire [15:0] frame_count,
    output wire [1:0] frame_buffer,
    output wire [9:0] frame_width, frame_height,
    output wire [31:0] scanout_underflows, capture_drops, present_underflows, plane_underflows, frames_shown,
    output wire [27:0] presenter_video
);
    wire vga_valid, vga_write, vga_done;
    wire [15:0] vga_port;
    wire [7:0] vga_wdata, vga_rdata;
    pc386_at_core #(
        .ISA_N(ISA_N), .ISA_D(ISA_D), .PIT_N(PIT_N), .PIT_D(PIT_D), .RTC_N(RTC_N), .RTC_D(RTC_D),
        .US_N(US_N), .US_D(US_D), .DMA_N(DMA_N), .DMA_D(DMA_D), .ATA_RESET_CLOCKS(ATA_RESET_CLOCKS)
    ) at (
        .clk(clk), .reset(reset),
        .io_valid(io_valid), .io_write(io_write), .io_port(io_port), .io_size(io_size), .io_wdata(io_wdata),
        .inta_valid(inta_valid), .io_busy(io_busy), .io_done(io_done), .inta_done(inta_done),
        .io_rdata(io_rdata), .inta_vector(inta_vector), .intr(intr), .a20m(a20m),
        .cpu_shutdown(cpu_shutdown), .flush_request(flush_request), .flush_done(flush_done), .cpu_reset(cpu_reset),
        .dma_valid(dma_valid), .dma_write(dma_write), .dma_address(dma_address), .dma_wdata(dma_wdata),
        .dma_done(dma_done), .dma_rdata(dma_rdata), .dma_drq(dma_drq),
        .dma_ack_valid(dma_ack_valid), .dma_ack_write(dma_ack_write), .dma_ack_tc(dma_ack_tc),
        .dma_ack_channel(dma_ack_channel), .dma_ack_wdata(dma_ack_wdata), .dma_ack_done(dma_ack_done),
        .dma_ack_rdata(dma_ack_rdata),
        .vga_valid(vga_valid), .vga_write(vga_write), .vga_port(vga_port), .vga_wdata(vga_wdata),
        .vga_done(vga_done), .vga_rdata(vga_rdata),
        .sound_access(sound_access), .sound_write(sound_write), .sound_adlib(sound_adlib),
        .sound_address(sound_address), .sound_wdata(sound_wdata), .sound_rdata(sound_rdata),
        .device_irq(device_irq), .speaker(speaker), .nmi_mask(nmi_mask),
        .post_valid(post_valid), .post_code(post_code),
        .debug_valid(debug_valid), .debug_port(debug_port), .debug_byte(debug_byte),
        .key_valid(key_valid), .key_code(key_code), .key_ready(key_ready),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy), .mouse_buttons(mouse_buttons),
        .base_kib(base_kib), .extended_kib(extended_kib), .above16m_64k(above16m_64k),
        .sb_base_high(sb_base_high), .sb_irq(sb_irq), .sb_dma(sb_dma), .cmos_busy(cmos_busy),
        .wall_valid(wall_valid), .wall_year(wall_year), .wall_month(wall_month), .wall_day(wall_day),
        .wall_hour(wall_hour), .wall_minute(wall_minute), .wall_second(wall_second), .wall_weekday(wall_weekday),
        .disk_present(disk_present), .disk_sectors(disk_sectors), .disk_cylinders(disk_cylinders),
        .disk_heads(disk_heads), .disk_spt(disk_spt),
        .disk_valid(disk_valid), .disk_write(disk_write), .disk_address(disk_address), .disk_wdata(disk_wdata),
        .disk_ready(disk_ready), .disk_rvalid(disk_rvalid), .disk_rdata(disk_rdata),
        .dirty_index(dirty_index), .dirty_word(dirty_word),
        .disk_frozen(disk_frozen), .disk_dirty_clear(disk_dirty_clear), .disk_dirty_clearing(disk_dirty_clearing),
        .disk_writer_busy(disk_writer_busy), .disk_fenced(disk_fenced), .disk_written(disk_written),
        .record_write(record_write), .record_index(record_index), .record_byte(record_byte),
        .record_load(record_load), .record_pair(record_pair), .record_word(record_word)
    );

    wire [28:0] capture_address;
    wire [7:0] capture_burstcount;
    wire capture_write, capture_waitrequest;
    wire [63:0] capture_writedata;
    wire [1:0] present_buffer;
    wire [15:0] frame_sequence;
    pc386_vga #(.DOT0_N(DOT0_N), .DOT0_D(DOT0_D), .DOT1_N(DOT1_N), .DOT1_D(DOT1_D)) vga (
        .clk(clk), .reset(reset),
        .io_valid(vga_valid), .io_write(vga_write), .io_port(vga_port), .io_wdata(vga_wdata),
        .io_done(vga_done), .io_rdata(vga_rdata),
        .mem_valid(mem_valid), .mem_write(mem_write), .mem_address(mem_address),
        .mem_byteenable(mem_byteenable), .mem_wdata(mem_wdata), .mem_ready(mem_ready),
        .mem_rvalid(mem_rvalid), .mem_rdata(mem_rdata),
        .p1_address(p1_address), .p1_burstcount(p1_burstcount), .p1_read(p1_read), .p1_write(p1_write),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_waitrequest(p1_waitrequest),
        .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid),
        .cap_address(capture_address), .cap_burstcount(capture_burstcount), .cap_write(capture_write),
        .cap_writedata(capture_writedata), .cap_waitrequest(capture_waitrequest),
        .frame_buffer(frame_buffer), .frame_width(frame_width), .frame_height(frame_height),
        .frame_sequence(frame_sequence), .present_buffer(present_buffer),
        .frame_start(frame_start), .frame_done(frame_done), .start_latched(start_latched),
        .frame_count(frame_count), .scanout_underflows(scanout_underflows), .capture_drops(capture_drops)
    );

    (* async_reg = "true" *) reg [1:0] pixel_reset_sync;
    always @(posedge pixel_clk) pixel_reset_sync <= {pixel_reset_sync[0], reset || !pixel_locked};
    wire pixel_reset = pixel_reset_sync[1];
    initial pixel_reset_sync = 2'b11;
    // The 720p timing follows the pixel PLL alone, not the machine.
    (* async_reg = "true" *) reg [1:0] timing_reset_sync;
    always @(posedge pixel_clk) timing_reset_sync <= {timing_reset_sync[0], !pixel_locked};
    wire timing_reset = timing_reset_sync[1];
    initial timing_reset_sync = 2'b11;

    wire [28:0] present_address;
    wire [7:0] present_burstcount;
    wire present_read, present_waitrequest, present_readdatavalid;
    pc386_presenter presenter (
        .clk(clk), .reset(reset),
        .frame_buffer(frame_buffer), .frame_width(frame_width), .frame_height(frame_height),
        .frame_sequence(frame_sequence), .present_buffer(present_buffer),
        .ddr_address(present_address), .ddr_burstcount(present_burstcount), .ddr_read(present_read),
        .ddr_waitrequest(present_waitrequest), .ddr_readdata(p2_readdata),
        .ddr_readdatavalid(present_readdatavalid),
        .pixel_clk(pixel_clk), .pixel_reset(pixel_reset), .timing_reset(timing_reset),
        .video(presenter_video), .underflows(present_underflows), .frames_shown(frames_shown)
    );

    wire plane_hold, plane_ddr_reset, plane_ddr_drained, plane_read, plane_waitrequest, plane_readdatavalid;
    wire [27:0] plane_address;
    wire [7:0] plane_burstcount;
    wire [127:0] plane_readdata;
    wire [28:0] bridge_address;
    wire [7:0] bridge_burstcount;
    wire bridge_read, bridge_waitrequest, bridge_readdatavalid;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_display_plane #(
        .PICTURE_LEFT(16'd160), .PICTURE_TOP(16'd0), .PICTURE_WIDTH(16'd960), .PICTURE_HEIGHT(16'd720)
    ) display (
        .clk_sys(clk), .pixel_clk(pixel_clk), .reset_hold(!pixel_locked), .exec_reset(reset),
        .request(display_request), .opcode(display_opcode), .index(display_index), .argument(display_argument),
        .response_valid(display_response_valid), .response_error(display_response_error),
        .response_data(display_response_data), .quiesced_sys(display_quiesced),
        .machine(presenter_video), .video(video), .ui_active(), .locked(), .displayed_sequence(),
        .underflows(plane_underflows), .faulted(),
        .ddr_hold(plane_hold), .ddr_reset(plane_ddr_reset), .ddr_drained(plane_ddr_drained),
        .ddr_address(plane_address), .ddr_burstcount(plane_burstcount), .ddr_read(plane_read),
        .ddr_waitrequest(plane_waitrequest), .ddr_readdata(plane_readdata), .ddr_readdatavalid(plane_readdatavalid)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    pc386_plane_bridge bridge (
        .clk(clk), .reset(reset), .pixel_clk(pixel_clk), .pixel_reset(pixel_reset),
        .ddr_hold(plane_hold), .ddr_reset(plane_ddr_reset), .ddr_drained(plane_ddr_drained),
        .plane_address(plane_address), .plane_burstcount(plane_burstcount), .plane_read(plane_read),
        .plane_waitrequest(plane_waitrequest), .plane_readdata(plane_readdata),
        .plane_readdatavalid(plane_readdatavalid),
        .port_address(bridge_address), .port_burstcount(bridge_burstcount), .port_read(bridge_read),
        .port_waitrequest(bridge_waitrequest), .port_readdata(p2_readdata), .port_readdatavalid(bridge_readdatavalid)
    );

    pc386_port2 port2 (
        .clk(clk), .reset(port2_reset),
        .plane_address(bridge_address), .plane_burstcount(bridge_burstcount), .plane_read(bridge_read),
        .plane_waitrequest(bridge_waitrequest), .plane_readdatavalid(bridge_readdatavalid),
        .present_address(present_address), .present_burstcount(present_burstcount), .present_read(present_read),
        .present_waitrequest(present_waitrequest), .present_readdatavalid(present_readdatavalid),
        .capture_address(capture_address), .capture_burstcount(capture_burstcount), .capture_write(capture_write),
        .capture_writedata(capture_writedata), .capture_waitrequest(capture_waitrequest),
        .aux_address(aux_address), .aux_burstcount(aux_burstcount), .aux_read(aux_read), .aux_write(aux_write),
        .aux_writedata(aux_writedata),
        .aux_byteenable(aux_byteenable), .aux_waitrequest(aux_waitrequest), .aux_readdatavalid(aux_readdatavalid),
        .address(p2_address), .burstcount(p2_burstcount), .read(p2_read), .write(p2_write),
        .writedata(p2_writedata), .byteenable(p2_byteenable), .waitrequest(p2_waitrequest),
        .readdatavalid(p2_readdatavalid)
    );
endmodule
