// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum machine: NMOS Z80, ULA, memory map, I/O and edge sockets.
//
// The build model (spectrum_model.vh) selects a 48K, 128K/+2, +2A/+3 or
// Pentagon 128. The ULA owns the T-state clock and stretches the CPU clock for
// contended accesses. Memory is ROM pages plus 16 KiB RAM banks:
//   48K       ROM 0, banks 5, 2, 0.
//   128K/+2   $7FFD (A15=0, A1=0): bits 2:0 bank at $C000, bit 3 screen in
//             bank 7, bit 4 ROM page, bit 5 locks paging until reset.
//   +2A/+3    $7FFD (A15:A14=01, A1=0) and $1FFD (A15:A12=0001, A1=0): bit 0
//             all-RAM modes {0,1,2,3} {4,5,6,7} {4,5,6,3} {4,7,6,3} chosen by
//             bits 2:1; otherwise bit 2 is the high ROM page bit.
//   Pentagon  $7FFD as the 128K, no contention and no floating bus.
// Port $FE (A0 low) is the ULA. Port $1F (A7:A5 low) is a built-in Kempston
// joystick. On 128K-class models $FFFD/$BFFD (A15, A14, A1) is the AY-3-8912.
// Other reads return the ULA's floating bus where the model has one. The tape
// deck plays a PZX image that the shell streams from its media store; a
// restore stream in the same unit loads a snapshot instead (spectrum_snapshot).
// While the deck plays (Fuse's loader detection starts and stops it), T
// states take 5 clocks instead of 14 or 15: the machine loads about 3 times
// faster and is otherwise unchanged, and the shell mutes its sound.
//
// The Pentagon has a Beta 128 disk interface (spectrum_beta) and a 48 KiB
// firmware whose third page is TR-DOS. As in Fuse, TR-DOS pages in over the
// ROM on an opcode fetch from $3D00-$3DFF and out on one from $4000 up, both
// only while $7FFD selects 48 BASIC; while it is paged, ports $1F, $3F, $5F,
// $7F and $FF are the Beta's. A reset with a disk inserted selects 48 BASIC
// and pages TR-DOS in, so TR-DOS boots the disk as Fuse's disk autoload does;
// without one the 128 ROM starts with TR-DOS paged out, as Fuse runs it. The
// host's launch disk resets the machine when it becomes ready (spectrum_beta
// boot). A snapshot restore starts with TR-DOS paged out.
//
// The +2A/+3 has a uPD765A (spectrum_upd765): $2FFD (A15:A12 0010, A1 0)
// reads its main status, $3FFD (0011) is its data register, and $1FFD bit 3
// runs the drive motor. A launch disk resets the machine, and once the ROM's
// menu scans every keyboard half-row in a frame, ENTER is held through the
// third frame after it, so the menu's Loader boots the disk as Fuse's
// phantom typist does.
`include "spectrum_bus.vh"
`include "spectrum_model.vh"

