// SPDX-License-Identifier: GPL-3.0-or-later
`include "fes_computer.vh"
// Atari 520ST machine, control, media and video for a board without linked
// ROM lanes (fes.computer 1.0 with fes.fabric.firmware-upload). The machine
// sources are the DE10 core's; this replaces its shell. clk_sys is the
// machine's 52.224 MHz. Firmware arrives through the mailbox front end before
// execution can be released. RAM, the floppy image and the ROM live in one
// SDRAM rank through the DE10 core's own st_memory, on the same clock and with
// the same phase slots; the ROM shares the CPU's memory port. The expansion and video sockets are vacant: the 720p word
// leaves through the direct video part for the session display.
// Board wrappers own clocks, HPS transport, the SDRAM pads, HDMI and audio
// serialization. No FPGA-family primitive belongs here.
module fes_atari_st_core #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire clk_sys, pixel_clk,
    // cold_reset restarts SDRAM initialization; reset_hold holds the machine.
    input wire cold_reset, pixel_reset, reset_hold,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire machine_held,
    output wire [27:0] machine_video,
    output wire signed [15:0] audio_sample,
    output wire display_request,
    output wire [6:0] display_opcode,
    output wire [7:0] display_index,
    output wire [15:0] display_argument,
    input wire display_response_valid, display_response_error,
    input wire [15:0] display_response_data,
    input wire display_quiesced,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire sdram_dqml, sdram_dqmh,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_in
);
    wire [31:0] mailbox_gpo, mailbox_gpi;
    wire [17:0] firmware_write_addr;
    wire [15:0] firmware_write_data;
    wire [1:0] firmware_write_enable;
    wire firmware_write_ready;
    fes_firmware_upload #(.AW(18), .BYTES(32'd196608)) firmware (
        .clk(clk_sys), .gpo(gpo), .gpi(gpi),
        .mailbox_gpo(mailbox_gpo), .mailbox_gpi(mailbox_gpi), .exec_reset(exec_reset),
        .ready(), .write_addr(firmware_write_addr), .write_data(firmware_write_data),
        .write_enable(firmware_write_enable), .write_ready(firmware_write_ready)
    );

    wire [143:0] keyboard;
    wire [15:0] controller_buttons;
    wire mouse_valid, mouse_ready;
    wire signed [15:0] mouse_dx, mouse_dy;
    wire [1:0] mouse_buttons;
    wire [19:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable, unit0_state;
    wire [31:0] unit0_size;
    wire media_source_ready;
    wire media_frozen, floppy_write_busy, floppy_changed;
    wire snapshot_req, snapshot_ready;
    wire [19:0] snapshot_addr;
    wire [7:0] snapshot_data;
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1), .ENABLE_MOUSE(1),
        .ENABLE_ATARI_ST_FLOPPY(1), .ENABLE_ATARI_ST_FLOPPY_WRITE(1),
        .ENABLE_ATARI_ST_FLOPPY_GEOMETRY(1), .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(20),
        .ENABLE_SESSION_DISPLAY(1), .ENABLE_HPS_DDR_WINDOW(0),
        .UNIT0_MIN(`FES_COMPUTER_ATARI_ST_FLOPPY_GEOMETRY_MIN_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_ATARI_ST_FLOPPY_GEOMETRY_MAX_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(mailbox_gpo), .build_id(BUILD_ID), .gpi(mailbox_gpi),
        .exec_reset(exec_reset), .keyboard_rows(keyboard), .controller_buttons(controller_buttons),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
        .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_unit(),
        .media_write_ready(media_source_ready),
        .media_write_busy(floppy_write_busy), .media_changed(floppy_changed),
        .media_reset(1'b0), .media_frozen(media_frozen),
        .media_read_req(snapshot_req), .media_read_addr(snapshot_addr),
        .media_read_ready(snapshot_ready), .media_read_data(snapshot_data),
        .save_ready(1'b0), .save_busy(1'b0), .save_size(15'd0),
        .unit0_state(unit0_state), .unit0_size(unit0_size),
        .unit1_state(), .unit1_size(),
        .display_request(display_request), .display_opcode(display_opcode),
        .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(display_response_valid),
        .display_response_error(display_response_error),
        .display_response_data(display_response_data),
        .display_quiesced(display_quiesced),
        .storage_request(), .storage_opcode(), .storage_index(), .storage_argument(),
        .storage_response_valid(1'b0), .storage_response_error(1'b0), .storage_response_data(16'd0),
        .execution_accepted()
    );

    wire initialized;
    // One registered copy per consumer keeps cold_reset off machine_release[1]
    // (one async-reset synchronizer reaching every subsystem triggers TMC-20602).
    // Each copy asserts asynchronously with cold_reset (while the machine PLL
    // is held in reset and clk_sys is stopped) and releases synchronously on
    // clk_sys one clock after machine_release[1], matching fes_atari_st_top.v.
    (* preserve, dont_merge *) reg system_cold_reset_n = 1'b0;
    (* preserve, dont_merge *) reg video_cold_reset_n = 1'b0;
    (* preserve, dont_merge *) reg memory_cold_reset_n = 1'b0;
    (* preserve, dont_merge *) reg media_cold_reset_n = 1'b0;
    always @(posedge clk_sys or posedge cold_reset) begin
        if (cold_reset) begin
            system_cold_reset_n <= 1'b0;
            video_cold_reset_n <= 1'b0;
            memory_cold_reset_n <= 1'b0;
            media_cold_reset_n <= 1'b0;
        end else begin
            system_cold_reset_n <= 1'b1;
            video_cold_reset_n <= 1'b1;
            memory_cold_reset_n <= 1'b1;
            media_cold_reset_n <= 1'b1;
        end
    end
    wire system_cold_reset = !system_cold_reset_n;
    wire video_cold_reset = !video_cold_reset_n;
    wire memory_cold_reset = !memory_cold_reset_n;
    wire media_cold_reset = !media_cold_reset_n;
    // One reset in six registers: the bus, the processor, the I/O decode,
    // the keyboard and serial chips, the sound chip, and the memory and video
    // raster. One register reaching all of them is too spread to place
    // (TMC-20602). Each peripheral group combines its register with a
    // register of the 68000's RESET output, as st_machine combines exp_reset.
    reg machine_reset = 1'b1;
    (* preserve, dont_merge *) reg machine_reset_cpu = 1'b1;
    (* preserve, dont_merge *) reg machine_reset_io = 1'b1;
    (* preserve, dont_merge *) reg machine_reset_input = 1'b1;
    (* preserve, dont_merge *) reg machine_reset_sound = 1'b1;
    (* preserve, dont_merge *) reg machine_reset_raster = 1'b1;
    wire machine_reset_next = exec_reset || cold_reset || reset_hold || !initialized;
    always @(posedge clk_sys) begin
        machine_reset <= machine_reset_next;
        machine_reset_cpu <= machine_reset_next;
        machine_reset_io <= machine_reset_next;
        machine_reset_input <= machine_reset_next;
        machine_reset_sound <= machine_reset_next;
        machine_reset_raster <= machine_reset_next;
    end
    assign machine_held = machine_reset;
    // The video's hold crosses into the pixel clock from a register of its own.
    (* preserve, dont_merge *) reg video_hold = 1'b1;
    always @(posedge clk_sys) video_hold <= machine_reset;
    wire rom_req, rom_ready, ram_req, ram_ready, ram_write;
    wire [17:1] rom_addr;
    wire [18:1] ram_addr;
    wire [15:0] rom_rdata, ram_rdata, ram_wdata;
    wire [1:0] ram_byte_enable;
    wire exp_phi2;
    wire io_reset = machine_reset_io || system.machine.cpu.cpu.oReset;
    wire input_reset = machine_reset_input || system.machine.cpu.cpu.oResetInput;
    wire sound_reset = machine_reset_sound || system.machine.cpu.cpu.oResetSound;
    wire raster_reset = machine_reset_raster || system.machine.cpu.cpu.oResetRaster;
    wire audio_valid, media_req, media_valid;
    wire [19:0] media_addr;
    wire [7:0] media_data;
    wire dma_req, dma_ready, dma_write;
    wire [15:0] dma_rdata;
    wire floppy_write_req, floppy_write_ready;
    wire [19:1] floppy_write_addr;
    wire [15:0] floppy_write_data;
    wire [23:0] dma_addr;
    wire [15:0] dma_wdata;
    wire [1:0] dma_byte_enable;
    wire [23:0] screen_base;
    wire [1:0] resolution;
    wire [143:0] palette;
    wire [7:0] sync_mode;
    wire vblank;
    wire native_display;
    wire [8:0] native_line, native_cycle;
    wire native_pixel_ce;
    wire native_short_line, native_blank;
    wire [8:0] native_word_loss;
    wire cpu_req, cpu_rom, cpu_write, cpu_ready;
    wire [18:1] cpu_addr;
    wire [15:0] cpu_wdata, cpu_rdata;
    wire [1:0] cpu_byte_enable;
    fes_st_rom_sdram rom (
        .rom_req(rom_req), .rom_addr(rom_addr), .rom_rdata(rom_rdata), .rom_ready(rom_ready),
        .ram_req(ram_req), .ram_write(ram_write), .ram_addr(ram_addr), .ram_wdata(ram_wdata),
        .ram_byte_enable(ram_byte_enable), .ram_rdata(ram_rdata), .ram_ready(ram_ready),
        .firmware_write_addr(firmware_write_addr), .firmware_write_data(firmware_write_data),
        .firmware_write_enable(firmware_write_enable), .firmware_write_ready(firmware_write_ready),
        .cpu_req(cpu_req), .cpu_rom(cpu_rom), .cpu_write(cpu_write), .cpu_addr(cpu_addr),
        .cpu_wdata(cpu_wdata), .cpu_byte_enable(cpu_byte_enable), .cpu_ready(cpu_ready),
        .cpu_rdata(cpu_rdata)
    );
    st_system #(.ENABLE_FLOPPY_WRITE(1), .EARLY_RAM_WRITE_COMPLETION(1)) system (
        .clk_sys(clk_sys), .reset(machine_reset), .cold_reset(system_cold_reset),
        .rom_req(rom_req), .rom_addr(rom_addr), .rom_rdata(rom_rdata), .rom_ready(rom_ready),
        .ram_req(ram_req), .ram_addr(ram_addr), .ram_wdata(ram_wdata),
        .ram_byte_enable(ram_byte_enable), .ram_write(ram_write), .ram_rdata(ram_rdata), .ram_ready(ram_ready),
        .exp_req(), .exp_addr(), .exp_wdata(), .exp_byte_enable(),
        .exp_write(), .exp_fc(), .cpu_reset(machine_reset_cpu), .io_reset(io_reset), .input_reset(input_reset),
        .sound_reset(sound_reset), .video_reset(raster_reset), .exp_reset(), .exp_phi1(), .exp_phi2(exp_phi2),
        .exp_present(1'b0), .exp_ack(1'b0), .exp_berr(1'b0), .exp_rdata(16'd0), .exp_irq(3'd0),
        .irq_ack(), .irq_level(), .keyboard(keyboard), .controller_buttons(controller_buttons),
        .monochrome(1'b0), .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
        .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready), .audio_pcm(audio_sample), .audio_valid(audio_valid),
        .media_ready(unit0_state == 2'(`FES_COMPUTER_MEDIA_STATE_READY)), .media_size(unit0_size),
        .media_req(media_req), .media_addr(media_addr), .media_data(media_data), .media_valid(media_valid),
        .media_frozen(media_frozen), .media_write_req(floppy_write_req), .media_write_addr(floppy_write_addr),
        .media_write_data(floppy_write_data), .media_write_ready(floppy_write_ready),
        .media_write_busy(floppy_write_busy), .media_changed(floppy_changed),
        .dma_req(dma_req), .dma_write(dma_write), .dma_rdata(dma_rdata), .dma_addr(dma_addr),
        .dma_wdata(dma_wdata), .dma_byte_enable(dma_byte_enable),
        .dma_ready(dma_ready), .screen_base(screen_base), .resolution(resolution), .palette(palette),
        .sync_mode(sync_mode), .debug_addr(), .debug_bus_error(),
        .debug_overlay(), .debug_halted(), .vblank(vblank), .hblank(),
        .native_display(native_display), .native_short_line(native_short_line),
        .native_blank(native_blank), .native_word_loss(native_word_loss), .native_line(native_line),
        .native_cycle(native_cycle), .native_pixel_ce(native_pixel_ce)
    );

    wire video_req, video_ready;
    wire [18:1] video_addr;
    wire [15:0] video_rdata;
    wire [31:0] video_request;
    wire ram_write_committed;
    wire [18:1] ram_write_addr;
    wire [15:0] ram_write_data;
    wire [1:0] ram_write_lanes;
    st_video_adapter video (
        .ram_write_committed(ram_write_committed), .ram_write_addr(ram_write_addr),
        .ram_write_data(ram_write_data), .ram_write_lanes(ram_write_lanes),
        .clk_sys(clk_sys), .clk_pixel(pixel_clk), .reset_sys(video_cold_reset), .reset_pixel(pixel_reset),
        .native_vblank(vblank), .native_display(native_display), .native_short_line(native_short_line),
        .native_blank(native_blank), .native_word_loss(native_word_loss), .native_line(native_line),
        .native_cycle(native_cycle), .native_pixel_ce(native_pixel_ce), .native_lookup_ce(exp_phi2),
        .sync_mode(sync_mode),
        .hold(video_hold), .screen_base(screen_base), .resolution(resolution), .palette(palette),
        .video_req(video_req), .video_addr(video_addr), .video_ready(video_ready), .video_rdata(video_rdata),
        .video_request(video_request), .debug_underruns(), .debug_frame()
    );
    // The DE10 shell's registered request, direct part and registered
    // response, without its vacant socket.
    reg [31:0] video_request_q = 32'd0;
    reg [27:0] video_response_q = 28'd0;
    wire [27:0] video_result;
    fes_video_part_direct part (.video_request(video_request_q), .video_response(video_result));
    always @(posedge pixel_clk) begin
        video_request_q <= video_request;
        video_response_q <= video_result;
    end
    assign machine_video = video_response_q;

    wire media_write_req, media_memory_ready;
    wire [19:1] media_memory_addr;
    wire [15:0] media_memory_wdata;
    wire [1:0] media_memory_enable;
    st_media_writer writer (
        .clk(clk_sys), .cold_reset(media_cold_reset), .source_addr(media_write_addr), .source_data(media_write_data),
        .source_enable(media_write_enable), .source_ready(media_source_ready),
        .memory_req(media_write_req), .memory_addr(media_memory_addr), .memory_wdata(media_memory_wdata),
        .memory_byte_enable(media_memory_enable), .memory_ready(media_memory_ready)
    );
    wire shared_write_req, shared_write_ready, shared_read_req, shared_read_ready;
    wire [18:0] shared_write_addr;
    wire [19:0] shared_read_addr;
    wire [15:0] shared_write_data;
    wire [1:0] shared_write_enable, write_ready, read_ready;
    wire [7:0] shared_read_data;
    st_media_port #(.ADDR_BITS(19)) media_writes (
        .clk(clk_sys), .cold_reset(media_cold_reset), .source_req({floppy_write_req,media_write_req}),
        .source_addr0(media_memory_addr), .source_addr1(floppy_write_addr),
        .source_data0(media_memory_wdata), .source_data1(floppy_write_data),
        .source_enable0(media_memory_enable), .source_enable1(2'b11), .source_ready(write_ready),
        .memory_req(shared_write_req), .memory_addr(shared_write_addr), .memory_data(shared_write_data),
        .memory_enable(shared_write_enable), .memory_ready(shared_write_ready)
    );
    assign media_memory_ready = write_ready[0];
    assign floppy_write_ready = write_ready[1];
    st_media_port #(.ADDR_BITS(20)) media_reads (
        .clk(clk_sys), .cold_reset(media_cold_reset), .source_req({snapshot_req,media_req}),
        .source_addr0(media_addr), .source_addr1(snapshot_addr),
        .source_data0(16'd0), .source_data1(16'd0),
        .source_enable0(2'd0), .source_enable1(2'd0), .source_ready(read_ready),
        .memory_req(shared_read_req), .memory_addr(shared_read_addr), .memory_data(), .memory_enable(),
        .memory_ready(shared_read_ready)
    );
    assign media_valid = read_ready[0];
    assign snapshot_ready = read_ready[1];
    assign media_data = shared_read_data;
    assign snapshot_data = shared_read_data;

    // The memory's phase slots follow the machine's raster cycle, which stands
    // still while the machine is held. Firmware is written through the CPU's
    // write slot during that hold, so the slots turn on a free count then. No
    // machine or video request is in flight across the change.
    reg [1:0] hold_phase = 2'd0;
    always @(posedge clk_sys) hold_phase <= hold_phase + 2'd1;
    wire [1:0] memory_phase = machine_reset_raster ? hold_phase : native_cycle[1:0];
    // Rate 0 consumes one sample a machine clock. The SDRAM clock pin is the
    // inverted machine clock, and this board's SDRAM answers up to 8 ns after
    // it: too late for the rising fabric edge half a period on. The sample is
    // taken on the next falling edge, at the chip's following clock edge and
    // before its data changes, and the controller reads it on the rising edge
    // where it read the DE10 shell's sample.
    reg [15:0] dq_sample;
    always @(negedge clk_sys) dq_sample <= dq_in;
    st_memory #(.PHASE_SLOTS(1), .POSTED_CPU_WRITES(1),
                .SINGLE_RANK_REFRESH(1), .SLOT_REFRESH(1)) memory (
        .ram_write_committed(ram_write_committed), .ram_write_addr(ram_write_addr),
        .ram_write_data(ram_write_data), .ram_write_lanes(ram_write_lanes),
        .clk(clk_sys), .clk_pin(clk_sys), .memory_phase(memory_phase),
        .raster_reset(raster_reset), .cold_reset(memory_cold_reset), .reset(1'b0), .initialized(initialized),
        .cpu_req(cpu_req), .cpu_rom(cpu_rom), .cpu_addr(cpu_addr), .cpu_write(cpu_write),
        .cpu_wdata(cpu_wdata), .cpu_byte_enable(cpu_byte_enable), .cpu_ready(cpu_ready),
        .cpu_rdata(cpu_rdata),
        .video_req(video_req), .video_addr(video_addr), .video_ready(video_ready), .video_rdata(video_rdata),
        .dma_req(dma_req), .dma_addr(dma_addr), .dma_write(dma_write), .dma_wdata(dma_wdata),
        .dma_byte_enable(dma_byte_enable), .dma_ready(dma_ready), .dma_rdata(dma_rdata),
        .media_write_req(shared_write_req), .media_write_addr(shared_write_addr),
        .media_write_wdata(shared_write_data),
        .media_write_byte_enable(shared_write_enable), .media_write_ready(shared_write_ready),
        .media_read_req(shared_read_req), .media_read_addr(shared_read_addr),
        .media_read_ready(shared_read_ready), .media_read_rdata(shared_read_data),
        .sdram_clk(sdram_clk), .sdram_cke(sdram_cke), .sdram_ncs(sdram_ncs), .sdram_nras(sdram_nras),
        .sdram_ncas(sdram_ncas), .sdram_nwe(sdram_nwe), .sdram_ba(sdram_ba), .sdram_a(sdram_a),
        .sdram_dqml(sdram_dqml), .sdram_dqmh(sdram_dqmh), .dq_out(dq_out), .dq_oe(dq_oe),
        .dq_rise(dq_sample), .dq_fall(dq_sample)
    );
    wire unused = ^{audio_valid, unit0_size};
endmodule
