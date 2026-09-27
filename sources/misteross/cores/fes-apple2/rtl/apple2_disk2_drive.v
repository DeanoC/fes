// SPDX-License-Identifier: GPL-2.0-or-later
// One 5.25-inch Disk II drive fed from a 143,360-byte DOS 3.3 order image.
//
// The head position is kept in quarter tracks. Every enabled magnet within
// three quarter tracks pulls the head one quarter track per evaluation, so
// overlapping phases settle between two magnets exactly as the stepper does,
// and ordinary DOS/ProDOS seeks arrive on whole tracks.
//
// While the motor runs, the drive emits one bit every four CPU cycles
// (4 us). The track is synthesised on the fly from the sector image with
// the standard 16-sector layout: 40 self-sync bytes before sector 0 and 20
// before each later sector, an address field (D5 AA 96, 4-and-4 volume 254,
// track, sector and checksum, DE AA EB), six sync bytes, and a 6-and-2 data
// field (D5 AA AD, 342 XOR-chained values plus checksum, DE AA EB).
// Self-sync bytes are FF followed by two zero bits. Physical sector p holds
// DOS logical sector DOS_SKEW[p]. Without a disk the drive emits LFSR noise,
// so software sees garbage nibbles rather than a silent latch. The image is
// read-only and write protect is always reported.
module apple2_disk2_drive (
    input  wire        clk,
    input  wire        reset,
    input  wire        cpu_ce,
    input  wire [3:0]  phases,
    input  wire        motor_on,
    input  wire        drive2,
    input  wire        disk_present,
    output reg  [17:0] media_addr,
    input  wire [7:0]  media_q,
    output reg         bit_ce,
    output reg         read_bit,
    output wire        write_protect,
    output wire [5:0]  track,
    output reg  [7:0]  quarter_track
);
    assign write_protect = 1'b1;
    localparam [7:0] QT_MAX = 8'd136;     // track 34
    localparam [7:0] VOLUME = 8'd254;

    // ------------------------------------------------------------------
    // Stepper. Magnet i attracts quarter-track positions 2i (mod 8).
    // ------------------------------------------------------------------
    reg [5:0] step_timer = 6'd0;
    reg signed [3:0] pull;
    integer m;
    reg [2:0] rel;
    always @* begin
        pull = 4'sd0;
        for (m = 0; m < 4; m = m + 1) begin
            rel = 3'(2 * m) - quarter_track[2:0];
            // rel in 0..7 is the forward distance; 4 is directly opposite.
            if (phases[m] && rel != 3'd0 && rel != 3'd4)
                pull = pull + ((rel < 3'd4) ? 4'sd1 : -4'sd1);
        end
    end
    always @(posedge clk) begin
        step_timer <= step_timer + 6'd1;
        if (reset)
            quarter_track <= 8'd0;
        else if (step_timer == 6'd0) begin
            if (pull > 0 && quarter_track < QT_MAX)
                quarter_track <= quarter_track + 8'd1;
            else if (pull < 0 && quarter_track != 8'd0)
                quarter_track <= quarter_track - 8'd1;
        end
    end
    wire [7:0] qt_round = quarter_track + 8'd1;
    assign track = qt_round[7:2];

    // ------------------------------------------------------------------
    // Bit clock: one bit bit_phase per four CPU cycles while the motor turns.
    // ------------------------------------------------------------------
    wire spinning = motor_on && !drive2;
    reg [1:0] bit_phase = 2'd0;
    always @(posedge clk) begin
        bit_ce <= 1'b0;
        if (cpu_ce && spinning) begin
            bit_phase <= bit_phase + 2'd1;
            bit_ce <= bit_phase == 2'd3;
        end
    end

    // ------------------------------------------------------------------
    // Track position of the NEXT nibble to generate.
    // ------------------------------------------------------------------
    localparam [1:0] F_SYNC = 2'd0, F_ADDR = 2'd1, F_DATA = 2'd2;
    reg [1:0] field = F_SYNC;
    reg [3:0] sector = 4'd0;
    reg [8:0] index = 9'd0;       // sync count down, or field byte index
    reg gap2 = 1'b0;              // sync run between address and data fields

    function [3:0] dos_skew;
        input [3:0] physical;
        case (physical)
            4'd0: dos_skew = 4'd0;   4'd1: dos_skew = 4'd7;
            4'd2: dos_skew = 4'd14;  4'd3: dos_skew = 4'd6;
            4'd4: dos_skew = 4'd13;  4'd5: dos_skew = 4'd5;
            4'd6: dos_skew = 4'd12;  4'd7: dos_skew = 4'd4;
            4'd8: dos_skew = 4'd11;  4'd9: dos_skew = 4'd3;
            4'd10: dos_skew = 4'd10; 4'd11: dos_skew = 4'd2;
            4'd12: dos_skew = 4'd9;  4'd13: dos_skew = 4'd1;
            4'd14: dos_skew = 4'd8;  default: dos_skew = 4'd15;
        endcase
    endfunction

    function [7:0] translate62;
        input [5:0] value;
        case (value)
            6'h00: translate62 = 8'h96; 6'h01: translate62 = 8'h97; 6'h02: translate62 = 8'h9a; 6'h03: translate62 = 8'h9b;
            6'h04: translate62 = 8'h9d; 6'h05: translate62 = 8'h9e; 6'h06: translate62 = 8'h9f; 6'h07: translate62 = 8'ha6;
            6'h08: translate62 = 8'ha7; 6'h09: translate62 = 8'hab; 6'h0a: translate62 = 8'hac; 6'h0b: translate62 = 8'had;
            6'h0c: translate62 = 8'hae; 6'h0d: translate62 = 8'haf; 6'h0e: translate62 = 8'hb2; 6'h0f: translate62 = 8'hb3;
            6'h10: translate62 = 8'hb4; 6'h11: translate62 = 8'hb5; 6'h12: translate62 = 8'hb6; 6'h13: translate62 = 8'hb7;
            6'h14: translate62 = 8'hb9; 6'h15: translate62 = 8'hba; 6'h16: translate62 = 8'hbb; 6'h17: translate62 = 8'hbc;
            6'h18: translate62 = 8'hbd; 6'h19: translate62 = 8'hbe; 6'h1a: translate62 = 8'hbf; 6'h1b: translate62 = 8'hcb;
            6'h1c: translate62 = 8'hcd; 6'h1d: translate62 = 8'hce; 6'h1e: translate62 = 8'hcf; 6'h1f: translate62 = 8'hd3;
            6'h20: translate62 = 8'hd6; 6'h21: translate62 = 8'hd7; 6'h22: translate62 = 8'hd9; 6'h23: translate62 = 8'hda;
            6'h24: translate62 = 8'hdb; 6'h25: translate62 = 8'hdc; 6'h26: translate62 = 8'hdd; 6'h27: translate62 = 8'hde;
            6'h28: translate62 = 8'hdf; 6'h29: translate62 = 8'he5; 6'h2a: translate62 = 8'he6; 6'h2b: translate62 = 8'he7;
            6'h2c: translate62 = 8'he9; 6'h2d: translate62 = 8'hea; 6'h2e: translate62 = 8'heb; 6'h2f: translate62 = 8'hec;
            6'h30: translate62 = 8'hed; 6'h31: translate62 = 8'hee; 6'h32: translate62 = 8'hef; 6'h33: translate62 = 8'hf2;
            6'h34: translate62 = 8'hf3; 6'h35: translate62 = 8'hf4; 6'h36: translate62 = 8'hf5; 6'h37: translate62 = 8'hf6;
            6'h38: translate62 = 8'hf7; 6'h39: translate62 = 8'hf9; 6'h3a: translate62 = 8'hfa; 6'h3b: translate62 = 8'hfb;
            6'h3c: translate62 = 8'hfc; 6'h3d: translate62 = 8'hfd; 6'h3e: translate62 = 8'hfe; default: translate62 = 8'hff;
        endcase
    endfunction

    // 6-and-2 auxiliary value: low two bits of three bytes, each bit-swapped.
    function [1:0] swap2;
        input [7:0] b;
        swap2 = {b[0], b[1]};
    endfunction

    // ------------------------------------------------------------------
    // Nibble generator: computes the next nibble while the current one
    // shifts out (a data nibble lasts 32 CPU cycles, ~1,600 system clocks).
    // ------------------------------------------------------------------
    localparam [2:0] G_IDLE = 3'd0, G_READ = 3'd1, G_WAIT = 3'd2, G_CAPTURE = 3'd3,
                     G_DONE = 3'd4;
    reg [2:0] gen_state = G_IDLE;
    reg next_valid = 1'b0;
    wire consume;                  // shifter takes next_nibble
    reg [7:0] next_nibble = 8'hff;
    reg next_sync = 1'b1;
    reg [5:0] prev_value = 6'd0;   // XOR chain of the data field
    reg [1:0] read_step = 2'd0;    // aux reads 0..2
    reg [1:0] wait_count = 2'd0;
    reg [5:0] aux_value = 6'd0;
    reg [5:0] gen_track = 6'd0;

    wire [8:0] n = index - 9'd3;   // data value index inside the data field
    wire [7:0] sector_offset;
    reg [7:0] byte_index;
    always @* begin
        if (n < 9'd86)
            byte_index = 8'(n) + (read_step == 2'd1 ? 8'd86 : read_step == 2'd2 ? 8'd172 : 8'd0);
        else
            byte_index = 8'(n - 9'd86);
    end
    wire [7:0] check = VOLUME ^ {2'b00, gen_track} ^ {4'd0, sector};

    reg [7:0] addr_nibble;
    always @* begin
        case (index[3:0])
            4'd0: addr_nibble = 8'hd5;
            4'd1: addr_nibble = 8'haa;
            4'd2: addr_nibble = 8'h96;
            4'd3: addr_nibble = {1'b1, VOLUME[7], 1'b1, VOLUME[5], 1'b1, VOLUME[3], 1'b1, VOLUME[1]};
            4'd4: addr_nibble = {1'b1, VOLUME[6], 1'b1, VOLUME[4], 1'b1, VOLUME[2], 1'b1, VOLUME[0]};
            4'd5: addr_nibble = {1'b1, 1'b0, 1'b1, gen_track[5], 1'b1, gen_track[3], 1'b1, gen_track[1]};
            4'd6: addr_nibble = {1'b1, 1'b0, 1'b1, gen_track[4], 1'b1, gen_track[2], 1'b1, gen_track[0]};
            4'd7: addr_nibble = {1'b1, 1'b0, 1'b1, 1'b0, 1'b1, sector[3], 1'b1, sector[1]};
            4'd8: addr_nibble = {1'b1, 1'b0, 1'b1, 1'b0, 1'b1, sector[2], 1'b1, sector[0]};
            4'd9: addr_nibble = {1'b1, check[7], 1'b1, check[5], 1'b1, check[3], 1'b1, check[1]};
            4'd10: addr_nibble = {1'b1, check[6], 1'b1, check[4], 1'b1, check[2], 1'b1, check[0]};
            4'd11: addr_nibble = 8'hde;
            4'd12: addr_nibble = 8'haa;
            default: addr_nibble = 8'heb;
        endcase
    end

    always @(posedge clk) begin
        if (reset) begin
            gen_state <= G_IDLE;
            field <= F_SYNC;
            sector <= 4'd0;
            index <= 9'd39;
            gap2 <= 1'b0;
            next_valid <= 1'b0;
            prev_value <= 6'd0;
        end else begin
            if (consume)
                next_valid <= 1'b0;
            case (gen_state)
                G_IDLE: if (!next_valid) begin
                    gen_track <= track;
                    read_step <= 2'd0;
                    aux_value <= 6'd0;
                    if (field == F_DATA && index >= 9'd3 && index < 9'd345)
                        gen_state <= G_READ;
                    else
                        gen_state <= G_DONE;
                end
                G_READ: begin
                    media_addr <= {gen_track, dos_skew(sector), byte_index};
                    wait_count <= 2'd0;
                    gen_state <= G_WAIT;
                end
                G_WAIT: begin
                    wait_count <= wait_count + 2'd1;
                    if (wait_count == 2'd2)
                        gen_state <= G_CAPTURE;
                end
                G_CAPTURE: begin
                    if (n < 9'd86) begin
                        // Byte n+172 exists only for n < 84.
                        case (read_step)
                            2'd0: aux_value[1:0] <= swap2(media_q);
                            2'd1: aux_value[3:2] <= swap2(media_q);
                            default: aux_value[5:4] <= swap2(media_q);
                        endcase
                        if (read_step == 2'd2 || (read_step == 2'd1 && n >= 9'd84)) begin
                            read_step <= 2'd3;
                            gen_state <= G_DONE;
                        end else begin
                            read_step <= read_step + 2'd1;
                            gen_state <= G_READ;
                        end
                    end else begin
                        aux_value <= media_q[7:2];
                        read_step <= 2'd3;
                        gen_state <= G_DONE;
                    end
                end
                default: begin   // G_DONE: publish and advance the position
                    next_valid <= 1'b1;
                    next_sync <= field == F_SYNC;
                    case (field)
                        F_SYNC: next_nibble <= 8'hff;
                        F_ADDR: next_nibble <= addr_nibble;
                        default: begin
                            if (index < 9'd3)
                                next_nibble <= index == 9'd0 ? 8'hd5 : index == 9'd1 ? 8'haa : 8'had;
                            else if (index < 9'd345) begin
                                next_nibble <= translate62(aux_value ^ prev_value);
                                prev_value <= aux_value;
                            end else if (index == 9'd345)
                                next_nibble <= translate62(prev_value);
                            else
                                next_nibble <= index == 9'd346 ? 8'hde : index == 9'd347 ? 8'haa : 8'heb;
                        end
                    endcase
                    case (field)
                        F_SYNC: begin
                            if (index == 9'd0) begin
                                field <= gap2 ? F_DATA : F_ADDR;
                                index <= 9'd0;
                                prev_value <= 6'd0;
                            end else begin
                                index <= index - 9'd1;
                            end
                        end
                        F_ADDR: begin
                            if (index == 9'd13) begin
                                field <= F_SYNC;
                                gap2 <= 1'b1;
                                index <= 9'd5;
                            end else begin
                                index <= index + 9'd1;
                            end
                        end
                        default: begin
                            if (index == 9'd348) begin
                                field <= F_SYNC;
                                gap2 <= 1'b0;
                                sector <= sector + 4'd1;
                                index <= sector == 4'd15 ? 9'd39 : 9'd19;
                            end else begin
                                index <= index + 9'd1;
                            end
                        end
                    endcase
                    gen_state <= G_IDLE;
                end
            endcase
        end
    end

    // ------------------------------------------------------------------
    // Bit shifter: MSB first; sync bytes carry two trailing zero bits.
    // ------------------------------------------------------------------
    reg [7:0] cur_nibble = 8'hff;
    reg cur_sync = 1'b1;
    reg [3:0] bit_index = 4'd0;
    reg [15:0] noise = 16'hace1;
    wire nibble_end = bit_index == (cur_sync ? 4'd9 : 4'd7);
    assign consume = bit_ce && disk_present && nibble_end && next_valid && !reset;
    always @(posedge clk) begin
        if (bit_ce) begin
            noise <= {noise[14:0], noise[15] ^ noise[13] ^ noise[12] ^ noise[10]};
            if (!disk_present) begin
                read_bit <= noise[0] & noise[3];
            end else begin
                read_bit <= bit_index < 4'd8 ? cur_nibble[3'd7 - bit_index[2:0]] : 1'b0;
                if (nibble_end) begin
                    bit_index <= 4'd0;
                    if (next_valid) begin
                        cur_nibble <= next_nibble;
                        cur_sync <= next_sync;
                    end else begin
                        cur_nibble <= 8'hff;
                        cur_sync <= 1'b1;
                    end
                end else begin
                    bit_index <= bit_index + 4'd1;
                end
            end
        end
        if (reset) begin
            bit_index <= 4'd0;
            cur_nibble <= 8'hff;
            cur_sync <= 1'b1;
            read_bit <= 1'b0;
        end
    end
endmodule
