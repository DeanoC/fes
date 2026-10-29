// SPDX-License-Identifier: MIT
// pc386 M4 diagnostic machine (docs/pc386-machine-design.md, phase M4): the
// machine and its audio with pc386_bus_master in the CPU's place. The shell
// (top.sv) adds the PLLs, the fes.computer mailbox, fes_hps_ddr and the HDMI
// and I2S pins; everything here runs in clk except the plane's pixel side.
//
//   port 0  the bus master: command ring, guest RAM for DMA, ATA image
//   port 1  VRAM
//   port 2  plane, presenter, capture
//
// p0_reset..p2_reset are fes_hps_ddr's per-port execution resets. The bus
// master holds the machine in reset after each launch until a CONFIG command
// releases it. Its hold also fences ports 1 and 2 (pc386_port_fence), so a
// machine reset never cuts a burst there. Port 0 is the bus master's own.
// Every guest time base is a rational enable of clk; the producer sets each
// N/D pair from the sealed frequency.
module pc386_diag #(
    parameter integer ISA_N = 1, ISA_D = 12,
    parameter integer PIT_N = 21, PIT_D = 880,
    parameter integer RTC_N = 256, RTC_D = 781250,
    parameter integer US_N = 1, US_D = 100,
    parameter integer DMA_N = 1, DMA_D = 24,
    parameter integer DOT0_N = 1007, DOT0_D = 4000,
    parameter integer DOT1_N = 14161, DOT1_D = 50000,
    parameter integer OPL2_N = 7, OPL2_D = 14080,
    parameter integer AUDIO_N = 3, AUDIO_D = 6250,
    parameter integer ATA_RESET_CLOCKS = 1000,
    parameter integer POLL_CLOCKS = 255
) (
    input wire clk, pixel_clk, pixel_locked,
    input wire p0_reset, p1_reset, p2_reset,
    // Display plane control (fes.video.session-display) and output
    input wire display_request,
    input wire [6:0] display_opcode,
    input wire [7:0] display_index,
    input wire [15:0] display_argument,
    output wire display_response_valid, display_response_error,
    output wire [15:0] display_response_data,
    output wire display_quiesced,
    output wire [27:0] video,
    // Mixer output at 48 kHz
    output wire signed [15:0] sample,
    // DDR port 0
    output wire [27:0] p0_address,
    output wire [7:0] p0_burstcount,
    output wire p0_read, p0_write,
    output wire [127:0] p0_writedata,
    output wire [15:0] p0_byteenable,
    input wire p0_waitrequest,
    input wire [127:0] p0_readdata,
    input wire p0_readdatavalid,
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
    input wire p2_readdatavalid
);
    // One register drives the machine reset: the imported chips take it
    // asynchronously.
    wire machine_hold;
    reg reset = 1'b1;
    always @(posedge clk) reset <= machine_hold || p1_reset || p2_reset;

    // The machine's ports 1 and 2, fenced by the bus master's hold.
    wire [28:0] machine_p1_address, machine_p2_address;
    wire [7:0] machine_p1_burstcount, machine_p2_burstcount;
    wire machine_p1_read, machine_p1_write, machine_p1_waitrequest, machine_p1_readdatavalid;
    wire machine_p2_read, machine_p2_write, machine_p2_waitrequest, machine_p2_readdatavalid;
    wire [63:0] machine_p1_writedata, machine_p1_readdata, machine_p2_writedata, machine_p2_readdata;
    wire [7:0] machine_p1_byteenable, machine_p2_byteenable;
    pc386_port_fence p1_fence (
        .clk(clk), .reset(p1_reset), .hold(machine_hold),
        .address(machine_p1_address), .burstcount(machine_p1_burstcount), .read(machine_p1_read),
        .write(machine_p1_write), .writedata(machine_p1_writedata), .byteenable(machine_p1_byteenable),
        .waitrequest(machine_p1_waitrequest), .readdata(machine_p1_readdata), .readdatavalid(machine_p1_readdatavalid),
        .m_address(p1_address), .m_burstcount(p1_burstcount), .m_read(p1_read), .m_write(p1_write),
        .m_writedata(p1_writedata), .m_byteenable(p1_byteenable), .m_waitrequest(p1_waitrequest),
        .m_readdata(p1_readdata), .m_readdatavalid(p1_readdatavalid)
    );
    pc386_port_fence p2_fence (
        .clk(clk), .reset(p2_reset), .hold(machine_hold),
        .address(machine_p2_address), .burstcount(machine_p2_burstcount), .read(machine_p2_read),
        .write(machine_p2_write), .writedata(machine_p2_writedata), .byteenable(machine_p2_byteenable),
        .waitrequest(machine_p2_waitrequest), .readdata(machine_p2_readdata), .readdatavalid(machine_p2_readdatavalid),
        .m_address(p2_address), .m_burstcount(p2_burstcount), .m_read(p2_read), .m_write(p2_write),
        .m_writedata(p2_writedata), .m_byteenable(p2_byteenable), .m_waitrequest(p2_waitrequest),
        .m_readdata(p2_readdata), .m_readdatavalid(p2_readdatavalid)
    );

    wire io_valid, io_write, inta_valid, io_busy, io_done, inta_done, intr, a20m;
    wire [15:0] io_port;
    wire [1:0] io_size;
    wire [31:0] io_wdata, io_rdata;
    wire [7:0] inta_vector;
    wire flush_request, cpu_reset;
    wire mem_valid, mem_write, mem_ready, mem_rvalid;
    wire [16:2] mem_address;
    wire [3:0] mem_byteenable;
    wire [31:0] mem_wdata, mem_rdata;
    wire dma_valid, dma_write, dma_done;
    wire [23:0] dma_address;
    wire [7:0] dma_wdata, dma_rdata;
    wire dma_ack_valid, dma_ack_write, dma_ack_tc;
    wire [1:0] dma_ack_channel;
    wire [7:0] dma_ack_wdata;
    wire speaker, nmi_mask;
    wire sound_access, sound_write, sound_adlib;
    wire [3:0] sound_address;
    wire [7:0] sound_wdata, sound_rdata;
    wire post_valid, debug_valid;
    wire [7:0] post_code, debug_byte;
    wire [15:0] debug_port;
    wire key_valid, key_ready;
    wire [7:0] key_code;
    wire [15:0] base_kib, extended_kib, above16m_64k;
    wire [7:0] sb_base_high, sb_irq, sb_dma;
    wire cmos_busy;
    wire disk_present;
    wire [31:0] disk_sectors;
    wire [15:0] disk_cylinders;
    wire [4:0] disk_heads;
    wire [5:0] disk_spt;
    wire disk_valid, disk_write, disk_ready, disk_rvalid;
    wire [23:0] disk_address;
    wire [63:0] disk_wdata, disk_rdata;
    wire [6:0] dirty_index;
    wire [15:0] dirty_word;
    wire frame_start, frame_done, start_latched;
    wire [15:0] frame_count;
    wire [1:0] frame_buffer;
    wire [9:0] frame_width, frame_height;
    wire [31:0] scanout_underflows, capture_drops, present_underflows, plane_underflows, frames_shown;
    wire [27:0] presenter_video;

    // Channel 1 is the Sound Blaster's; 0, 2 and 3 have no device, so their
    // acknowledges complete with FFh, as on an empty ISA bus.
    wire drq1, dsp_done;
    wire [7:0] dsp_rdata;
    wire [15:0] sound_irq;
    wire channel1 = dma_ack_valid && dma_ack_channel == 2'd1;
    reg other_done;
    always @(posedge clk) other_done <= !reset && dma_ack_valid && !channel1 && !other_done;
    wire dma_ack_done = channel1 ? dsp_done : other_done;
    wire [7:0] dma_ack_rdata = channel1 ? dsp_rdata : 8'hff;

    pc386_machine #(
        .ISA_N(ISA_N), .ISA_D(ISA_D), .PIT_N(PIT_N), .PIT_D(PIT_D), .RTC_N(RTC_N), .RTC_D(RTC_D),
        .US_N(US_N), .US_D(US_D), .DMA_N(DMA_N), .DMA_D(DMA_D), .DOT0_N(DOT0_N), .DOT0_D(DOT0_D),
        .DOT1_N(DOT1_N), .DOT1_D(DOT1_D), .ATA_RESET_CLOCKS(ATA_RESET_CLOCKS)
    ) machine (
        .clk(clk), .reset(reset), .pixel_clk(pixel_clk), .pixel_locked(pixel_locked),
        .io_valid(io_valid), .io_write(io_write), .io_port(io_port), .io_size(io_size), .io_wdata(io_wdata),
        .inta_valid(inta_valid), .io_busy(io_busy), .io_done(io_done), .inta_done(inta_done),
        .io_rdata(io_rdata), .inta_vector(inta_vector), .intr(intr), .a20m(a20m),
        .cpu_shutdown(1'b0), .flush_request(flush_request), .flush_done(flush_request), .cpu_reset(cpu_reset),
        .mem_valid(mem_valid), .mem_write(mem_write), .mem_address(mem_address),
        .mem_byteenable(mem_byteenable), .mem_wdata(mem_wdata), .mem_ready(mem_ready),
        .mem_rvalid(mem_rvalid), .mem_rdata(mem_rdata),
        .dma_valid(dma_valid), .dma_write(dma_write), .dma_address(dma_address), .dma_wdata(dma_wdata),
        .dma_done(dma_done), .dma_rdata(dma_rdata), .dma_drq({2'b00, drq1}),
        .dma_ack_valid(dma_ack_valid), .dma_ack_write(dma_ack_write), .dma_ack_tc(dma_ack_tc),
        .dma_ack_channel(dma_ack_channel), .dma_ack_wdata(dma_ack_wdata), .dma_ack_done(dma_ack_done),
        .dma_ack_rdata(dma_ack_rdata), .device_irq(sound_irq), .speaker(speaker), .nmi_mask(nmi_mask),
        .sound_access(sound_access), .sound_write(sound_write), .sound_adlib(sound_adlib),
        .sound_address(sound_address), .sound_wdata(sound_wdata), .sound_rdata(sound_rdata),
        .post_valid(post_valid), .post_code(post_code),
        .debug_valid(debug_valid), .debug_port(debug_port), .debug_byte(debug_byte),
        .key_valid(key_valid), .key_code(key_code), .key_ready(key_ready),
        .mouse_valid(1'b0), .mouse_dx(8'sd0), .mouse_dy(8'sd0), .mouse_buttons(2'b00),
        .base_kib(base_kib), .extended_kib(extended_kib), .above16m_64k(above16m_64k),
        .sb_base_high(sb_base_high), .sb_irq(sb_irq), .sb_dma(sb_dma), .cmos_busy(cmos_busy),
        .wall_valid(1'b0), .wall_year(16'd0), .wall_month(8'd0), .wall_day(8'd0), .wall_hour(8'd0),
        .wall_minute(8'd0), .wall_second(8'd0), .wall_weekday(8'd0),
        .disk_present(disk_present), .disk_sectors(disk_sectors), .disk_cylinders(disk_cylinders),
        .disk_heads(disk_heads), .disk_spt(disk_spt),
        .disk_valid(disk_valid), .disk_write(disk_write), .disk_address(disk_address), .disk_wdata(disk_wdata),
        .disk_ready(disk_ready), .disk_rvalid(disk_rvalid), .disk_rdata(disk_rdata),
        .dirty_index(dirty_index), .dirty_word(dirty_word),
        .p1_address(machine_p1_address), .p1_burstcount(machine_p1_burstcount), .p1_read(machine_p1_read),
        .p1_write(machine_p1_write), .p1_writedata(machine_p1_writedata), .p1_byteenable(machine_p1_byteenable),
        .p1_waitrequest(machine_p1_waitrequest), .p1_readdata(machine_p1_readdata),
        .p1_readdatavalid(machine_p1_readdatavalid),
        .p2_address(machine_p2_address), .p2_burstcount(machine_p2_burstcount), .p2_read(machine_p2_read),
        .p2_write(machine_p2_write), .p2_writedata(machine_p2_writedata), .p2_byteenable(machine_p2_byteenable),
        .p2_waitrequest(machine_p2_waitrequest), .p2_readdata(machine_p2_readdata),
        .p2_readdatavalid(machine_p2_readdatavalid),
        .display_request(display_request), .display_opcode(display_opcode), .display_index(display_index),
        .display_argument(display_argument), .display_response_valid(display_response_valid),
        .display_response_error(display_response_error), .display_response_data(display_response_data),
        .display_quiesced(display_quiesced), .video(video),
        .frame_start(frame_start), .frame_done(frame_done), .start_latched(start_latched),
        .frame_count(frame_count), .frame_buffer(frame_buffer), .frame_width(frame_width),
        .frame_height(frame_height), .scanout_underflows(scanout_underflows), .capture_drops(capture_drops),
        .present_underflows(present_underflows), .plane_underflows(plane_underflows),
        .frames_shown(frames_shown), .presenter_video(presenter_video)
    );

    // The 48 kHz frame request is a rational enable of clk; fes_audio_output
    // carries the newest sample into the I2S clock.
    wire request, sample_valid, opl2_valid;
    wire signed [15:0] mixed, opl2_sample;
    wire [7:0] dsp_dac;
    pc386_rate #(.N(AUDIO_N), .D(AUDIO_D)) audio_rate (.clk(clk), .reset(reset), .tick(request));
    pc386_audio #(.US_N(US_N), .US_D(US_D), .OPL2_N(OPL2_N), .OPL2_D(OPL2_D)) audio (
        .clk(clk), .reset(reset),
        .access(sound_access), .write(sound_write), .adlib(sound_adlib), .address(sound_address),
        .wdata(sound_wdata), .rdata(sound_rdata), .sb_irq(sb_irq), .irq(sound_irq),
        .drq1(drq1), .dack_valid(channel1), .dack_write(dma_ack_write), .dack_wdata(dma_ack_wdata),
        .dack_done(dsp_done), .dack_rdata(dsp_rdata),
        .speaker(speaker), .request(request), .sample_valid(sample_valid), .sample(mixed),
        .opl2_valid(opl2_valid), .opl2_sample(opl2_sample), .dsp_dac(dsp_dac)
    );
    reg signed [15:0] held;
    always @(posedge clk)
        if (reset) held <= 0;
        else if (sample_valid) held <= mixed;
    assign sample = held;

    // What READ 0-15 return:
    //    0 capture drops         1 presenter underflows   2 plane underflows
    //    3 scanout underflows    4 720p frames shown      5 status (below)
    //    6 capture height:width  7 newest capture buffer [17:16], captured frames [15:0]
    //    8 dirty_word
    //    9 mixer sample         10 mixer samples         11 OPL2 samples
    //   12 8237 memory cycles   13 DSP DMA acknowledges  14 debug-port bytes
    //   15 CPU resets [23:16], last debug byte [7:0]
    // status: last POST code [15:8], CMOS busy, plane quiesced, NMI mask,
    // speaker, A20M, INTR [5:0].
    reg [31:0] samples, opl2_samples, dma_cycles, dsp_acks, debug_bytes;
    reg [7:0] cpu_resets, last_post, last_debug;
    reg reset_seen;
    always @(posedge clk)
        if (reset) begin
            samples <= 0; opl2_samples <= 0; dma_cycles <= 0; dsp_acks <= 0; debug_bytes <= 0;
            cpu_resets <= 0; last_post <= 0; last_debug <= 0; reset_seen <= 0;
        end else begin
            reset_seen <= cpu_reset;
            if (sample_valid) samples <= samples + 32'd1;
            if (opl2_valid) opl2_samples <= opl2_samples + 32'd1;
            if (dma_done) dma_cycles <= dma_cycles + 32'd1;
            if (channel1 && dsp_done) dsp_acks <= dsp_acks + 32'd1;
            if (debug_valid) begin debug_bytes <= debug_bytes + 32'd1; last_debug <= debug_byte; end
            if (post_valid) last_post <= post_code;
            if (cpu_reset && !reset_seen) cpu_resets <= cpu_resets + 8'd1;
        end
    wire [16*32-1:0] observe = {
        {8'd0, cpu_resets, 8'd0, last_debug}, debug_bytes, dsp_acks, dma_cycles,
        opl2_samples, samples, {{16{held[15]}}, held}, {16'd0, dirty_word},
        {14'd0, frame_buffer, frame_count}, {6'd0, frame_height, 6'd0, frame_width},
        {16'd0, last_post, 2'd0, cmos_busy, display_quiesced, nmi_mask, speaker, a20m, intr},
        frames_shown, scanout_underflows, plane_underflows, present_underflows, capture_drops
    };
    wire unused = ^{dma_ack_tc, start_latched, frame_start, frame_done, presenter_video,
                    debug_port, opl2_sample, dsp_dac, flush_request};

    pc386_bus_master #(.POLL_CLOCKS(POLL_CLOCKS)) master (
        .clk(clk), .reset(p0_reset), .machine_hold(machine_hold),
        .io_valid(io_valid), .io_write(io_write), .inta_valid(inta_valid), .io_port(io_port),
        .io_size(io_size), .io_wdata(io_wdata), .io_busy(io_busy), .io_done(io_done), .inta_done(inta_done),
        .io_rdata(io_rdata), .inta_vector(inta_vector), .intr(intr),
        .mem_valid(mem_valid), .mem_write(mem_write), .mem_address(mem_address),
        .mem_byteenable(mem_byteenable), .mem_wdata(mem_wdata), .mem_ready(mem_ready),
        .mem_rvalid(mem_rvalid), .mem_rdata(mem_rdata),
        .dma_valid(dma_valid), .dma_write(dma_write), .dma_address(dma_address), .dma_wdata(dma_wdata),
        .dma_done(dma_done), .dma_rdata(dma_rdata),
        .disk_valid(disk_valid), .disk_write(disk_write), .disk_address(disk_address), .disk_wdata(disk_wdata),
        .disk_ready(disk_ready), .disk_rvalid(disk_rvalid), .disk_rdata(disk_rdata),
        .key_valid(key_valid), .key_code(key_code), .key_ready(key_ready),
        .base_kib(base_kib), .extended_kib(extended_kib), .above16m_64k(above16m_64k),
        .sb_base_high(sb_base_high), .sb_irq(sb_irq), .sb_dma(sb_dma),
        .disk_present(disk_present), .disk_sectors(disk_sectors), .disk_cylinders(disk_cylinders),
        .disk_heads(disk_heads), .disk_spt(disk_spt), .dirty_index(dirty_index), .observe(observe),
        .p0_address(p0_address), .p0_burstcount(p0_burstcount), .p0_read(p0_read), .p0_write(p0_write),
        .p0_writedata(p0_writedata), .p0_byteenable(p0_byteenable), .p0_waitrequest(p0_waitrequest),
        .p0_readdata(p0_readdata), .p0_readdatavalid(p0_readdatavalid)
    );
endmodule
