// SPDX-License-Identifier: GPL-2.0-or-later
// Beta 128 disk interface: an FD1793 and the system register, as Fuse 1.6.0
// models them (peripherals/disk/beta.c, wd_fdc.c with WD_FLAG_BETA128).
//
// Ports, decoded by the machine while TR-DOS is paged: $1F command/status,
// $3F track, $5F sector, $7F data, $FF system register (write: bits 1:0
// drive, 3 HLT, 4 side 0 when set, 5 MFM; read: bit 7 INTRQ, bit 6 DRQ,
// the rest 0). READY and the drive motor follow the FDC's head load, which
// drops 3 s after a command ends.
//
// Drive A reads the fes.media.spectrum-disk raw track image (FogCast
// internal/spectrumdisks) through the store's fetch port: 8 KiB slots,
// cylinder c side s in slot 1+2c+s, its length in bytes 0-1, the missing
// clock bitmap from byte 16 and the track bytes from byte 1024. A slot past
// the image, or of length 0, is unformatted. Drives B-D are empty. Each drive
// keeps its own head position.
//
// Unlike Fuse, which hands the CPU the next byte whenever it reads the data
// register, the disk turns: while the head is loaded the head passes one
// track byte every 112 T states (32 us at 3.5 MHz, 300 rpm on a 6250-byte
// track), with a 10 ms index pulse at byte 0. Commands decode the bytes as
// they pass. A byte the CPU has not read when the next arrives sets Lost Data.
// The image is write protected: Write Sector and Write Track end with Write
// Protect. Type I commands step at the r1r0 rate and verify within five
// revolutions; Read Sector (single or multiple), Read Address and Read Track
// follow wd_fdc.c, including its 30 ms E delay and 15 ms verify settle.
//
// reset is the machine's reset, which master-resets the FDC as Fuse's
// beta_reset does; power_reset (the shell's execution reset) restarts the
// drive. When drive A's image becomes ready its header is read, and a launch
// image (flags bit 1) raises boot for 64 clocks: the machine resets into
// TR-DOS with the disk inserted, as Fuse's disk autoload does. A disk changed
// while the machine runs only swaps the medium.
module spectrum_beta #(
    parameter integer AW = 21,
    parameter integer T_PER_MS = 3500
) (
    input  wire          clk,
    input  wire          power_reset,
    input  wire          reset,
    input  wire          tick,          // one clock per T state
    input  wire [7:0]    port,          // A7..A0 of the access
    input  wire          write_strobe,  // one clock per I/O write to a Beta port
    input  wire          read_strobe,   // one clock per I/O read of a Beta port
    input  wire [7:0]    wdata,
    output reg  [7:0]    rdata,
    input  wire          disk_ready,
    input  wire [AW:0]   disk_bytes,
    output reg           fetch,
    output reg  [AW-1:0] fetch_addr,
    input  wire          fetch_ready,
    input  wire [7:0]    fetch_data,
    output wire          boot
);
    localparam integer SLOT_BITS = 13;
    localparam [12:0] NOMINAL_BYTES = 13'd6250;
    localparam [12:0] MAX_BYTES = 13'd6912;
    localparam [6:0] MAX_CYLINDER = 7'd85;
    localparam [6:0] T_PER_BYTE = 7'd112;
    localparam [12:0] INDEX_BYTES = 13'd313;  // 10 ms of 200
    localparam [23:0] HEAD_UNLOAD_T = 24'(3000 * T_PER_MS);

    // ---------------------------------------------------------- registers
    reg [7:0] command, track_r, sector_r, data_r;
    reg busy, drq, intrq, lost, crc_error, rnf, spinup, wrprot;
    reg type1_status;
    reg head_load;
    reg hlt;
    reg [1:0] drive;
    reg side;
    reg mfm;
    reg [6:0] cylinder [0:3];
    reg [23:0] unload_count;
    reg unload_armed;
    reg index_irq;      // Force Interrupt I2: INTRQ at the next index

    wire disk_here = drive == 2'd0 && disk_ready;
    wire [6:0] head_cylinder = cylinder[drive];
    wire tr00 = head_cylinder == 7'd0;

    // ----------------------------------------------------------- rotation
    reg [6:0] byte_t;
    reg [12:0] pos;
    reg [12:0] track_bytes;   // 0: unformatted
    wire [12:0] turn_bytes = track_bytes != 13'd0 ? track_bytes : NOMINAL_BYTES;
    wire index_pulse = head_load && pos < INDEX_BYTES;
    wire byte_strobe = tick && head_load && byte_t == T_PER_BYTE - 7'd1;
    wire [12:0] pos_next = pos + 13'd1 >= turn_bytes ? 13'd0 : pos + 13'd1;
    // The byte under the head once this clock's strobe has moved it.
    wire [12:0] head_pos = byte_strobe ? pos_next : pos;

    // The track under the head, re-read when the drive, cylinder, side or
    // disk changes. Track bytes are fetched one per byte time.
    wire [7:0] slot = {head_cylinder, side} + 8'd1;
    wire [AW-1:0] slot_base = AW'({slot, {SLOT_BITS{1'b0}}});
    wire slot_present = disk_here && {1'b0, slot_base} + (AW+1)'(1 << SLOT_BITS) <= disk_bytes;
    reg [9:0] track_key;
    wire [9:0] key_now = {slot_present, drive == 2'd0, head_cylinder, side};
    typedef enum logic [2:0] {F_IDLE, F_LEN0, F_LEN1, F_DATA, F_CLOCK, F_EVENT, F_FLAGS} fetch_state_t;
    fetch_state_t fstate;
    reg [7:0] len_low, byte_data;
    reg byte_clock;
    reg byte_ev;              // one clock: byte_data/byte_clock passed the head
    reg byte_wrap;            // that byte was byte 0
    reg pending;              // a byte strobe arrived during a fetch
    reg ready_q, boot_check;
    reg [6:0] boot_count;
    assign boot = boot_count != 7'd0;

    // ------------------------------------------------------------- decoder
    function automatic [15:0] crc_step(input [15:0] crc, input [7:0] b);
        integer i;
        reg [15:0] c;
        begin
            c = crc ^ {b, 8'd0};
            for (i = 0; i < 8; i = i + 1)
                c = c[15] ? {c[14:0], 1'b0} ^ 16'h1021 : {c[14:0], 1'b0};
            crc_step = c;
        end
    endfunction
    localparam [15:0] CRC_A1A1A1 = 16'hcdb4;
    reg [1:0] a1_run;
    wire mark_byte = byte_ev && a1_run == 2'd3 && !byte_clock && mfm;
    wire sync_a1 = byte_clock && byte_data == 8'ha1;

    typedef enum logic [3:0] {
        S_IDLE, S_STEP, S_STEP_WAIT, S_SETTLE, S_DELAY, S_HLT, S_SEARCH, S_ID,
        S_DAM, S_DATA, S_TRACK_INDEX, S_TRACK, S_LAST
    } state_t;
    state_t state;
    typedef enum logic [1:0] {OP_VERIFY, OP_READ, OP_ADDRESS, OP_TRACK} op_t;
    op_t op;
    reg [23:0] wait_t;
    reg [2:0] revs;
    reg [2:0] id_count;
    reg [7:0] id_c, id_r, id_n;
    reg [15:0] crc;
    reg [10:0] data_left;     // data bytes then two CRC bytes
    reg [5:0] dam_window;
    reg step_out;

    wire cmd_type1 = !command[7];
    wire [1:0] rate = command[1:0];
    wire [23:0] step_t = rate == 2'd0 ? 24'(6 * T_PER_MS) : rate == 2'd1 ? 24'(12 * T_PER_MS) :
                         rate == 2'd2 ? 24'(20 * T_PER_MS) : 24'(30 * T_PER_MS);

    wire [7:0] status = {
        !head_load, wrprot, spinup, rnf, crc_error,
        lost,
        type1_status ? (!disk_here || index_pulse) : drq,
        busy
    };
    always @* begin
        case (port)
            8'h1f: rdata = status;
            8'h3f: rdata = track_r;
            8'h5f: rdata = sector_r;
            8'h7f: rdata = data_r;
            default: rdata = {intrq, drq, 6'd0};
        endcase
    end

    task automatic finish;
        begin
            busy <= 1'b0;
            intrq <= 1'b1;
            state <= S_IDLE;
            unload_armed <= head_load;
            unload_count <= HEAD_UNLOAD_T;
        end
    endtask

    task automatic deliver(input [7:0] value);
        begin
            if (drq) lost <= 1'b1;
            data_r <= value;
            drq <= 1'b1;
        end
    endtask

    // ------------------------------------------------------------- the drive
    always @(posedge clk) begin
        byte_ev <= 1'b0;
        if (boot_count != 7'd0) boot_count <= boot_count - 7'd1;
        if (power_reset) begin
            byte_t <= 7'd0;
            pos <= 13'd0;
            track_bytes <= 13'd0;
            track_key <= 10'd0;
            fstate <= F_IDLE;
            fetch <= 1'b0;
            pending <= 1'b0;
            a1_run <= 2'd0;
            ready_q <= 1'b0;
            boot_check <= 1'b0;
            boot_count <= 7'd0;
        end else begin
            if (!disk_ready) ready_q <= 1'b0;
            else if (!ready_q) begin
                ready_q <= 1'b1;
                boot_check <= 1'b1;
            end
            // ---------------------------------------------------- rotation
            if (tick && head_load) byte_t <= byte_t == T_PER_BYTE - 7'd1 ? 7'd0 : byte_t + 7'd1;
            if (byte_strobe) pos <= pos_next;

            // A changed track is measured before its bytes are read.
            case (fstate)
                F_IDLE: begin
                    if (boot_check && disk_ready) begin
                        boot_check <= 1'b0;
                        fstate <= F_FLAGS;
                        fetch <= 1'b1;
                        fetch_addr <= AW'(7);
                    end else if (key_now != track_key) begin
                        track_key <= key_now;
                        track_bytes <= 13'd0;
                        if (slot_present) begin
                            fstate <= F_LEN0;
                            fetch <= 1'b1;
                            fetch_addr <= slot_base;
                        end
                    end else if (byte_strobe || pending) begin
                        pending <= 1'b0;
                        if (track_bytes != 13'd0 && head_pos < track_bytes) begin
                            fstate <= F_DATA;
                            fetch <= 1'b1;
                            fetch_addr <= slot_base + AW'(1024) + AW'(head_pos);
                        end else begin
                            byte_data <= 8'h00;
                            byte_clock <= 1'b0;
                            fstate <= F_EVENT;
                        end
                    end
                end
                F_LEN0: if (fetch_ready) begin
                    len_low <= fetch_data;
                    fetch_addr <= slot_base + AW'(1);
                    fstate <= F_LEN1;
                end
                F_LEN1: if (fetch_ready) begin
                    fetch <= 1'b0;
                    track_bytes <= {fetch_data, len_low} <= {3'd0, MAX_BYTES} ?
                                   {fetch_data[4:0], len_low} : 13'd0;
                    fstate <= F_IDLE;
                end
                F_DATA: if (fetch_ready) begin
                    byte_data <= fetch_data;
                    fetch_addr <= slot_base + AW'(16) + AW'(pos[12:3]);
                    fstate <= F_CLOCK;
                end
                F_CLOCK: if (fetch_ready) begin
                    fetch <= 1'b0;
                    byte_clock <= fetch_data[pos[2:0]];
                    fstate <= F_EVENT;
                end
                F_FLAGS: if (fetch_ready) begin
                    fetch <= 1'b0;
                    if (fetch_data[1]) boot_count <= 7'd64;
                    fstate <= F_IDLE;
                end
                default: begin
                    byte_ev <= 1'b1;
                    byte_wrap <= pos == 13'd0;
                    fstate <= F_IDLE;
                end
            endcase
            if (byte_strobe && fstate != F_IDLE) pending <= 1'b1;
            // A track change abandons the byte in flight.
            if (fstate != F_IDLE && fstate != F_EVENT && fstate != F_FLAGS && key_now != track_key) begin
                fetch <= 1'b0;
                fstate <= F_IDLE;
            end

            if (byte_ev) a1_run <= sync_a1 ? (a1_run == 2'd3 ? 2'd3 : a1_run + 2'd1) : 2'd0;

        end
    end

    // --------------------------------------------------------------- the FDC
    integer i;
    always @(posedge clk) begin
        if (reset) begin
            command <= 8'd0;
            track_r <= 8'd0;
            sector_r <= 8'd0;
            data_r <= 8'd0;
            busy <= 1'b0;
            drq <= 1'b0;
            intrq <= 1'b0;
            lost <= 1'b1;            // track 0 after the master reset
            crc_error <= 1'b0;
            rnf <= 1'b0;
            spinup <= 1'b0;
            wrprot <= 1'b0;
            type1_status <= 1'b1;
            head_load <= 1'b0;
            hlt <= 1'b1;
            drive <= 2'd0;
            side <= 1'b0;
            mfm <= 1'b1;
            for (i = 0; i < 4; i = i + 1) cylinder[i] <= 7'd0;
            unload_armed <= 1'b0;
            unload_count <= 24'd0;
            index_irq <= 1'b0;
            state <= S_IDLE;
            op <= OP_VERIFY;
            wait_t <= 24'd0;
        end else begin
            // --------------------------------------------- head unloading
            if (tick && unload_armed) begin
                if (unload_count == 24'd0) begin
                    unload_armed <= 1'b0;
                    head_load <= 1'b0;
                end else unload_count <= unload_count - 24'd1;
            end
            if (index_irq && byte_strobe && pos_next == INDEX_BYTES) begin
                index_irq <= 1'b0;
                intrq <= 1'b1;
            end

            // ------------------------------------------------- the command
            if (tick && wait_t != 24'd0) wait_t <= wait_t - 24'd1;
            case (state)
                S_STEP: begin
                    // wd_fdc.c type_i_loop / update / noupdate.
                    if (step_out && tr00) begin
                        track_r <= 8'd0;
                        state <= S_SETTLE;
                    end else begin
                        if (step_out) cylinder[drive] <= head_cylinder - 7'd1;
                        else if (head_cylinder != MAX_CYLINDER) cylinder[drive] <= head_cylinder + 7'd1;
                        wait_t <= step_t;
                        state <= S_STEP_WAIT;
                    end
                end
                S_STEP_WAIT: if (wait_t == 24'd0) begin
                    if (command[6:5] != 2'b00 || track_r == data_r) state <= S_SETTLE;
                    else begin
                        step_out <= track_r > data_r;
                        track_r <= track_r > data_r ? track_r - 8'd1 : track_r + 8'd1;
                        state <= S_STEP;
                    end
                end
                S_SETTLE: begin
                    // type_i_verify: with V, load the head and settle 15 ms.
                    if (command[2]) begin
                        head_load <= 1'b1;
                        unload_armed <= 1'b0;
                        wait_t <= 24'(15 * T_PER_MS);
                        op <= OP_VERIFY;
                        state <= S_DELAY;
                    end else begin
                        lost <= tr00;
                        finish();
                    end
                end
                S_DELAY: if (wait_t == 24'd0) state <= S_HLT;
                S_HLT: if (hlt) begin
                    revs <= 3'd0;
                    case (op)
                        OP_VERIFY: begin
                            if (head_load) spinup <= 1'b1;
                            lost <= tr00;
                            state <= S_SEARCH;
                        end
                        OP_TRACK: state <= S_TRACK_INDEX;
                        default: state <= S_SEARCH;
                    endcase
                    if (op == OP_READ ? command[5] : op == OP_TRACK && command[4]) begin   // Write Sector / Write Track
                        wrprot <= 1'b1;
                        finish();
                    end
                end
                S_SEARCH: if (byte_ev) begin
                    if (byte_wrap && revs == 3'd4) begin
                        rnf <= 1'b1;
                        finish();
                    end else if (mark_byte && byte_data == 8'hfe) begin
                        crc <= crc_step(CRC_A1A1A1, 8'hfe);
                        id_count <= 3'd0;
                        state <= S_ID;
                    end
                    if (byte_wrap) revs <= revs + 3'd1;
                end
                S_ID: if (byte_ev) begin
                    crc <= crc_step(crc, byte_data);
                    id_count <= id_count + 3'd1;
                    case (id_count)
                        3'd0: id_c <= byte_data;
                        3'd2: id_r <= byte_data;
                        3'd3: id_n <= byte_data;
                        default: ;
                    endcase
                    if (op == OP_ADDRESS) deliver(byte_data);
                    if (id_count == 3'd5) begin
                        // crc_step of the last byte gives zero for a good ID.
                        case (op)
                            OP_VERIFY: if (crc_step(crc, byte_data) == 16'd0) begin
                                if (id_c != track_r) rnf <= 1'b1;
                                finish();
                            end else state <= S_SEARCH;
                            OP_READ: if (crc_step(crc, byte_data) == 16'd0 &&
                                         id_c == track_r && id_r == sector_r) begin
                                dam_window <= 6'd44;
                                state <= S_DAM;
                            end else state <= S_SEARCH;
                            default: begin   // Read Address
                                // Ends once the CPU takes the last byte, as
                                // wd_fdc.c ends it on that read.
                                crc_error <= crc_step(crc, byte_data) != 16'd0;
                                sector_r <= id_c;
                                state <= S_LAST;
                            end
                        endcase
                    end
                end
                S_DAM: if (byte_ev) begin
                    dam_window <= dam_window - 6'd1;
                    if (mark_byte && byte_data >= 8'hf8 && byte_data <= 8'hfb) begin
                        spinup <= byte_data != 8'hfb;   // deleted data mark
                        crc <= crc_step(CRC_A1A1A1, byte_data);
                        data_left <= (11'd128 << id_n[1:0]) + 11'd2;
                        state <= S_DATA;
                    end else if (dam_window == 6'd0 || mark_byte) begin
                        rnf <= 1'b1;
                        finish();
                    end
                end
                S_DATA: if (byte_ev) begin
                    crc <= crc_step(crc, byte_data);
                    data_left <= data_left - 11'd1;
                    if (data_left > 11'd2) deliver(byte_data);
                    if (data_left == 11'd1) begin
                        if (crc_step(crc, byte_data) == 16'd0 && command[4]) begin
                            sector_r <= sector_r + 8'd1;
                            revs <= 3'd0;
                            state <= S_SEARCH;
                        end else begin
                            crc_error <= crc_step(crc, byte_data) != 16'd0;
                            finish();
                        end
                    end
                end
                S_LAST: if (!drq || byte_ev) finish();
                S_TRACK_INDEX: if (byte_ev && byte_wrap) begin
                    deliver(byte_data);
                    state <= S_TRACK;
                end
                S_TRACK: if (byte_ev) begin
                    if (byte_wrap) finish();
                    else deliver(byte_data);
                end
                default: ;
            endcase

            // ------------------------------------------------- CPU access
            if (read_strobe && port == 8'h1f) intrq <= 1'b0;
            if (read_strobe && port == 8'h7f) drq <= 1'b0;
            if (write_strobe) begin
                case (port)
                    8'h3f: track_r <= wdata;
                    8'h5f: sector_r <= wdata;
                    8'h7f: data_r <= wdata;
                    8'hff: begin
                        drive <= wdata[1:0];
                        hlt <= wdata[3];
                        side <= !wdata[4];
                        mfm <= wdata[5];
                    end
                    default: begin   // $1F command
                        intrq <= 1'b0;
                        if (wdata[7:4] == 4'hd) begin
                            // Type IV: abandon the command; I3 interrupts
                            // now, I2 at the next index. Fuse's spinup
                            // without the h bit loads the head.
                            busy <= 1'b0;
                            wrprot <= 1'b0;
                            crc_error <= 1'b0;
                            drq <= 1'b0;
                            type1_status <= 1'b1;
                            state <= S_IDLE;
                            wait_t <= 24'd0;
                            lost <= tr00;
                            head_load <= 1'b1;
                            unload_armed <= 1'b0;
                            if (wdata[3]) intrq <= 1'b1;
                            index_irq <= !wdata[3] && wdata[2];
                        end else if (!busy) begin
                            command <= wdata;
                            unload_armed <= 1'b0;
                            if (!wdata[7]) begin
                                // Type I: Restore, Seek, Step, Step In/Out.
                                busy <= 1'b1;
                                type1_status <= 1'b1;
                                crc_error <= 1'b0;
                                rnf <= 1'b0;
                                drq <= 1'b0;
                                spinup <= 1'b1;
                                if (wdata[3]) head_load <= 1'b1;
                                else if (!wdata[2]) head_load <= 1'b0;
                                if (wdata[6:5] != 2'b00) begin
                                    // Step keeps the last direction; Step
                                    // In/Out set it. u updates the track.
                                    if (wdata[6]) step_out <= wdata[5];
                                    if (wdata[4])
                                        track_r <= (wdata[6] ? wdata[5] : step_out) ?
                                                   track_r - 8'd1 : track_r + 8'd1;
                                    state <= S_STEP;
                                end else begin
                                    // Seek toward the data register; Restore
                                    // seeks from 255 to 0.
                                    if (!wdata[4]) begin
                                        track_r <= 8'hfe;
                                        data_r <= 8'd0;
                                        step_out <= 1'b1;
                                        state <= S_STEP;
                                    end else if (track_r == data_r) state <= S_SETTLE;
                                    else begin
                                        step_out <= track_r > data_r;
                                        track_r <= track_r > data_r ? track_r - 8'd1 : track_r + 8'd1;
                                        state <= S_STEP;
                                    end
                                end
                            end else if (!head_load) begin
                                // Beta READY is the head load: a Type II or
                                // III command on an unloaded head ends at once.
                                intrq <= 1'b1;
                                unload_armed <= 1'b0;
                            end else if (wdata[6:4] != 3'b101) begin
                                busy <= 1'b1;
                                type1_status <= 1'b0;
                                rnf <= 1'b0;
                                drq <= 1'b0;
                                lost <= 1'b0;
                                spinup <= 1'b0;
                                if (!wdata[6]) wrprot <= 1'b0;
                                op <= !wdata[6] ? OP_READ : wdata[5] ? OP_TRACK : OP_ADDRESS;
                                wait_t <= wdata[2] ? 24'(30 * T_PER_MS) : 24'd0;
                                state <= S_DELAY;
                            end
                        end
                    end
                endcase
            end
        end
    end
endmodule
