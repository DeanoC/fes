// SPDX-License-Identifier: MIT
`include "fes_computer.vh"

// pc386 system (docs/pc386-machine-design.md, phase M5 = CPU Phase 3): the
// machine and its audio with fes_i386_cpu, its L1s and bus interface on DDR
// port 0, behind the fes.computer 1.1 mailbox. The shell (top.sv) adds the
// PLLs, the HPS GP interface, fes_hps_ddr and the HDMI and I2S pins;
// everything here runs in clk except the plane's pixel side.
//
//   port 0  the CPU: guest RAM at window offset 0x04000000, and the firmware
//           ROM (pc386_rom) in front of it
//   port 1  VRAM
//   port 2  plane, presenter, capture; pc386_disk: the ATA image at window
//           offset 0x08000000 and its CRC pass
//
// The mailbox (fes_computer_mailbox_core) answers identity, execution, the
// keyboard's rows, mouse packets and the session-display plane, and hands the
// ATA disk, the fes.pc386.machine record and the wall clock to
// fes_computer_storage. The rows go to the 8042's host keyboard model, and a
// mouse packet to its aux device once the machine is out of reset. The record's
// settings are the machine's straps at each release: the governor's preset,
// frequency and turbo at launch (fes_i386_governor samples them at CPU reset),
// the self-modifying-code profile and the Sound Blaster's port, IRQ and DMA.
// Its CMOS words are what the RTC
// loads after reset and writes back. The wall clock reaches the RTC once its
// CMOS setup is done, and the CPU starts after that.
//
// Hold stops the CPU at once: its warm reset lets port 0's traffic finish.
// Once the CPU has been held 4 clocks with its memory idle and the drive has
// landed every sector it accepted (drained), ddr_hold makes fes_hps_ddr hold
// every port, and once port 0 has taken the hold the machine is reset with
// them (machine_held). While the disk verifies, ddr_hold is low so that the
// CRC pass can read port 2; the machine stays in reset. p0_reset..p2_reset
// are fes_hps_ddr's per-port resets. After release the only reset is the
// CPU's own, from the AT core (8042, port 92h), which asserts FLUSH first: it
// holds the CPU at least 4 clocks and until its memory is idle, and the
// machine, its DDR ports and port 0's reads in flight carry on. Every guest
// time base is a rational enable of clk; the producer sets each N/D pair
// from the sealed frequency.
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
    parameter integer GOV_N = 1, GOV_D = 1000,  // 100 kHz, the governor's frequency unit
    parameter integer ATA_RESET_CLOCKS = 1000,
    parameter ROM_HEX = "build/firmware/pc386/pc386-rom.hex"
) (
    input wire clk, pixel_clk, pixel_locked,
    // The HPS's GP mailbox
    input wire [31:0] gpo,
    input wire [127:0] build_id,
    output wire [31:0] gpi,
    // Held execution (the audio's silence), fes_hps_ddr's hold and its resets
    output wire exec_reset,
    output reg ddr_hold,
    input wire p0_reset, p1_reset, p2_reset,
    // The plane's output, in pixel_clk
    output wire [27:0] video,
    // Mixer output at 48 kHz
    output wire signed [15:0] sample,
    // What the system does, for system_tb; the shell leaves these open.
    //   0  flags [7:0] (halted, stopped, A20M, INTR, running, governor held D
    //      since the machine's reset), last POST code
    //      [15:8], CPU resets [23:16], instructions retired [63:32]
    //   1  CS [15:0], the last exception or interrupt vector delivered [31:24],
    //      EIP [63:32]
    //   2  720p frames shown [31:0], scanout underflows [63:32]
    //   3  capture drops [31:0], presenter underflows [63:32]
    //   4  plane underflows [31:0], mixer samples [63:32]
    //   5  8237 memory cycles [31:0], OPL2 samples [63:32]
    // The presenter's and the plane's counters run in pixel_clk and cross
    // whole (pc386_snapshot). debug_byte is each debug-port byte.
    output wire [6*64-1:0] status,
    output wire debug_valid,
    output wire [7:0] debug_byte,
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
    // ---- The mailbox and its storage endpoint
    wire display_request, display_response_valid, display_response_error, display_quiesced;
    wire [6:0] display_opcode;
    wire [7:0] display_index;
    wire [15:0] display_argument, display_response_data;
    wire storage_request, storage_response_valid, storage_response_error, execution_accepted;
    wire [6:0] storage_opcode;
    wire [7:0] storage_index;
    wire [15:0] storage_argument, storage_response_data;
    wire [143:0] keyboard_rows;
    wire mouse_valid, mouse_ready;
    // Unsigned: the pc386 lock's Yosys asserts when a chparam'd top slices a
    // signed wire into a parameterised cell (DeanoC/yosys#71).
    wire [15:0] mouse_dx, mouse_dy;
    wire [1:0] mouse_buttons;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(1), .ENABLE_MOUSE(1), .ENABLE_AUDIO(1), .ENABLE_SESSION_DISPLAY(1),
        .ENABLE_ATA_DISK(1), .ENABLE_PERSISTENCE(1), .ENABLE_PC386_MACHINE(1), .ENABLE_WALL_CLOCK(1)
    ) gp_mailbox (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows), .controller_buttons(),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy), .mouse_buttons(mouse_buttons),
        .mouse_ready(mouse_ready),
        .media_write_addr(), .media_write_data(), .media_write_enable(), .media_write_unit(),
        .media_write_ready(1'b0), .media_write_busy(1'b0), .media_changed(1'b0),
        .media_reset(1'b0), .media_frozen(),
        .media_read_req(), .media_read_addr(), .media_read_ready(1'b0), .media_read_data(8'd0),
        .save_ready(1'b0), .save_busy(1'b0), .save_size(15'd0),
        .unit0_state(), .unit0_size(), .unit1_state(), .unit1_size(),
        .display_request(display_request), .display_opcode(display_opcode),
        .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(display_response_valid), .display_response_error(display_response_error),
        .display_response_data(display_response_data), .display_quiesced(display_quiesced),
        .storage_request(storage_request), .storage_opcode(storage_opcode), .storage_index(storage_index),
        .storage_argument(storage_argument), .storage_response_valid(storage_response_valid),
        .storage_response_error(storage_response_error), .storage_response_data(storage_response_data),
        .execution_accepted(execution_accepted)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    wire [2:0] disk_state;
    wire [31:0] disk_sectors, disk_crc;
    wire [15:0] disk_cylinders;
    wire [4:0] disk_heads;
    wire [5:0] disk_spt;
    wire disk_verify_done, disk_verify_match, disk_frozen, disk_fenced, disk_writer_busy, disk_written;
    wire disk_dirty_clear, disk_dirty_clearing;
    wire [6:0] dirty_index;
    wire [15:0] dirty_word;
    wire [127:0] settings;
    wire record_write, record_load;
    wire [6:0] record_index;
    wire [7:0] record_byte;
    wire [5:0] record_pair;
    wire [15:0] record_word, record_clock_pair;
    wire [2:0] record_clock_index;
    wire wall_apply;
    wire [15:0] wall_year;
    wire [7:0] wall_month, wall_day, wall_hour, wall_minute, wall_second, wall_weekday;
    fes_computer_storage #(.ENABLE_ATA_DISK(1), .ENABLE_DATA(1), .ENABLE_WALL_CLOCK(1)) storage (
        .clk(clk), .request(storage_request), .opcode(storage_opcode), .index(storage_index),
        .argument(storage_argument), .response_valid(storage_response_valid),
        .response_error(storage_response_error), .response_data(storage_response_data),
        .exec_reset(exec_reset), .execution_accepted(execution_accepted),
        .disk_state(disk_state), .disk_sectors(disk_sectors), .disk_cylinders(disk_cylinders),
        .disk_heads(disk_heads), .disk_spt(disk_spt), .disk_crc(disk_crc),
        .disk_verify_done(disk_verify_done), .disk_verify_match(disk_verify_match),
        .disk_frozen(disk_frozen), .disk_fenced(disk_fenced), .disk_busy(disk_writer_busy),
        .disk_written(disk_written), .disk_clear(disk_dirty_clear), .disk_clearing(disk_dirty_clearing),
        .disk_dirty_index(dirty_index), .disk_dirty_word(dirty_word),
        .machine_settings(settings), .cmos_write(record_write), .cmos_byte_index(record_index),
        .cmos_byte(record_byte), .cmos_load(record_load), .cmos_load_pair(record_pair),
        .cmos_load_word(record_word), .cmos_clock_index(record_clock_index), .cmos_clock_pair(record_clock_pair),
        .wall_apply(wall_apply), .wall_year(wall_year), .wall_month(wall_month), .wall_day(wall_day),
        .wall_hour(wall_hour), .wall_minute(wall_minute), .wall_second(wall_second),
        .wall_weekday(wall_weekday)
    );
    wire verifying = disk_state == 3'(`FES_COMPUTER_DISK_STATE_VERIFYING);
    wire disk_present = disk_state == 3'(`FES_COMPUTER_MEDIA_STATE_READY);
    // The settings words: 0 the governor's preset, 1 its frequency override
    // (100 kHz units), 2 the code profile, 3 turbo at launch, 4-6 the Sound
    // Blaster's port, IRQ and DMA channel. Commit has checked each field's
    // range (fes_computer_storage).
    wire [2:0] gov_preset = settings[2:0];
    wire [9:0] gov_frequency = settings[25:16];
    wire gov_turbo = settings[48];
    wire queue16 = settings[32];
    wire [7:0] sb_base_high = settings[75:68];
    wire [7:0] sb_irq = settings[87:80];
    wire [7:0] sb_dma = settings[103:96];

    // ---- Hold and the machine reset
    wire cpu_mem_idle, rom_idle;
    reg [2:0] quiet = 3'd0;               // clocks the CPU has been held, to 4
    reg drained = 1'b1, machine_held = 1'b1;
    initial ddr_hold = 1'b1;
    always @(posedge clk) begin
        quiet <= !exec_reset ? 3'd0 : quiet == 3'd4 ? 3'd4 : quiet + 3'd1;
        drained <= exec_reset && (drained || (quiet == 3'd4 && cpu_mem_idle && rom_idle && !disk_writer_busy));
        machine_held <= drained && (machine_held || p0_reset);
        ddr_hold <= drained && !verifying;
    end
    // An applied wall clock waits for the RTC's CMOS setup after release.
    reg wall_pending = 1'b0, wall_valid = 1'b0;
    always @(posedge clk) begin
        wall_valid <= 0;
        if (wall_apply) wall_pending <= 1;
        else if (wall_pending && !reset && !cmos_busy) begin wall_valid <= 1; wall_pending <= 0; end
    end
    // One register drives the machine reset: the imported chips take it
    // asynchronously.
    reg reset = 1'b1;
    always @(posedge clk) reset <= machine_held || p0_reset || p1_reset || p2_reset;

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
    wire post_valid;
    wire [7:0] post_code;
    wire [15:0] debug_port;
    wire cmos_busy;
    wire disk_valid, disk_write, disk_ready, disk_rvalid;
    wire [23:0] disk_address;
    wire [63:0] disk_wdata, disk_rdata;
    wire frame_start, frame_done, start_latched;
    wire [15:0] frame_count;
    wire [1:0] frame_buffer;
    wire [9:0] frame_width, frame_height;
    wire [31:0] scanout_underflows, capture_drops, present_underflows, plane_underflows, frames_shown;
    wire [27:0] presenter_video;
    wire [28:0] aux_address;
    wire [7:0] aux_burstcount;
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
        // The mailbox sign-extends the packet's 8-bit dx and dy.
        .keyboard_rows(keyboard_rows), .mouse_valid(mouse_valid), .mouse_dx(mouse_dx[7:0]),
        .mouse_dy(mouse_dy[7:0]), .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready),
        // 640 KiB and 15 MiB of extended memory: 16 MiB of RAM, ram_top below.
        .base_kib(16'd640), .extended_kib(16'd15360), .above16m_64k(16'd0),
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
        .record_load(record_load), .record_pair(record_pair), .record_word(record_word),
        .record_clock_index(record_clock_index), .record_clock_pair(record_clock_pair),
        .p1_address(p1_address), .p1_burstcount(p1_burstcount), .p1_read(p1_read), .p1_write(p1_write),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_waitrequest(p1_waitrequest),
        .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid),
        .p2_address(p2_address), .p2_burstcount(p2_burstcount), .p2_read(p2_read), .p2_write(p2_write),
        .p2_writedata(p2_writedata), .p2_byteenable(p2_byteenable), .p2_waitrequest(p2_waitrequest),
        .p2_readdata(p2_readdata), .p2_readdatavalid(p2_readdatavalid),
        .aux_address(aux_address), .aux_burstcount(aux_burstcount), .aux_read(aux_read), .aux_write(aux_write),
        .aux_writedata(aux_writedata),
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
    // It leaves reset once the machine has, the RTC's CMOS setup is done and
    // it has any wall clock the runtime sent (started), and Hold stops it at
    // once. The AT core's CPU reset pulse holds it again for at least 4 clocks
    // and until its memory is idle.
    wire cpu_halt, cpu_stopped, cpu_retire, gov_hold;
    reg started, cpu_cold, cpu_reset;
    reg [2:0] stretch;
    always @(posedge clk) begin
        cpu_cold <= reset;
        cpu_reset <= reset || exec_reset || !started || stretch != 3'd0;
        if (reset) begin
            started <= 0; stretch <= 0;
        end else begin
            if (!cmos_busy && !wall_pending && !wall_valid) started <= 1;
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
    fes_i386_cpu #(.GOV_N(GOV_N), .GOV_D(GOV_D)) cpu (
        .clk(clk), .cold_reset(cpu_cold), .reset(cpu_reset),
        // The reset state, Bochs's: CS F000h at FFFF0000h, EIP FFF0h, every
        // limit FFFFh and every attribute a present read/write data segment;
        // CR0 clear (ET too: no 387), CR2 and CR3 0, GDTR and IDTR at 0
        // with limit FFFFh. The GPRs' zero is sized: an unsized '0 on a
        // parameterised cell's port asserts in the lock's Yosys under
        // hierarchy -chparam (DeanoC/yosys#71).
        .init_gpr(256'd0), .init_eflags(32'h2), .init_cr0(32'h0), .init_cr2(32'h0), .init_cr3(32'h0),
        .init_gdtr_base(32'h0), .init_gdtr_limit(16'hffff), .init_idtr_base(32'h0), .init_idtr_limit(16'hffff),
        .init_eip(32'hfff0),
        .init_sel({16'h0, 16'h0, 16'h0, 16'h0, 16'hf000, 16'h0}),
        .init_base({32'h0, 32'h0, 32'h0, 32'h0, 32'hffff0000, 32'h0}),
        .init_limit({6{32'hffff}}), .init_ar({6{16'h0093}}),
        .a20m(a20m), .attr_upper(ATTR_UPPER), .ram_top(7'd16), .queue16(queue16),
        .gov_preset(gov_preset), .gov_frequency(gov_frequency), .gov_turbo(gov_turbo),
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
        .trace_unimplemented(cpu_stopped), .trace_pm_stop(), .trace_error(), .trace_issue(), .trace_md_wait(), .trace_fetch(),
        .trace_fetch_block(), .trace_dread(), .trace_dread_addr(), .trace_mem_wait(),
        .trace_mem_idle(cpu_mem_idle), .trace_replay(), .trace_smc(), .trace_smc_open(), .trace_ex_store(),
        .trace_gov_hold(gov_hold), .trace_gov_debt(), .trace_gov_charge(),
        .trace_store(), .trace_store_addr(), .trace_store_data(), .trace_store_mask(), .arch_gpr(),
        .arch_eflags(), .arch_cr0(), .arch_cr2(), .arch_cr3(), .arch_gdtr_base(), .arch_gdtr_limit(),
        .arch_idtr_base(), .arch_idtr_limit(), .arch_eip(cpu_eip), .arch_sel(cpu_sel),
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

    // ---- The status words
    wire [31:0] present_underflows_sys, plane_underflows_sys, frames_shown_sys;
    pc386_snapshot #(.W(96)) pixel_counts (
        .source_clk(pixel_clk), .value({frames_shown, plane_underflows, present_underflows}),
        .clk(clk), .snapshot({frames_shown_sys, plane_underflows_sys, present_underflows_sys})
    );
    reg [31:0] retired, samples, opl2_samples, dma_cycles;
    reg [7:0] cpu_resets, last_post;
    reg gov_held;
    always @(posedge clk)
        if (reset) begin
            retired <= 0; samples <= 0; opl2_samples <= 0; dma_cycles <= 0; cpu_resets <= 0; last_post <= 0;
            gov_held <= 0;
        end else begin
            if (gov_hold) gov_held <= 1;
            if (cpu_retire) retired <= retired + 32'd1;
            if (sample_valid) samples <= samples + 32'd1;
            if (opl2_valid) opl2_samples <= opl2_samples + 32'd1;
            if (dma_done) dma_cycles <= dma_cycles + 32'd1;
            if (machine_cpu_reset) cpu_resets <= cpu_resets + 8'd1;
            if (post_valid) last_post <= post_code;
        end
    assign status = {
        opl2_samples, dma_cycles, samples, plane_underflows_sys, present_underflows_sys, capture_drops,
        scanout_underflows, frames_shown_sys, cpu_eip, cpu_vector, 8'd0, cpu_sel[1],
        retired, 8'd0, cpu_resets, last_post, 2'd0, gov_held, !reset, intr, a20m, cpu_stopped, cpu_halt
    };

    // ---- The ATA image and the CRC pass on port 2
    pc386_disk disk (
        .clk(clk), .reset(p2_reset), .verify(verifying), .sectors(disk_sectors), .crc(disk_crc),
        .verify_done(disk_verify_done), .verify_match(disk_verify_match),
        .disk_valid(disk_valid), .disk_write(disk_write), .disk_address(disk_address), .disk_wdata(disk_wdata),
        .disk_ready(disk_ready), .disk_rvalid(disk_rvalid), .disk_rdata(disk_rdata),
        .aux_address(aux_address), .aux_burstcount(aux_burstcount), .aux_read(aux_read), .aux_write(aux_write),
        .aux_writedata(aux_writedata), .aux_byteenable(aux_byteenable), .aux_waitrequest(aux_waitrequest),
        .aux_readdatavalid(aux_readdatavalid), .readdata(p2_readdata)
    );

    wire unused = ^{dma_ack_tc, mouse_dx[15:8], mouse_dy[15:8], start_latched, frame_start, frame_done, presenter_video, debug_port, opl2_sample,
                    dsp_dac, frame_count, frame_buffer, frame_width, frame_height,
                    nmi_mask, cpu_mmio_address[fes_i386_pkg::PA-3:15], cpu_sel[5:2], cpu_sel[0],
                    settings[15:3], settings[31:26], settings[63:49], settings[127:104], settings[47:33], settings[79:76],
                    settings[67:64], settings[95:88]};
endmodule
