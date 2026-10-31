// SPDX-License-Identifier: MIT
// pc386 system (docs/pc386-machine-design.md, phase M5 = CPU Phase 3): the
// machine and its audio with fes_i386_cpu, its L1s and bus interface on DDR
// port 0. The shell (top.sv) adds the PLLs, the fes.computer mailbox,
// fes_hps_ddr and the HDMI and I2S pins; everything here runs in clk except
// the plane's pixel side.
//
//   port 0  the CPU: guest RAM at window offset 0x04000000, and the firmware
//           ROM (pc386_rom) in front of it
//   port 1  VRAM
//   port 2  plane, presenter, capture; pc386_host_link's single beats: the
//           ATA image, its configuration, status and debug blocks
//
// p0_reset..p2_reset are fes_hps_ddr's per-port execution resets. The
// machine stays in reset until pc386_host_link has the HPS's configuration,
// and the CPU until the RTC has its wall clock and CMOS setup. After that
// the only reset is the CPU's own, from the AT core (8042, port 92h), which
// asserts FLUSH first: it holds the CPU at least 4 clocks and until its
// memory is idle, and the machine, its DDR ports and port 0's reads in
// flight carry on. Every guest time base is a rational enable of clk; the
// producer sets each N/D pair from the sealed frequency.
module pc386_system #(
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
    parameter ROM_HEX = "build/firmware/pc386/pc386-rom.hex"
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
    wire machine_hold, cpu_hold;
    reg reset = 1'b1;
    always @(posedge clk) reset <= machine_hold || p0_reset || p1_reset || p2_reset;

    wire io_valid, io_write, inta_valid, io_busy, io_done, inta_done, intr, a20m;
    wire [15:0] io_port;
    wire [1:0] io_size;
    wire [31:0] io_wdata, io_rdata;
    wire [7:0] inta_vector;
    wire flush_request, flush_done, machine_cpu_reset;
    wire mem_valid, mem_write, mem_ready, mem_rvalid;
    wire [fes_i386_pkg::PA-3:0] cpu_mmio_address;
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
    wire key_ready;
    wire [15:0] base_kib, extended_kib, above16m_64k;
    wire [7:0] sb_base_high, sb_irq, sb_dma;
    wire cmos_busy;
    wire wall_valid;
    wire [15:0] wall_year;
    wire [7:0] wall_month, wall_day, wall_hour, wall_minute, wall_second, wall_weekday;
    wire disk_present, queue16;
    wire [31:0] disk_sectors;
    wire [15:0] disk_cylinders;
    wire [4:0] disk_heads;
    wire [5:0] disk_spt;
    wire disk_valid, disk_write, disk_ready, disk_rvalid;
    wire [23:0] disk_address;
    wire [63:0] disk_wdata, disk_rdata;
    wire [15:0] dirty_word;
    wire frame_start, frame_done, start_latched;
    wire [15:0] frame_count;
    wire [1:0] frame_buffer;
    wire [9:0] frame_width, frame_height;
    wire [31:0] scanout_underflows, capture_drops, present_underflows, plane_underflows, frames_shown;
    wire [27:0] presenter_video;
    wire [28:0] aux_address;
    wire aux_read, aux_write, aux_waitrequest, aux_readdatavalid;
    wire [63:0] aux_writedata;
    wire [7:0] aux_byteenable;

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
        .clk(clk), .reset(reset), .port2_reset(p2_reset), .pixel_clk(pixel_clk), .pixel_locked(pixel_locked),
        .io_valid(io_valid), .io_write(io_write), .io_port(io_port), .io_size(io_size), .io_wdata(io_wdata),
        .inta_valid(inta_valid), .io_busy(io_busy), .io_done(io_done), .inta_done(inta_done),
        .io_rdata(io_rdata), .inta_vector(inta_vector), .intr(intr), .a20m(a20m),
        .cpu_shutdown(1'b0), .flush_request(flush_request), .flush_done(flush_done),
        .cpu_reset(machine_cpu_reset),
        .mem_valid(mem_valid), .mem_write(mem_write), .mem_address(cpu_mmio_address[14:0]),
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
        .key_valid(1'b0), .key_code(8'd0), .key_ready(key_ready),
        .mouse_valid(1'b0), .mouse_dx(8'sd0), .mouse_dy(8'sd0), .mouse_buttons(2'b00),
        .base_kib(base_kib), .extended_kib(extended_kib), .above16m_64k(above16m_64k),
        .sb_base_high(sb_base_high), .sb_irq(sb_irq), .sb_dma(sb_dma), .cmos_busy(cmos_busy),
        .wall_valid(wall_valid), .wall_year(wall_year), .wall_month(wall_month), .wall_day(wall_day),
        .wall_hour(wall_hour), .wall_minute(wall_minute), .wall_second(wall_second), .wall_weekday(wall_weekday),
        .disk_present(disk_present), .disk_sectors(disk_sectors), .disk_cylinders(disk_cylinders),
        .disk_heads(disk_heads), .disk_spt(disk_spt),
        .disk_valid(disk_valid), .disk_write(disk_write), .disk_address(disk_address), .disk_wdata(disk_wdata),
        .disk_ready(disk_ready), .disk_rvalid(disk_rvalid), .disk_rdata(disk_rdata),
        .dirty_index(7'd0), .dirty_word(dirty_word),
        .p1_address(p1_address), .p1_burstcount(p1_burstcount), .p1_read(p1_read), .p1_write(p1_write),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_waitrequest(p1_waitrequest),
        .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid),
        .p2_address(p2_address), .p2_burstcount(p2_burstcount), .p2_read(p2_read), .p2_write(p2_write),
        .p2_writedata(p2_writedata), .p2_byteenable(p2_byteenable), .p2_waitrequest(p2_waitrequest),
        .p2_readdata(p2_readdata), .p2_readdatavalid(p2_readdatavalid),
        .aux_address(aux_address), .aux_read(aux_read), .aux_write(aux_write), .aux_writedata(aux_writedata),
        .aux_byteenable(aux_byteenable), .aux_waitrequest(aux_waitrequest), .aux_readdatavalid(aux_readdatavalid),
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

    // ---- The CPU
    // It leaves reset once the machine has, the RTC has its wall clock and
    // its CMOS setup is done (started). The AT core's CPU reset pulse holds
    // it again for at least 4 clocks and until its memory is idle.
    wire cpu_mem_idle, rom_idle, cpu_halt, cpu_stopped, cpu_retire;
    reg started, cpu_cold, cpu_reset;
    reg [2:0] stretch;
    always @(posedge clk) begin
        cpu_cold <= reset;
        cpu_reset <= reset || !started || stretch != 3'd0;
        if (reset) begin
            started <= 0; stretch <= 0;
        end else begin
            if (!cpu_hold && !cmos_busy) started <= 1;
            if (machine_cpu_reset) stretch <= 3'd4;
            else if (stretch != 3'd0 && (stretch != 3'd1 || (cpu_mem_idle && rom_idle))) stretch <= stretch - 3'd1;
        end
    end

    // I/O and INTA: the CPU holds a request until it is acknowledged; the
    // machine takes one cycle at a time, a clock's io_valid or inta_valid
    // while it is not busy, and answers with io_done or inta_done. The
    // acknowledge, with IN's data cut to the access's size, follows a clock
    // later. A cycle the machine is running when the CPU resets completes
    // unacknowledged.
    wire cpu_io_req, cpu_io_write, cpu_inta_req;
    wire [15:0] cpu_io_port;
    wire [1:0] cpu_io_size;
    wire [31:0] cpu_io_wdata;
    reg io_issued, inta_issued, io_orphan, inta_orphan, io_ack, inta_ack;
    reg [31:0] io_result;
    reg [7:0] inta_result;
    wire idle_bus = !io_issued && !inta_issued && !io_ack && !inta_ack && !cpu_reset && !io_busy;
    assign io_valid = idle_bus && cpu_io_req;
    assign inta_valid = idle_bus && cpu_inta_req && !cpu_io_req;
    assign io_write = cpu_io_write;
    assign io_port = cpu_io_port;
    assign io_size = cpu_io_size;
    assign io_wdata = cpu_io_wdata;
    always @(posedge clk) begin
        io_ack <= 0;
        inta_ack <= 0;
        if (reset) begin
            io_issued <= 0; inta_issued <= 0; io_orphan <= 0; inta_orphan <= 0; io_result <= 0; inta_result <= 0;
        end else begin
            if (io_valid) io_issued <= 1;
            if (inta_valid) inta_issued <= 1;
            if (cpu_reset) begin io_orphan <= io_issued; inta_orphan <= inta_issued; end
            if (io_issued && io_done) begin
                io_issued <= 0; io_orphan <= 0;
                io_ack <= !io_orphan && !cpu_reset;
                io_result <= cpu_io_size == 2'd0 ? {24'd0, io_rdata[7:0]} :
                             cpu_io_size == 2'd1 ? {16'd0, io_rdata[15:0]} : io_rdata;
            end
            if (inta_issued && inta_done) begin
                inta_issued <= 0; inta_orphan <= 0;
                inta_ack <= !inta_orphan && !cpu_reset;
                inta_result <= inta_vector;
            end
        end
    end

    // The attribute map, 640 KiB to 1 MiB in 16 KiB slots: VGA memory MMIO,
    // the VGA and system BIOS read-only, upper RAM on; 16 MiB of RAM.
    localparam [47:0] ATTR_UPPER = {{4{2'd1}}, {10{2'd0}}, {2{2'd1}}, {8{2'd2}}};
    wire [27:0] cpu_ddr_address;
    wire [7:0] cpu_ddr_burstcount;
    wire cpu_ddr_read, cpu_ddr_write, cpu_ddr_waitrequest, cpu_ddr_readdatavalid;
    wire [127:0] cpu_ddr_writedata, cpu_ddr_readdata;
    wire [15:0] cpu_ddr_byteenable;
    wire [7:0] cpu_vector;
    wire [5:0][15:0] cpu_sel;
    wire [31:0] cpu_eip;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_i386_cpu cpu (
        .clk(clk), .cold_reset(cpu_cold), .reset(cpu_reset),
        // The reset state, Bochs's: CS F000h at FFFF0000h, EIP FFF0h, every
        // limit FFFFh and every attribute a present read/write data segment;
        // CR0's MP and TS clear.
        .init_gpr('0), .init_eflags(32'h2), .init_cr0(32'h0), .init_eip(32'hfff0),
        .init_sel({16'h0, 16'h0, 16'h0, 16'h0, 16'hf000, 16'h0}),
        .init_base({32'h0, 32'h0, 32'h0, 32'h0, 32'hffff0000, 32'h0}),
        .init_limit({6{32'hffff}}), .init_ar({6{16'h0093}}),
        .a20m(a20m), .attr_upper(ATTR_UPPER), .ram_top(7'd16), .queue16(queue16),
        .ddr_address(cpu_ddr_address), .ddr_burstcount(cpu_ddr_burstcount), .ddr_read(cpu_ddr_read),
        .ddr_write(cpu_ddr_write), .ddr_writedata(cpu_ddr_writedata), .ddr_byteenable(cpu_ddr_byteenable),
        .ddr_waitrequest(cpu_ddr_waitrequest), .ddr_readdata(cpu_ddr_readdata),
        .ddr_readdatavalid(cpu_ddr_readdatavalid),
        .mmio_valid(mem_valid), .mmio_write(mem_write), .mmio_address(cpu_mmio_address),
        .mmio_byteenable(mem_byteenable), .mmio_wdata(mem_wdata), .mmio_ready(mem_ready),
        .mmio_rvalid(mem_rvalid), .mmio_rdata(mem_rdata),
        .dma_valid(dma_valid), .dma_write(dma_write), .dma_address(dma_address), .dma_wdata(dma_wdata),
        .dma_done(dma_done), .dma_rdata(dma_rdata),
        .flush_request(flush_request), .flush_done(flush_done),
        .io_req(cpu_io_req), .io_write(cpu_io_write), .io_port(cpu_io_port), .io_size(cpu_io_size),
        .io_wdata(cpu_io_wdata), .io_ack(io_ack), .io_rdata(io_result),
        .intr(intr), .nmi(1'b0), .inta_req(cpu_inta_req), .inta_ack(inta_ack), .inta_vector(inta_result),
        .trace_retire(cpu_retire), .trace_exception(), .trace_vector(cpu_vector), .trace_halt(cpu_halt),
        .trace_unimplemented(cpu_stopped), .trace_issue(), .trace_md_wait(), .trace_fetch(),
        .trace_fetch_block(), .trace_dread(), .trace_dread_addr(), .trace_mem_wait(),
        .trace_mem_idle(cpu_mem_idle), .trace_replay(), .trace_smc(), .trace_smc_open(), .trace_ex_store(),
        .trace_store(), .trace_store_addr(), .trace_store_data(), .trace_store_mask(), .arch_gpr(),
        .arch_eflags(), .arch_cr0(), .arch_eip(cpu_eip), .arch_sel(cpu_sel),
        .arch_base(), .arch_limit(), .arch_ar()
    );
    /* verilator lint_on PINCONNECTEMPTY */

    pc386_rom #(.ROM_HEX(ROM_HEX)) rom (
        .clk(clk), .reset(p0_reset),
        .address(cpu_ddr_address), .burstcount(cpu_ddr_burstcount), .read(cpu_ddr_read), .write(cpu_ddr_write),
        .writedata(cpu_ddr_writedata), .byteenable(cpu_ddr_byteenable), .waitrequest(cpu_ddr_waitrequest),
        .readdata(cpu_ddr_readdata), .readdatavalid(cpu_ddr_readdatavalid),
        .m_address(p0_address), .m_burstcount(p0_burstcount), .m_read(p0_read), .m_write(p0_write),
        .m_writedata(p0_writedata), .m_byteenable(p0_byteenable), .m_waitrequest(p0_waitrequest),
        .m_readdata(p0_readdata), .m_readdatavalid(p0_readdatavalid), .idle(rom_idle)
    );

    // ---- What the status block reports (pc386_host_link words 2-7):
    //   2  flags [7:0] (halted, stopped, A20M, INTR), last POST code [15:8],
    //      CPU resets [23:16], instructions retired [63:32]
    //   3  CS [15:0], the last exception or interrupt vector delivered [31:24],
    //      EIP [63:32]
    //   4  720p frames shown [31:0], scanout underflows [63:32]
    //   5  capture drops [31:0], presenter underflows [63:32]
    //   6  plane underflows [31:0], mixer samples [63:32]
    //   7  8237 memory cycles [31:0], OPL2 samples [63:32]
    // The presenter's and the plane's counters run in pixel_clk and cross
    // whole (pc386_snapshot).
    wire [31:0] present_underflows_sys, plane_underflows_sys, frames_shown_sys;
    pc386_snapshot #(.W(96)) pixel_counts (
        .source_clk(pixel_clk), .value({frames_shown, plane_underflows, present_underflows}),
        .clk(clk), .snapshot({frames_shown_sys, plane_underflows_sys, present_underflows_sys})
    );
    reg [31:0] retired, samples, opl2_samples, dma_cycles;
    reg [7:0] cpu_resets, last_post;
    always @(posedge clk)
        if (reset) begin
            retired <= 0; samples <= 0; opl2_samples <= 0; dma_cycles <= 0; cpu_resets <= 0; last_post <= 0;
        end else begin
            if (cpu_retire) retired <= retired + 32'd1;
            if (sample_valid) samples <= samples + 32'd1;
            if (opl2_valid) opl2_samples <= opl2_samples + 32'd1;
            if (dma_done) dma_cycles <= dma_cycles + 32'd1;
            if (machine_cpu_reset) cpu_resets <= cpu_resets + 8'd1;
            if (post_valid) last_post <= post_code;
        end
    wire [6*64-1:0] observe = {
        opl2_samples, dma_cycles, samples, plane_underflows_sys, present_underflows_sys, capture_drops,
        scanout_underflows, frames_shown_sys, cpu_eip, cpu_vector, 8'd0, cpu_sel[1],
        retired, 8'd0, cpu_resets, last_post, 4'd0, intr, a20m, cpu_stopped, cpu_halt
    };

    pc386_host_link link (
        .clk(clk), .reset(p2_reset), .machine_hold(machine_hold), .cpu_hold(cpu_hold),
        .base_kib(base_kib), .extended_kib(extended_kib), .above16m_64k(above16m_64k),
        .sb_base_high(sb_base_high), .sb_irq(sb_irq), .sb_dma(sb_dma),
        .disk_present(disk_present), .queue16(queue16), .disk_sectors(disk_sectors),
        .disk_cylinders(disk_cylinders), .disk_heads(disk_heads), .disk_spt(disk_spt),
        .cmos_busy(cmos_busy), .wall_valid(wall_valid), .wall_year(wall_year), .wall_month(wall_month),
        .wall_day(wall_day), .wall_hour(wall_hour), .wall_minute(wall_minute), .wall_second(wall_second),
        .wall_weekday(wall_weekday),
        .disk_valid(disk_valid), .disk_write(disk_write), .disk_address(disk_address), .disk_wdata(disk_wdata),
        .disk_ready(disk_ready), .disk_rvalid(disk_rvalid), .disk_rdata(disk_rdata),
        .debug_valid(debug_valid), .debug_byte(debug_byte), .observe(observe),
        .address(aux_address), .read(aux_read), .write(aux_write), .writedata(aux_writedata),
        .byteenable(aux_byteenable), .waitrequest(aux_waitrequest), .readdata(p2_readdata),
        .readdatavalid(aux_readdatavalid)
    );

    wire unused = ^{dma_ack_tc, start_latched, frame_start, frame_done, presenter_video, debug_port, opl2_sample,
                    dsp_dac, key_ready, dirty_word, frame_count, frame_buffer, frame_width, frame_height,
                    nmi_mask, cpu_mmio_address[fes_i386_pkg::PA-3:15], cpu_sel[5:2], cpu_sel[0]};
endmodule