module spectrum_machine #(
    parameter ROM_FILE = "build/diagnostics/fes-spectrum/firmware.hex"
) (
    input  wire        clk_sys,
    input  wire        reset,
    input  wire [39:0] matrix,
    input  wire [4:0]  kempston,
    input  wire        tape_ready,
    input  wire [24:0] tape_size,
    input  wire        disk_ready,
    input  wire [22:0] disk_size,
    output wire        disk_fetch,
    output wire [21:0] disk_fetch_addr,
    input  wire        disk_fetch_ready,
    input  wire [7:0]  disk_fetch_data,
    output wire        tape_stream_enable,
    input  wire        tape_stream_valid,
    input  wire [7:0]  tape_stream_data,
    output wire        tape_stream_take,
    output wire        tape_playing,
    // Turbo: T states take 5 clocks while the deck plays, or always with
    // turbo_always (the benches' check that turbo changes nothing in T states).
    input  wire        turbo_always,
    output wire        turbo,
    output wire [`SP_BUS_REQ-1:0] bus_request,
    input  wire [`SP_BUS_RSP-1:0] response1,
    output wire        pixel_we,
    output wire [7:0]  pixel_row,
    output wire [5:0]  pixel_chunk,
    output wire [15:0] pixel_data,
    output wire        frame_done,
    output wire        tick,
    output wire        speaker,
    output wire        mic,
    output wire        ear,
    output wire [7:0]  ay_pcm,
    output wire signed [15:0] slot_audio,
    output wire        cpu_retired, cpu_illegal, cpu_halted, frame_int_n,
    output wire [15:0] cpu_pc
);
    localparam integer MODEL = `SPECTRUM_MODEL;
    localparam bit PAGED = MODEL != 0;
    localparam bit AMSTRAD = MODEL == 2;
    localparam bit HAS_AY = MODEL != 0;
    localparam [1:0] STYLE = MODEL == 3 ? 2'd0 : MODEL == 2 ? 2'd2 : 2'd1;
    localparam bit BETA = MODEL == 3;
    localparam bit UPD765 = MODEL == 2;

    // ------------------------------------------------------------ CPU and ULA
    wire cpu_m1_n, cpu_mreq_n, cpu_iorq_n, cpu_rd_n, cpu_wr_n, cpu_rfsh_n, cpu_halt_n;
    wire [15:0] cpu_a;
    wire [7:0] cpu_do;
    wire [7:0] cpu_di;
    wire [15:0] cpu_bc;
    wire cycle_present;
    wire [2:0] cycle_kind;
    wire [5:0] cycle_t;
    wire cpu_ce_p, cpu_ce_n, tick_p, tick_n, int_n, contend_request;
    wire nmi = response1[`SP_BUS_NMI];
    wire card_wait = response1[`SP_BUS_WAIT];
    // A snapshot restore resets the machine (not the shell) before it runs.
    wire restore_reset, restoring, restore_wait;
    wire disk_boot;
    wire machine_reset = reset | restore_reset | disk_boot;

    fes_z80_nmos cpu (
        .clk(clk_sys), .reset(machine_reset), .ce_p(cpu_ce_p), .ce_n(cpu_ce_n),
        .wait_n(~(card_wait | restore_wait)), .int_n(int_n), .nmi_n(~nmi), .busrq_n(1'b1), .din(cpu_di),
        .m1_n(cpu_m1_n), .mreq_n(cpu_mreq_n), .iorq_n(cpu_iorq_n), .rd_n(cpu_rd_n),
        .wr_n(cpu_wr_n), .rfsh_n(cpu_rfsh_n), .halt_n(cpu_halt_n), .busak_n(),
        .a(cpu_a), .dout(cpu_do), .illegal(cpu_illegal), .retired(cpu_retired),
        .retire_pc(), .debug_pc(cpu_pc), .debug_sp(), .debug_af(), .debug_bc(cpu_bc), .debug_de(),
        .debug_hl(), .debug_ix(), .debug_iy(), .debug_ir(), .debug_iff(),
        .cycle_present(cycle_present), .cycle_kind(cycle_kind), .cycle_t(cycle_t)
    );
    assign cpu_halted = ~cpu_halt_n;
    assign frame_int_n = int_n;
    assign tick = tick_p;

    // --------------------------------------------------------------- paging
    reg [7:0] port_7ffd = 8'h00;
    reg [3:0] port_1ffd = 4'b0000;   // bit 3: the +3's disk motor
    wire paging_locked = port_7ffd[5];
    // TR-DOS paging. dos_entry is the fetch that pages it in, so that fetch
    // already reads TR-DOS.
    reg dos_active = 1'b0;
    wire opcode_fetch = ~cpu_m1_n & ~cpu_mreq_n;
    wire dos_entry = BETA && opcode_fetch && cpu_a[15:8] == 8'h3d && port_7ffd[4];
    wire dos_rom = BETA && (dos_active || dos_entry);

    // Memory slot a[15:14] -> {ROM, ROM page or RAM bank}.
    function [3:0] slot_map(input [1:0] slot);
        reg [2:0] special;
        begin
            if (AMSTRAD && port_1ffd[0]) begin
                case (port_1ffd[2:1])
                    2'd0: special = {1'b0, slot};
                    2'd1: special = {1'b1, slot};
                    2'd2: special = slot == 2'd3 ? 3'd3 : {1'b1, slot};
                    default: special = slot == 2'd3 ? 3'd3 : slot == 2'd1 ? 3'd7 : {1'b1, slot};
                endcase
                slot_map = {1'b0, special};
            end else begin
                case (slot)
                    2'd0: slot_map = dos_rom ? 4'b1010 :
                                     {1'b1, 1'b0, AMSTRAD ? port_1ffd[2] : 1'b0, PAGED ? port_7ffd[4] : 1'b0};
                    2'd1: slot_map = {1'b0, 3'd5};
                    2'd2: slot_map = {1'b0, 3'd2};
                    default: slot_map = {1'b0, PAGED ? port_7ffd[2:0] : 3'd0};
                endcase
            end
        end
    endfunction
    function bank_contended(input [3:0] mapped);
        bank_contended = !mapped[3] && (MODEL == 0 ? mapped[2:0] == 3'd5 :
                                        MODEL == 1 ? mapped[0] :
                                        MODEL == 2 ? mapped[2] : 1'b0);
    endfunction
    wire [3:0] cpu_map = slot_map(cpu_a[15:14]);
    wire cpu_rom = cpu_map[3];

    spectrum_contention #(.STYLE(STYLE)) contention (
        .cycle_present(cycle_present), .cycle_kind(cycle_kind), .cycle_t(cycle_t),
        .a(cpu_a), .a_contended(bank_contended(cpu_map)),
        .mem_point(), .ula_io_point(), .request(contend_request)
    );

    wire [13:0] screen_addr;
    wire [7:0] screen5_data, screen7_data, floating;
    reg [2:0] border_q = 3'd7;
    assign turbo = turbo_always | tape_playing;
    spectrum_ula #(.MODEL(MODEL)) ula (
        .clk(clk_sys), .reset(machine_reset), .turbo(turbo), .tick_p(tick_p), .tick_n(tick_n),
        .contend_request(contend_request), .cpu_ce_p(cpu_ce_p), .cpu_ce_n(cpu_ce_n),
        .int_n(int_n), .screen7(port_7ffd[3]), .screen_addr(screen_addr),
        .screen5_data(screen5_data), .screen7_data(screen7_data),
        .border(border_q), .floating(floating),
        .pixel_we(pixel_we), .pixel_row(pixel_row), .pixel_chunk(pixel_chunk),
        .pixel_data(pixel_data), .frame_done(frame_done)
    );

    // ----------------------------------------------------------- edge bus
    reg rd_q = 1'b1;
    reg wr_q = 1'b1;
    always @(posedge clk_sys) begin
        rd_q <= cpu_rd_n;
        wr_q <= cpu_wr_n;
    end
    wire strobe = (rd_q & ~cpu_rd_n) | (wr_q & ~cpu_wr_n);
    assign bus_request[`SP_BUS_A] = cpu_a;
    assign bus_request[`SP_BUS_D] = cpu_do;
    assign bus_request[`SP_BUS_MREQ] = ~cpu_mreq_n;
    assign bus_request[`SP_BUS_IORQ] = ~cpu_iorq_n;
    assign bus_request[`SP_BUS_RD] = ~cpu_rd_n;
    assign bus_request[`SP_BUS_WR] = ~cpu_wr_n;
    assign bus_request[`SP_BUS_M1] = ~cpu_m1_n;
    assign bus_request[`SP_BUS_STROBE] = strobe;
    assign bus_request[`SP_BUS_RESET] = machine_reset;
    assign bus_request[`SP_BUS_RESERVED] = 1'b0;

    wire [27:0] chosen = response1[`SP_BUS_DRIVE] ? response1 : 28'd0;
    wire romcs = response1[`SP_BUS_ROMCS];

    // The card's PCM plays whether or not it drives the CPU bus.
    assign slot_audio = response1[`SP_BUS_AUDIO];

    // --------------------------------------------------------------- memory
    wire [7:0] rom_data;
    spectrum_rom #(.ROM_FILE(ROM_FILE)) rom (
        .clk(clk_sys), .address({cpu_map[1:0], cpu_a[13:0]}), .data(rom_data)
    );

    wire mem_write = ~cpu_mreq_n & ~cpu_wr_n & ~cpu_rom;
    reg mem_write_q = 1'b0;
    always @(posedge clk_sys) mem_write_q <= mem_write;
    wire [7:0] ram_data;
    spectrum_ram #(.BANKS(PAGED ? 8 : 3)) ram (
        .clk(clk_sys), .cpu_we(mem_write & ~mem_write_q),
        .cpu_bank(cpu_map[2:0]), .cpu_offset(cpu_a[13:0]), .cpu_wdata(cpu_do),
        .cpu_rdata(ram_data), .ula_offset(screen_addr),
        .ula5_rdata(screen5_data), .ula7_rdata(screen7_data)
    );

    // ------------------------------------------------------------------ I/O
    wire io = ~cpu_iorq_n & cpu_m1_n;
    wire io_write = io & ~cpu_wr_n;
    reg io_write_q = 1'b0;
    always @(posedge clk_sys) io_write_q <= io_write;
    wire io_write_strobe = io_write & ~io_write_q;
    wire ula_port = ~cpu_a[0];
    wire port_7ffd_hit = AMSTRAD ? (cpu_a[15:14] == 2'b01 && !cpu_a[1])
                                 : (PAGED && !cpu_a[15] && !cpu_a[1]);
    wire port_1ffd_hit = AMSTRAD && cpu_a[15:12] == 4'b0001 && !cpu_a[1];
    wire ay_select_hit = HAS_AY && cpu_a[15:14] == 2'b11 && !cpu_a[1];
    wire ay_data_hit = HAS_AY && cpu_a[15:14] == 2'b10 && !cpu_a[1];
    wire kempston_hit = cpu_a[7:5] == 3'b000;
    wire fdc_status_hit = UPD765 && (cpu_a & 16'hf002) == 16'h2000;
    wire fdc_data_hit = UPD765 && (cpu_a & 16'hf002) == 16'h3000;
    wire beta_hit = dos_active && (cpu_a[7:0] == 8'h1f || cpu_a[7:0] == 8'h3f ||
                                   cpu_a[7:0] == 8'h5f || cpu_a[7:0] == 8'h7f ||
                                   cpu_a[7:0] == 8'hff);

    reg speaker_q = 1'b0;
    reg mic_q = 1'b0;
    reg ear_default = 1'b0;   // last OUT bit 4: Issue 3 / 128K read-back
    always @(posedge clk_sys) begin
        if (machine_reset) begin
            border_q <= 3'd7;
            speaker_q <= 1'b0;
            mic_q <= 1'b0;
            ear_default <= 1'b0;
            // A reset with a disk inserted boots it through TR-DOS from 48 BASIC.
            port_7ffd <= BETA && disk_ready && !restore_reset ? 8'h10 : 8'h00;
            port_1ffd <= 4'b0000;
            dos_active <= BETA && disk_ready && !restore_reset;
        end else begin
            if (dos_entry) dos_active <= 1'b1;
            else if (opcode_fetch && cpu_a[15:14] != 2'b00 && port_7ffd[4]) dos_active <= 1'b0;
            if (io_write_strobe && ula_port) begin
                border_q <= cpu_do[2:0];
                mic_q <= cpu_do[3];
                speaker_q <= cpu_do[4];
                ear_default <= cpu_do[4];
            end
            if (io_write_strobe && port_7ffd_hit && !paging_locked)
                port_7ffd <= cpu_do;
            if (io_write_strobe && port_1ffd_hit && !paging_locked)
                port_1ffd <= cpu_do[3:0];
            // The 128K/+2 latches the data bus of any read that decodes as
            // $7FFD into the paging register.
            if (MODEL == 1 && cpu_ce_n && cycle_present && cycle_kind == 3'd3 &&
                cycle_t == 6'd4 && !cpu_a[15] && !cpu_a[1] && !paging_locked)
                port_7ffd <= cpu_di;
        end
    end
    assign speaker = speaker_q;
    assign mic = mic_q;

    // One clock per CPU read of a ULA port, for the deck's loader detection.
    wire ula_reading = io & ~cpu_rd_n & ula_port;
    reg ula_reading_q = 1'b0;
    always @(posedge clk_sys) ula_reading_q <= ula_reading;

    wire mem_read = ~cpu_mreq_n & ~cpu_rd_n;
    wire deck_ready, deck_stream_enable, deck_stream_take;
    spectrum_snapshot #(.MODEL(8'(MODEL))) snapshot (
        .clk(clk_sys), .reset(reset), .ready(tape_ready), .size(tape_size),
        .stream_enable(tape_stream_enable), .stream_valid(tape_stream_valid),
        .stream_data(tape_stream_data), .stream_take(tape_stream_take),
        .deck_ready(deck_ready), .deck_stream_enable(deck_stream_enable),
        .deck_stream_take(deck_stream_take), .mem_read(mem_read),
        .machine_reset(restore_reset), .restoring(restoring), .restore_wait(restore_wait)
    );

    wire tape_ear;
    spectrum_tape #(.IS_48K(MODEL == 0)) tape (
        .clk(clk_sys), .reset(reset), .tick(tick_p), .ready(deck_ready), .size(tape_size),
        .stream_enable(deck_stream_enable), .stream_valid(tape_stream_valid & deck_ready),
        .stream_data(tape_stream_data), .stream_take(deck_stream_take),
        .ula_read(ula_reading & ~ula_reading_q), .cpu_b(cpu_bc[15:8]),
        .ear(tape_ear), .playing(tape_playing), .underruns()
    );
    assign ear = tape_ear;

    wire [7:0] ay_read_data;
    generate if (HAS_AY) begin : psg
        // AY clock is half the CPU clock, from the uncontended T-state edge.
        reg ay_div = 1'b0;
        always @(posedge clk_sys) if (tick_p) ay_div <= ~ay_div;
        zonx_ay ay (
            .clk(clk_sys), .reset_n(~machine_reset), .chip_ce(tick_p && ay_div),
            .address_write(io_write_strobe && ay_select_hit),
            .data_write(io_write_strobe && ay_data_hit),
            .data(cpu_do), .read_data(ay_read_data), .pcm(ay_pcm), .ym_levels()
        );
    end else begin : no_psg
        assign ay_read_data = 8'hff;
        assign ay_pcm = 8'd0;
    end endgenerate

    // Port $FE reads keys in the half-rows selected by A15:A8. Bit 6 is EAR;
    // with no tape signal it reads back OUT bit 4 (Issue 3), or 0 on the +2A.
    wire [7:0] row_select = ~cpu_a[15:8];
    // The +3's launch typist (phantom_typist.c, Enter mode): wait for a frame
    // in which all eight half-rows are read, then press ENTER through the
    // third frame after it.
    typedef enum logic [1:0] {T_IDLE, T_WAIT, T_DELAY, T_PRESS} typist_t;
    typist_t typist = T_IDLE;
    reg [7:0] rows_read = 8'd0;
    reg typist_go = 1'b0;
    reg [1:0] typist_frames = 2'd0;
    reg int_q = 1'b1;
    wire frame_start = int_q && !int_n;
    wire one_row = row_select == 8'h01 || row_select == 8'h02 || row_select == 8'h04 || row_select == 8'h08 ||
                   row_select == 8'h10 || row_select == 8'h20 || row_select == 8'h40 || row_select == 8'h80;
    always @(posedge clk_sys) begin
        int_q <= int_n;
        if (reset || !UPD765) begin
            typist <= T_IDLE;
            typist_go <= 1'b0;
            rows_read <= 8'd0;
        end else begin
            if (disk_boot) begin
                typist <= T_WAIT;
                typist_go <= 1'b0;
            end else if (frame_start) begin
                rows_read <= 8'd0;
                case (typist)
                    T_WAIT: if (typist_go) begin
                        typist <= T_DELAY;
                        typist_frames <= 2'd2;
                    end
                    T_DELAY: if (typist_frames == 2'd1) typist <= T_PRESS;
                             else typist_frames <= typist_frames - 2'd1;
                    T_PRESS: typist <= T_IDLE;
                    default: ;
                endcase
            end else if (typist == T_WAIT && ula_reading && !ula_reading_q && one_row) begin
                rows_read <= rows_read | row_select;
                if ((rows_read | row_select) == 8'hff) typist_go <= 1'b1;
            end
        end
    end
    wire [39:0] keys_in = typist == T_PRESS ? matrix & ~(40'd1 << 30) : matrix;   // ENTER
    wire [4:0] keys =
        (row_select[0] ? keys_in[4:0] : 5'h1f) &
        (row_select[1] ? keys_in[9:5] : 5'h1f) &
        (row_select[2] ? keys_in[14:10] : 5'h1f) &
        (row_select[3] ? keys_in[19:15] : 5'h1f) &
        (row_select[4] ? keys_in[24:20] : 5'h1f) &
        (row_select[5] ? keys_in[29:25] : 5'h1f) &
        (row_select[6] ? keys_in[34:30] : 5'h1f) &
        (row_select[7] ? keys_in[39:35] : 5'h1f);
    wire ear_bit = (AMSTRAD ? 1'b0 : ear_default) ^ tape_ear;
    wire [7:0] ula_read = {1'b1, ear_bit, 1'b1, keys};
    // +2A/+3: only ports 1+4n up to $1000 see the floating bus. The
    // Pentagon returns the video data only on port $FF.
    wire amstrad_float_port = cpu_a <= 16'h1000 && cpu_a > 16'h0001 && cpu_a[1:0] == 2'b01;
    wire [7:0] unattached = MODEL == 3 ? (cpu_a[7:0] == 8'hff ? floating : 8'hff) :
                            AMSTRAD && !amstrad_float_port ? 8'hff : floating;
    wire io_read = io & ~cpu_rd_n;
    reg io_read_q = 1'b0;
    always @(posedge clk_sys) io_read_q <= io_read;

    wire [7:0] beta_data, fdc_data;
    generate if (BETA) begin : beta
        spectrum_beta disk (
            .clk(clk_sys), .power_reset(reset), .reset(machine_reset), .tick(tick_p), .port(cpu_a[7:0]),
            .write_strobe(io_write_strobe && beta_hit),
            .read_strobe(io_read && !io_read_q && beta_hit),
            .wdata(cpu_do), .rdata(beta_data), .disk_ready(disk_ready), .disk_bytes(disk_size),
            .fetch(disk_fetch), .fetch_addr(disk_fetch_addr),
            .fetch_ready(disk_fetch_ready), .fetch_data(disk_fetch_data), .boot(disk_boot)
        );
        assign fdc_data = 8'hff;
    end else if (UPD765) begin : upd765
        spectrum_upd765 disk (
            .clk(clk_sys), .power_reset(reset), .reset(machine_reset), .tick(tick_p),
            .motor(port_1ffd[3]), .data_port(fdc_data_hit),
            .write_strobe(io_write_strobe && fdc_data_hit),
            .read_strobe(io_read && !io_read_q && (fdc_status_hit || fdc_data_hit)),
            .wdata(cpu_do), .rdata(fdc_data), .disk_ready(disk_ready), .disk_bytes(disk_size),
            .fetch(disk_fetch), .fetch_addr(disk_fetch_addr),
            .fetch_ready(disk_fetch_ready), .fetch_data(disk_fetch_data), .boot(disk_boot)
        );
        assign beta_data = 8'hff;
    end else begin : no_disk
        assign beta_data = 8'hff;
        assign fdc_data = 8'hff;
        assign disk_fetch = 1'b0;
        assign disk_fetch_addr = 22'd0;
        assign disk_boot = 1'b0;
    end endgenerate
    wire [7:0] rom_mux = (romcs && chosen[`SP_BUS_DRIVE]) ? chosen[`SP_BUS_RDATA] :
                         romcs ? 8'hff : rom_data;
    wire [7:0] io_mux = chosen[`SP_BUS_DRIVE] ? chosen[`SP_BUS_RDATA] :
                        beta_hit ? beta_data :
                        fdc_status_hit || fdc_data_hit ? fdc_data :
                        ula_port ? ula_read :
                        kempston_hit ? {3'b000, kempston} :
                        ay_select_hit ? ay_read_data : unattached;
    assign cpu_di = mem_read ? (restoring ? tape_stream_data : cpu_rom ? rom_mux : ram_data) :
                    io_read ? io_mux : 8'hff;
endmodule
