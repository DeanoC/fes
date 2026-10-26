// SPDX-License-Identifier: GPL-2.0-or-later
// Floppy drives for the Spectrum disk controllers (spectrum_beta,
// spectrum_upd765): the disk turning under the head, read from the
// fes.media.spectrum-disk raw track image (FogCast internal/spectrumdisks)
// through the DDR store's fetch port.
//
// Drive 0 holds the image; the others are empty. Each drive keeps its own
// head position; step moves the selected drive's head one cylinder (out
// stops at 0, in at MAX_CYLINDER) and home puts it at 0. While spin is high
// the disk turns: one track byte passes the head every T_PER_BYTE T states
// (32 us at 3.5 MHz), and a turn takes the track's length in bytes, or 6250
// for an unformatted track, with a 10 ms index pulse at byte 0.
//
// The image's header gives its slot size (8 or 16 KiB, bytes 7-8 are read
// when drive 0's image becomes ready). Cylinder c, side s is slot 1+2c+s:
// its length in bytes 0-1, bit 0 of byte 2 an FM track, the mark bitmap from
// byte 16, the track bytes from an eighth of the slot and up to eight weak
// ranges just below them. A slot past the image, or of length 0, is
// unformatted. A changed track is measured before its bytes are read; each
// byte is then fetched as it reaches the head, with its mark bit, and a byte
// in a weak range reads as Fuse 1.6.0 reads one, mixed with random bits.
//
// byte_ev marks each byte passing the head. mark is high with byte_ev on an
// address mark's mark byte in the selected density: in MFM, a byte with its
// clock present after three A1 bytes with missing clocks; in FM, a byte with
// its special clock. A track of the other density shows no marks.
//
// A launch image (header flags bit 1) raises boot for 64 clocks when it
// becomes ready. power_reset (the shell's execution reset) restarts the
// drives, their heads and boot detection; the machine reset that boot causes
// does not, so the drive keeps turning through it.
module spectrum_fdd #(
    parameter integer AW = 22,
    parameter integer DRIVES = 4,
    parameter [6:0] MAX_CYLINDER = 7'd85,
    parameter [6:0] T_PER_BYTE = 7'd112
) (
    input  wire          clk,
    input  wire          power_reset,
    input  wire          tick,          // one clock per T state
    input  wire          spin,          // the selected drive's disk turns
    input  wire [1:0]    drive,
    input  wire          side,
    input  wire          mfm,           // the controller's density
    input  wire          step,          // one clock
    input  wire          step_out,
    input  wire          home,          // one clock: selected head to cylinder 0
    output wire [6:0]    cylinder,
    output wire          tr00,
    output wire          loaded,        // the selected drive holds a disk
    output wire          index_pulse,
    output wire          index_end,     // one clock: the current index pulse ends
    output reg           byte_ev,
    output reg  [7:0]    byte_data,
    output reg           byte_clock,
    output reg           byte_wrap,     // the byte was byte 0 (the index)
    output wire          mark,
    input  wire          disk_ready,
    input  wire [AW:0]   disk_bytes,
    output reg           fetch,
    output reg  [AW-1:0] fetch_addr,
    input  wire          fetch_ready,
    input  wire [7:0]    fetch_data,
    output wire          boot
);
    localparam [13:0] NOMINAL_BYTES = 14'd6250;
    localparam [13:0] INDEX_BYTES = 14'd313;  // 10 ms of 200

    reg [6:0] head [0:DRIVES-1];
    assign cylinder = head[drive];
    assign tr00 = cylinder == 7'd0;
    assign loaded = drive == 2'd0 && disk_ready;

    // ------------------------------------------------------------ rotation
    reg [6:0] byte_t;
    reg [13:0] pos;
    reg [13:0] track_bytes;    // 0: unformatted
    reg track_fm;
    wire [13:0] turn_bytes = track_bytes != 14'd0 ? track_bytes : NOMINAL_BYTES;
    assign index_pulse = spin && pos < INDEX_BYTES;
    wire byte_strobe = tick && spin && byte_t == T_PER_BYTE - 7'd1;
    wire [13:0] pos_next = pos + 14'd1 >= turn_bytes ? 14'd0 : pos + 14'd1;
    assign index_end = byte_strobe && pos_next == INDEX_BYTES;
    // The byte under the head once this clock's strobe has moved it.
    wire [13:0] head_pos = byte_strobe ? pos_next : pos;

    // ---------------------------------------------------------------- slots
    reg big_slots;                 // 16 KiB slots
    wire [7:0] slot = {cylinder, side} + 8'd1;
    wire [AW-1:0] slot_base = big_slots ? AW'({slot, 14'd0}) : AW'({slot, 13'd0});
    wire [AW-1:0] data_base = slot_base + (big_slots ? AW'(2048) : AW'(1024));
    wire [13:0] max_bytes = big_slots ? 14'd15104 : 14'd6912;
    wire [AW:0] slot_end = {1'b0, slot_base} + (big_slots ? (AW+1)'(16384) : (AW+1)'(8192));
    wire slot_present = loaded && slot_end <= disk_bytes;
    reg [10:0] track_key;
    wire [10:0] key_now = {slot_present, big_slots, drive == 2'd0, cylinder, side};

    reg [13:0] weak_start [0:7];
    reg [13:0] weak_len [0:7];
    reg [5:0] weak_k;          // range byte being read, 0..31
    reg [7:0] weak_low;
    reg weak_byte;
    integer w;
    reg weak_hit;
    always @* begin
        weak_hit = 1'b0;
        for (w = 0; w < 8; w = w + 1)
            if (pos - weak_start[w] < weak_len[w]) weak_hit = 1'b1;
    end
    reg [15:0] lfsr;

    typedef enum logic [3:0] {
        F_IDLE, F_HEADER, F_SHIFT, F_LEN0, F_LEN1, F_FM, F_WEAK, F_DATA, F_CLOCK, F_EVENT
    } fetch_state_t;
    fetch_state_t fstate;
    reg [7:0] len_low;
    reg pending;               // a byte strobe arrived during a fetch
    reg ready_q, header_check;
    reg [6:0] boot_count;
    assign boot = boot_count != 7'd0;

    // ---------------------------------------------------------------- marks
    reg [1:0] a1_run;
    assign mark = byte_ev && (track_fm ? !mfm && byte_clock : mfm && a1_run == 2'd3 && !byte_clock);
    wire sync_a1 = byte_clock && byte_data == 8'ha1;

    integer i;
    always @(posedge clk) begin
        byte_ev <= 1'b0;
        lfsr <= {lfsr[14:0], lfsr[15] ^ lfsr[13] ^ lfsr[12] ^ lfsr[10]};
        if (boot_count != 7'd0) boot_count <= boot_count - 7'd1;
        if (step && !(step_out && tr00) && !(!step_out && cylinder == MAX_CYLINDER))
            head[drive] <= step_out ? cylinder - 7'd1 : cylinder + 7'd1;
        if (home) head[drive] <= 7'd0;
        if (power_reset) begin
            byte_t <= 7'd0;
            pos <= 14'd0;
            track_bytes <= 14'd0;
            track_fm <= 1'b0;
            track_key <= 11'd0;
            fstate <= F_IDLE;
            fetch <= 1'b0;
            pending <= 1'b0;
            a1_run <= 2'd0;
            ready_q <= 1'b0;
            header_check <= 1'b0;
            boot_count <= 7'd0;
            big_slots <= 1'b0;
            lfsr <= 16'hace1;
            for (i = 0; i < 8; i = i + 1) weak_len[i] <= 14'd0;
            for (i = 0; i < DRIVES; i = i + 1) head[i] <= 7'd0;
        end else begin
            if (!disk_ready) ready_q <= 1'b0;
            else if (!ready_q) begin
                ready_q <= 1'b1;
                header_check <= 1'b1;
            end
            if (tick && spin) byte_t <= byte_t == T_PER_BYTE - 7'd1 ? 7'd0 : byte_t + 7'd1;
            if (byte_strobe) pos <= pos_next;

            case (fstate)
                F_IDLE: begin
                    if (header_check && disk_ready) begin
                        header_check <= 1'b0;
                        fstate <= F_HEADER;
                        fetch <= 1'b1;
                        fetch_addr <= AW'(7);
                    end else if (key_now != track_key) begin
                        track_key <= key_now;
                        track_bytes <= 14'd0;
                        track_fm <= 1'b0;
                        for (i = 0; i < 8; i = i + 1) weak_len[i] <= 14'd0;
                        if (slot_present) begin
                            fstate <= F_LEN0;
                            fetch <= 1'b1;
                            fetch_addr <= slot_base;
                        end
                    end else if (byte_strobe || pending) begin
                        pending <= 1'b0;
                        if (track_bytes != 14'd0 && head_pos < track_bytes) begin
                            fstate <= F_DATA;
                            fetch <= 1'b1;
                            fetch_addr <= data_base + AW'(head_pos);
                        end else begin
                            byte_data <= 8'h00;
                            byte_clock <= 1'b0;
                            weak_byte <= 1'b0;
                            fstate <= F_EVENT;
                        end
                    end
                end
                F_HEADER: if (fetch_ready) begin
                    if (fetch_data[1]) boot_count <= 7'd64;
                    fetch_addr <= AW'(8);
                    fstate <= F_SHIFT;
                end
                F_SHIFT: if (fetch_ready) begin
                    fetch <= 1'b0;
                    big_slots <= fetch_data == 8'd14;
                    fstate <= F_IDLE;
                end
                F_LEN0: if (fetch_ready) begin
                    len_low <= fetch_data;
                    fetch_addr <= slot_base + AW'(1);
                    fstate <= F_LEN1;
                end
                F_LEN1: if (fetch_ready) begin
                    track_bytes <= {fetch_data, len_low} <= {2'd0, max_bytes} ?
                                   {fetch_data[5:0], len_low} : 14'd0;
                    fetch_addr <= slot_base + AW'(2);
                    fstate <= F_FM;
                end
                F_FM: if (fetch_ready) begin
                    track_fm <= fetch_data[0];
                    weak_k <= 6'd0;
                    fetch_addr <= data_base - AW'(144);
                    fstate <= F_WEAK;
                end
                F_WEAK: if (fetch_ready) begin
                    // Eight ranges of start and length, little endian.
                    if (!weak_k[0]) weak_low <= fetch_data;
                    else if (!weak_k[1]) weak_start[weak_k[4:2]] <= {fetch_data[5:0], weak_low};
                    else weak_len[weak_k[4:2]] <= {fetch_data[5:0], weak_low};
                    weak_k <= weak_k + 6'd1;
                    fetch_addr <= fetch_addr + AW'(1);
                    if (weak_k == 6'd31) begin
                        fetch <= 1'b0;
                        fstate <= F_IDLE;
                    end
                end
                F_DATA: if (fetch_ready) begin
                    byte_data <= fetch_data;
                    weak_byte <= weak_hit;
                    fetch_addr <= slot_base + AW'(16) + AW'(pos[13:3]);
                    fstate <= F_CLOCK;
                end
                F_CLOCK: if (fetch_ready) begin
                    fetch <= 1'b0;
                    byte_clock <= fetch_data[pos[2:0]];
                    // fdd.c: data &= rand, data |= rand.
                    if (weak_byte) byte_data <= (byte_data & lfsr[7:0]) | lfsr[15:8];
                    fstate <= F_EVENT;
                end
                default: begin
                    byte_ev <= 1'b1;
                    byte_wrap <= pos == 14'd0;
                    fstate <= F_IDLE;
                end
            endcase
            if (byte_strobe && fstate != F_IDLE) pending <= 1'b1;
            // A track change abandons the byte in flight.
            if ((fstate == F_LEN0 || fstate == F_LEN1 || fstate == F_FM || fstate == F_WEAK ||
                 fstate == F_DATA || fstate == F_CLOCK) && key_now != track_key) begin
                fetch <= 1'b0;
                fstate <= F_IDLE;
            end

            if (byte_ev) a1_run <= sync_a1 ? (a1_run == 2'd3 ? 2'd3 : a1_run + 2'd1) : 2'd0;
        end
    end
endmodule
