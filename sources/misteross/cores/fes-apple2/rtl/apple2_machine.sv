// SPDX-License-Identifier: GPL-2.0-or-later
// FES Apple II machine: an Apple II+ class motherboard in the 52.224 MHz
// system domain.
//
// The NMOS 6502 runs from a fractional ~1.0205 MHz clock enable (the original
// 14.31818 MHz x 65/912 rate) with its RDY input used as that enable. The core
// presents an address in one enabled cycle and consumes data on the next, so
// the motherboard registers each bus cycle at the enable, performs side
// effects on the following STROBE clock and holds the read data stable until
// the next enable.
//
// Memory: 48 KiB RAM, a built-in 16 KiB language card (slot 0 switches
// $C080-$C08F), and a 16 KiB linked firmware window whose $D000-$FFFF part is
// the motherboard ROM. Slots 1-7 share one registered request word
// (apple2_bus.vh) with per-slot DEVSEL/IOSEL and a shared IOSTROBE. A built-in
// card may take its $Cn00 page from the linked firmware window instead of
// carrying its own ROM (BUILTIN_ROM_SLOTS). Video is scanned from RAM port B
// in the HDMI domain by apple2_video; this module only publishes the soft
// switches.
`include "apple2_bus.vh"

module apple2_machine #(
    parameter [7:0] BUILTIN_ROM_SLOTS = 8'b0100_0000,
    parameter integer SYSTEM_CLOCK_HZ = 52_224_000,
    parameter integer CPU_CLOCK_HZ = 1_020_484
) (
    input  wire        clk_sys,
    input  wire        reset,              // power-on style reset (host hold)
    input  wire        reset_key,          // Ctrl-Reset: 6502/soft-switch reset, RAM kept

    // ASCII keyboard: key_event pulses with the 7-bit code to latch at $C000.
    input  wire        key_event,
    input  wire [6:0]  key_code,
    // Game I/O: three push buttons and four paddle positions (0..255).
    input  wire [2:0]  buttons,
    input  wire [31:0] paddles,
    input  wire        cassette_in,

    // Slot bus. The request is common; selects are per slot 1..7 (bit n).
    output reg  [`A2_BUS_REQ-1:0] slot_request,
    output reg  [7:0]  slot_devsel,
    output reg  [7:0]  slot_iosel,
    input  wire [`A2_BUS_RSP*8-1:0] slot_response,

    // Video: soft switches (system domain) and the pixel-domain RAM port.
    output reg         video_text,
    output reg         video_mixed,
    output reg         video_page2,
    output reg         video_hires,
    output reg  [3:0]  annunciators,
    input  wire        video_clk,
    input  wire [15:0] video_addr,
    output wire [7:0]  video_data,

    output reg         speaker,
    output reg         cassette_out,
    output wire signed [15:0] slot_audio,

    // Diagnostics for simulation.
    output wire [15:0] debug_pc_addr,
    output wire        cpu_cycle,
    output wire [7:0]  debug_bus_data,
    output wire        debug_bus_write
);
    localparam integer PHASE_BITS = 27;

    // ------------------------------------------------------------------
    // CPU clock enable. cycle_clock counts system clocks since the enable so
    // the bus can place STROBE, the mid-cycle Q3 pulse and the read sample.
    // ------------------------------------------------------------------
    reg [PHASE_BITS-1:0] cpu_phase = 0;
    wire [PHASE_BITS:0] cpu_phase_next = {1'b0, cpu_phase} + CPU_CLOCK_HZ;
    wire cpu_ce = cpu_phase_next >= SYSTEM_CLOCK_HZ;
    always @(posedge clk_sys)
        cpu_phase <= cpu_ce ? PHASE_BITS'(cpu_phase_next - SYSTEM_CLOCK_HZ)
                            : cpu_phase_next[PHASE_BITS-1:0];
    reg [5:0] cycle_clock = 0;
    always @(posedge clk_sys)
        if (cpu_ce) cycle_clock <= 6'd0;
        else if (cycle_clock != 6'd63) cycle_clock <= cycle_clock + 6'd1;
    // Latched request is valid from cycle_clock 0 (the clock after cpu_ce).
    wire bus_strobe = cycle_clock == 6'd0;
    wire bus_mid = cycle_clock == 6'd25;
    wire bus_sample = cycle_clock == 6'd16;

    // ------------------------------------------------------------------
    // Reset: host reset initializes the machine; Ctrl-Reset is the backplane
    // RESET line (6502 and cards), RAM and II/II+ soft switches kept. Unlike
    // the IIe, the motherboard latches and language card have no RESET input.
    // Hold the line throughout the key press, then stretch its release so
    // even a short press gives every card a few CPU cycles of RESET.
    // ------------------------------------------------------------------
    reg [3:0] reset_hold = 4'hf;
    always @(posedge clk_sys) begin
        if (reset || reset_key)
            reset_hold <= 4'hf;
        else if (cpu_ce && reset_hold != 4'd0)
            reset_hold <= reset_hold - 4'd1;
    end
    wire system_reset = reset || reset_hold != 4'd0;

    // The core's reset sequence writes three stack bytes where the NMOS part
    // performs reads. Suppress those writes so RESET never alters RAM.
    reg [1:0] reset_writes = 2'd3;
    always @(posedge clk_sys)
        if (system_reset) reset_writes <= 2'd3;
        else if (cpu_ce && reset_writes != 2'd0) reset_writes <= reset_writes - 2'd1;

    wire [15:0] cpu_ab;
    wire [7:0] cpu_do;
    wire cpu_we;
    reg [7:0] cpu_di = 8'hff;
    wire slot_irq;
    wire slot_nmi;

    cpu6502 cpu (
        .clk(clk_sys), .reset(system_reset), .AB(cpu_ab), .DI(cpu_di),
        .DO(cpu_do), .WE(cpu_we), .IRQ(slot_irq), .NMI(slot_nmi), .RDY(cpu_ce)
    );

    reg [15:0] bus_addr = 16'h0000;
    reg [7:0]  bus_wdata = 8'h00;
    reg        bus_we = 1'b0;
    always @(posedge clk_sys)
        if (cpu_ce) begin
            bus_addr <= cpu_ab;
            bus_wdata <= cpu_do;
            bus_we <= cpu_we && reset_writes == 2'd0 && !system_reset;
        end

    // ------------------------------------------------------------------
    // Address decode of the held cycle.
    // ------------------------------------------------------------------
    wire sel_ram = bus_addr < 16'hC000;
    wire sel_io = bus_addr[15:8] == 8'hC0;
    wire sel_kbd = sel_io && bus_addr[7:4] == 4'h0;
    wire sel_kbd_clear = sel_io && bus_addr[7:4] == 4'h1;
    wire sel_cass_out = sel_io && bus_addr[7:4] == 4'h2;
    wire sel_speaker = sel_io && bus_addr[7:4] == 4'h3;
    wire sel_switch = sel_io && bus_addr[7:4] == 4'h5;
    wire sel_input = sel_io && bus_addr[7:4] == 4'h6;
    wire sel_ptrig = sel_io && bus_addr[7:4] == 4'h7;
    wire sel_lc = sel_io && bus_addr[7:4] == 4'h8;
    wire sel_devsel = sel_io && bus_addr[7] && bus_addr[6:4] != 3'd0;
    wire sel_iosel = bus_addr[15:11] == 5'b11000 && bus_addr[10:8] != 3'd0;
    wire sel_iostrobe = bus_addr[15:11] == 5'b11001;
    wire sel_high = bus_addr[15:12] >= 4'hD;
    wire [2:0] io_slot = sel_iosel ? bus_addr[10:8] : bus_addr[6:4];

    // ------------------------------------------------------------------
    // Slot bus request. The request word is registered from the held cycle,
    // so cards see it one clock after STROBE is formed here.
    // ------------------------------------------------------------------
    always @(posedge clk_sys) begin
        slot_request <= 32'd0;
        slot_request[`A2_BUS_A] <= bus_addr;
        slot_request[`A2_BUS_D] <= bus_wdata;
        slot_request[`A2_BUS_READ] <= !bus_we;
        slot_request[`A2_BUS_STROBE] <= bus_strobe;
        slot_request[`A2_BUS_Q3] <= bus_strobe || bus_mid;
        slot_request[`A2_BUS_RESET] <= system_reset;
        slot_request[`A2_BUS_IOSTROBE] <= sel_iostrobe;
        slot_devsel <= 8'd0;
        slot_iosel <= 8'd0;
        if (sel_devsel) slot_devsel[io_slot] <= 1'b1;
        if (sel_iosel) slot_iosel[io_slot] <= 1'b1;
    end

    // Response gather. Slot 0 is the built-in language card and never drives.
    reg [7:0] slot_data;
    reg slot_drive;
    reg irq_any;
    reg nmi_any;
    reg inh_any;
    integer s;
    always @* begin
        slot_data = 8'h00;
        slot_drive = 1'b0;
        irq_any = 1'b0;
        nmi_any = 1'b0;
        inh_any = 1'b0;
        for (s = 1; s < 8; s = s + 1) begin
            if (slot_response[s*`A2_BUS_RSP + `A2_BUS_DRIVE]) begin
                slot_data = slot_data | slot_response[s*`A2_BUS_RSP +: 8];
                slot_drive = 1'b1;
            end
            irq_any = irq_any | slot_response[s*`A2_BUS_RSP + `A2_BUS_IRQ];
            nmi_any = nmi_any | slot_response[s*`A2_BUS_RSP + `A2_BUS_NMI];
            inh_any = inh_any | slot_response[s*`A2_BUS_RSP + `A2_BUS_INH];
        end
    end
    assign slot_irq = irq_any;
    assign slot_nmi = nmi_any;

    // Card PCM is summed through a registered adder tree (audio latency is
    // irrelevant) and saturated to 16 bits.
    function signed [18:0] pcm;
        input integer slot;
        pcm = {{3{slot_response[slot*`A2_BUS_RSP + 27]}}, slot_response[slot*`A2_BUS_RSP + 12 +: 16]};
    endfunction
    reg signed [18:0] audio_pair [0:3];
    reg signed [18:0] audio_half [0:1];
    reg signed [18:0] audio_sum = 19'sd0;
    always @(posedge clk_sys) begin
        audio_pair[0] <= pcm(1) + pcm(2);
        audio_pair[1] <= pcm(3) + pcm(4);
        audio_pair[2] <= pcm(5) + pcm(6);
        audio_pair[3] <= pcm(7);
        audio_half[0] <= audio_pair[0] + audio_pair[1];
        audio_half[1] <= audio_pair[2] + audio_pair[3];
        audio_sum <= audio_half[0] + audio_half[1];
    end
    assign slot_audio = audio_sum > 19'sd32767 ? 16'sh7fff :
                        audio_sum < -19'sd32768 ? -16'sh8000 : audio_sum[15:0];

    // ------------------------------------------------------------------
    // Language card: RAM bank select for $D000-$FFFF.
    // ------------------------------------------------------------------
    reg lc_bank2 = 1'b1;
    reg lc_read_ram = 1'b0;
    reg lc_write_ram = 1'b1;
    reg lc_prewrite = 1'b0;

    // Physical RAM address: bank 1 of $D000-$DFFF lives in the unused
    // $C000-$CFFF RAM page.
    wire [15:0] ram_addr = (bus_addr[15:12] == 4'hD && !lc_bank2) ?
                           {4'hC, bus_addr[11:0]} : bus_addr;
    wire ram_write = bus_strobe && bus_we &&
                     (sel_ram || (sel_high && lc_write_ram && !inh_any));
    wire [7:0] ram_q;
    apple2_ram main_ram (
        .clk_a(clk_sys), .addr_a(ram_addr), .wdata_a(bus_wdata), .we_a(ram_write),
        .q_a(ram_q), .clk_b(video_clk), .addr_b(video_addr), .q_b(video_data)
    );

    // Linked firmware window: offsets $1000-$3FFF for $D000-$FFFF, and a
    // built-in card's $Cn00 page at offset $0n00.
    wire builtin_rom_page = sel_iosel && BUILTIN_ROM_SLOTS[io_slot];
    wire [13:0] rom_addr = builtin_rom_page ? {3'b000, bus_addr[10:0]} : bus_addr[13:0];
    wire [7:0] rom_q;
    apple2_rom rom (.clk(clk_sys), .address(rom_addr), .data(rom_q));

    // ------------------------------------------------------------------
    // Motherboard I/O.
    // ------------------------------------------------------------------
    reg [6:0] kbd_code = 7'h00;
    reg kbd_strobe = 1'b0;
    reg [11:0] paddle_timer [0:3];
    reg [2:0] buttons_s = 3'b000;
    reg cassette_s = 1'b0;
    always @(posedge clk_sys) begin
        buttons_s <= buttons;
        cassette_s <= cassette_in;
    end

    integer p;
    always @(posedge clk_sys) begin
        if (key_event) begin
            kbd_code <= key_code;
            kbd_strobe <= 1'b1;
        end
        if (reset) begin
            kbd_strobe <= 1'b0;
            speaker <= 1'b0;
            cassette_out <= 1'b0;
        end
        if (reset) begin
            video_text <= 1'b1;
            video_mixed <= 1'b0;
            video_page2 <= 1'b0;
            video_hires <= 1'b0;
            annunciators <= 4'b0000;
            lc_bank2 <= 1'b1;
            lc_read_ram <= 1'b0;
            lc_write_ram <= 1'b1;
            lc_prewrite <= 1'b0;
            for (p = 0; p < 4; p = p + 1)
                paddle_timer[p] <= 12'd0;
        end else begin
            if (cpu_ce)
                for (p = 0; p < 4; p = p + 1)
                    if (paddle_timer[p] != 12'd0)
                        paddle_timer[p] <= paddle_timer[p] - 12'd1;
            if (bus_strobe && !system_reset) begin
                if (sel_kbd_clear && !key_event)
                    kbd_strobe <= 1'b0;
                if (sel_cass_out)
                    cassette_out <= !cassette_out;
                if (sel_speaker)
                    speaker <= !speaker;
                if (sel_switch) begin
                    case (bus_addr[3:1])
                        3'd0: video_text <= bus_addr[0];
                        3'd1: video_mixed <= bus_addr[0];
                        3'd2: video_page2 <= bus_addr[0];
                        3'd3: video_hires <= bus_addr[0];
                        default: annunciators[bus_addr[2:1]] <= bus_addr[0];
                    endcase
                end
                if (sel_ptrig)
                    // PREAD counts 11-cycle loop iterations per unit.
                    for (p = 0; p < 4; p = p + 1)
                        paddle_timer[p] <= {paddles[p*8 +: 8], 3'b000} +
                                           {3'b000, paddles[p*8 +: 8], 1'b0} +
                                           {4'b0000, paddles[p*8 +: 8]} + 12'd8;
                if (sel_lc) begin
                    lc_bank2 <= !bus_addr[3];
                    lc_read_ram <= bus_addr[1:0] == 2'b00 || bus_addr[1:0] == 2'b11;
                    if (!bus_addr[0]) begin
                        lc_prewrite <= 1'b0;
                        lc_write_ram <= 1'b0;
                    end else if (!bus_we) begin
                        if (lc_prewrite)
                            lc_write_ram <= 1'b1;
                        lc_prewrite <= 1'b1;
                    end else begin
                        lc_prewrite <= 1'b0;
                    end
                end
            end
        end
    end

    reg input_bit;
    always @* begin
        case (bus_addr[2:0])
            3'd0: input_bit = cassette_s;
            3'd1: input_bit = buttons_s[0];
            3'd2: input_bit = buttons_s[1];
            3'd3: input_bit = buttons_s[2];
            3'd4: input_bit = paddle_timer[0] != 12'd0;
            3'd5: input_bit = paddle_timer[1] != 12'd0;
            3'd6: input_bit = paddle_timer[2] != 12'd0;
            default: input_bit = paddle_timer[3] != 12'd0;
        endcase
    end

    // ------------------------------------------------------------------
    // CPU read data. Sampled once per cycle, after RAM/ROM (two clocks) and
    // the registered slot response (about four clocks) have settled.
    // Unclaimed slot space and write-only switches read as $FF.
    // ------------------------------------------------------------------
    reg [7:0] read_data;
    always @* begin
        read_data = 8'hff;
        if (sel_ram)
            read_data = ram_q;
        else if (sel_kbd || sel_kbd_clear)
            read_data = {kbd_strobe, kbd_code};
        else if (sel_input)
            read_data = {input_bit, 7'h7f};
        else if (builtin_rom_page)
            read_data = rom_q;
        else if (sel_high && !inh_any)
            read_data = lc_read_ram ? ram_q : rom_q;
        if ((sel_devsel || sel_iosel || sel_iostrobe || (sel_high && inh_any)) && slot_drive)
            read_data = slot_data;
    end

    always @(posedge clk_sys)
        if (bus_sample)
            cpu_di <= read_data;

    assign debug_pc_addr = bus_addr;
    assign cpu_cycle = bus_strobe;
    assign debug_bus_data = bus_we ? bus_wdata : read_data;
    assign debug_bus_write = bus_we;

    initial begin
        video_text = 1'b1;
        video_mixed = 1'b0;
        video_page2 = 1'b0;
        video_hires = 1'b0;
        annunciators = 4'b0000;
        speaker = 1'b0;
        cassette_out = 1'b0;
        slot_request = 32'd0;
        slot_devsel = 8'd0;
        slot_iosel = 8'd0;
        for (p = 0; p < 4; p = p + 1)
            paddle_timer[p] = 12'd0;
    end
endmodule
