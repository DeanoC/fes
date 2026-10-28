// SPDX-License-Identifier: GPL-2.0-or-later
// The +2A/+3's uPD765A floppy controller, as Fuse 1.6.0 models it
// (peripherals/disk/upd_fdc.c with UPD765A at 4 MHz and the +3 wiring of
// machines/specplus3.c), in non-DMA mode.
//
// The machine decodes $2FFD (A15:A12 0010, A1 0) as the main status register
// and $3FFD (0011) as the data register; $1FFD bit 3 is the drive motor. The
// +3 wires only US0, so units 0 and 2 are drive A and 1 and 3 drive B; drive
// A reads the fes.media.spectrum-disk raw track image through spectrum_fdd,
// and drive B is empty: write protected, with its own head, never ready, as
// in Fuse's default +3. A drive is ready 400 ms after the motor starts with
// a disk in it and the disk turns while the motor runs, as fdd.c has it. The
// step, head load and head unload times count whole milliseconds.
//
// Commands follow upd_fdc.c: Read Data and Read Deleted Data (MT, the +3's
// missing terminal count ending every read with End of Cylinder, Fuse's SK
// from the first parameter byte), Read Diagnostic, Read ID, Recalibrate and
// Seek (with Fuse's recalibrate over 77 steps), Sense Interrupt Status (an
// invalid command without a pending interrupt), Specify (step, head unload
// and head load times at the 4 MHz clock), Sense Drive Status, Version and
// invalid commands. Searches for an ID give up after two index pulses, and
// the status bits of a search are those of its last ID or index, as upd_fdc.c
// leaves them. The image is write protected: Write Data, Write Deleted Data
// and Format end with Not Writable. Scan is treated as an invalid command.
//
// Unlike Fuse, which hands the CPU the next data byte whenever it reads the
// data register, data bytes arrive as they pass the head (one every 32 us); a
// byte still unread when the next arrives ends the command with Overrun, as
// the chip does. reset is the machine's reset (upd_fdc_master_reset).
module spectrum_upd765 #(
    parameter integer AW = 22,
    parameter integer T_PER_MS = 3547
) (
    input  wire          clk,
    input  wire          power_reset,
    input  wire          reset,
    input  wire          tick,          // one clock per T state
    input  wire          motor,
    input  wire          data_port,     // the access is $3FFD, not $2FFD
    input  wire          write_strobe,  // one clock per I/O write to $3FFD
    input  wire          read_strobe,   // one clock per I/O read of $2FFD/$3FFD
    input  wire [7:0]    wdata,
    output wire [7:0]    rdata,
    input  wire          disk_ready,
    input  wire [AW:0]   disk_bytes,
    output wire          fetch,
    output wire [AW-1:0] fetch_addr,
    input  wire          fetch_ready,
    input  wire [7:0]    fetch_data,
    output wire          boot
);
    // Commands, in upd_fdc.c's order (their order decides some status).
    typedef enum logic [3:0] {
        C_READ_DATA, C_READ_DIAG, C_WRITE_DATA, C_WRITE_ID, C_SCAN, C_READ_ID,
        C_RECALIBRATE, C_SENSE_INT, C_SPECIFY, C_SENSE_DRIVE, C_VERSION, C_SEEK, C_INVALID
    } cmd_t;
    function automatic cmd_t identify(input [7:0] b);
        if ((b & 8'h1f) == 8'h06 || (b & 8'h1f) == 8'h0c) identify = C_READ_DATA;
        else if ((b & 8'h9f) == 8'h02) identify = C_READ_DIAG;
        else if (b == 8'h07) identify = C_RECALIBRATE;
        else if (b == 8'h0f) identify = C_SEEK;
        else if ((b & 8'h3f) == 8'h05 || (b & 8'h3f) == 8'h09) identify = C_WRITE_DATA;
        else if ((b & 8'hbf) == 8'h0d) identify = C_WRITE_ID;
        else if ((b & 8'h1f) == 8'h11 || (b & 8'h1f) == 8'h19 || (b & 8'h1f) == 8'h1d) identify = C_SCAN;
        else if ((b & 8'hbf) == 8'h0a) identify = C_READ_ID;
        else if (b == 8'h08) identify = C_SENSE_INT;
        else if (b == 8'h03) identify = C_SPECIFY;
        else if (b == 8'h04) identify = C_SENSE_DRIVE;
        else if ((b & 8'h1f) == 8'h10) identify = C_VERSION;
        else identify = C_INVALID;
    endfunction
    function automatic [3:0] params(input cmd_t c);
        case (c)
            C_READ_DATA, C_READ_DIAG, C_WRITE_DATA, C_SCAN: params = 4'd8;
            C_WRITE_ID: params = 4'd5;
            C_SEEK, C_SPECIFY: params = 4'd2;
            C_READ_ID, C_RECALIBRATE, C_SENSE_DRIVE: params = 4'd1;
            default: params = 4'd0;
        endcase
    endfunction
    function automatic [2:0] results(input cmd_t c);
        case (c)
            C_READ_DATA, C_READ_DIAG, C_WRITE_DATA, C_WRITE_ID, C_SCAN, C_READ_ID: results = 3'd7;
            C_SENSE_INT: results = 3'd2;
            C_SENSE_DRIVE, C_VERSION, C_INVALID: results = 3'd1;
            default: results = 3'd0;
        endcase
    endfunction

    localparam [7:0] ST0_NOT_READY = 8'h08, ST0_EQUIP = 8'h10, ST0_SEEK_END = 8'h20,
                     ST0_ABNORMAL = 8'h40, ST0_READY_CHANGE = 8'hc0;
    localparam [7:0] ST1_MA = 8'h01, ST1_NW = 8'h02, ST1_ND = 8'h04, ST1_OR = 8'h10,
                     ST1_DE = 8'h20, ST1_EN = 8'h80;
    localparam [7:0] ST2_MD = 8'h01, ST2_BC = 8'h02, ST2_WC = 8'h10, ST2_DD = 8'h20, ST2_CM = 8'h40;
    localparam [23:0] READY_T = 24'(400 * T_PER_MS);
    localparam [23:0] SPIN_DOWN_T = 24'(300 * T_PER_MS);
    localparam [15:0] CRC_A1A1A1 = 16'hcdb4;

    // ------------------------------------------------------------ registers
    typedef enum logic [1:0] {P_CMD, P_EXE, P_RES} phase_t;
    phase_t phase;
    reg rqm, dio, exm, cb;
    reg [3:0] seeking;                 // MSR D0-D3
    reg [7:0] command;
    cmd_t cmd;
    reg [7:0] dr [0:8];                // data_register: parameter bytes
    reg [3:0] cycle;                   // parameter or result bytes so far
    reg [7:0] st0, st1, st2, st3;
    reg [7:0] sense0, sense1;
    reg [7:0] data_r;
    reg [1:0] us;
    reg hd;
    reg mt, mf, sk, del_data, first_rw;
    reg [7:0] pcn [0:3];
    reg [7:0] ncn [0:3];
    reg [7:0] rec [0:3];
    reg [2:0] seek_st [0:3];           // 0 none, 1 seek, 2 recalibrate, 4 done, 5 failed, 6 not ready
    reg [3:0] seek_queue;              // started, waiting for the stepper
    typedef enum logic [2:0] {I_NONE, I_RESULT, I_EXE, I_READY, I_SEEK} intrq_t;
    intrq_t intrq;
    reg [9:0] step_t, hut_t, hld_t;    // in ms
    reg head_loaded;
    reg [9:0] head_count;
    reg head_armed;

    // ------------------------------------------------------------- the drive
    reg motor_q;
    reg [23:0] motor_count;
    reg spinning, drive_ready;
    reg [1:0] seek_unit;
    reg seek_busy;
    reg step;
    reg step_out;
    wire [1:0] fdd_drive = {1'b0, seek_busy ? seek_unit[0] : us[0]};
    wire loaded, tr00, index_pulse, byte_ev, byte_clock, byte_wrap, mark;
    wire [6:0] cylinder;
    wire [7:0] byte_data;
    spectrum_fdd #(.AW(AW), .DRIVES(2)) drives (
        .clk(clk), .power_reset(power_reset), .tick(tick), .spin(spinning), .drive(fdd_drive),
        .side(hd), .mfm(mf), .step(step), .step_out(step_out), .home(1'b0),
        .cylinder(cylinder), .tr00(tr00), .loaded(loaded), .index_pulse(index_pulse), .index_end(),
        .byte_ev(byte_ev), .byte_data(byte_data), .byte_clock(byte_clock),
        .byte_wrap(byte_wrap), .mark(mark), .disk_ready(disk_ready), .disk_bytes(disk_bytes),
        .fetch(fetch), .fetch_addr(fetch_addr), .fetch_ready(fetch_ready), .fetch_data(fetch_data),
        .boot(boot)
    );
    // Drive B is empty: never ready.
    wire ready = drive_ready && loaded;

    reg [7:0] result_byte;
    always @* begin
        if (cmd == C_SENSE_DRIVE) result_byte = st3;
        else if (cmd == C_SENSE_INT) result_byte = cycle == 4'd0 ? sense0 : sense1;
        else if (cycle == 4'd0) result_byte = st0;
        else if (cycle == 4'd1) result_byte = st1;
        else if (cycle == 4'd2) result_byte = st2;
        else result_byte = dr[cycle - 4'd2];
    end
    // A read of the data register keeps the byte it started with: the CPU
    // samples it later in the cycle, after the read has moved the phase on.
    wire [7:0] data_now = !(rqm && dio) ? 8'hff : phase == P_RES ? result_byte : data_r;
    reg [7:0] data_held;
    always @(posedge clk) if (read_strobe && data_port) data_held <= data_now;
    assign rdata = !data_port ? {rqm, dio, exm, cb, seeking} : read_strobe ? data_now : data_held;

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

    // --------------------------------------------------------------- execution
    typedef enum logic [3:0] {
        X_IDLE, X_DISPATCH, X_HEAD, X_INDEX, X_SEARCH, X_ID, X_DAM, X_DATA
    } exec_t;
    exec_t xs;
    typedef enum logic [1:0] {K_IDLE, K_STEP, K_WAIT} seek_t;
    seek_t ks;
    reg [9:0] wait_t, seek_t_count;    // in ms
    // The step, head load and unload timers count milliseconds.
    reg [11:0] ms_div;
    wire ms_tick = tick && ms_div == 12'(T_PER_MS - 1);
    reg [1:0] revs;                    // index pulses in this search
    reg [2:0] id_count;
    reg [7:0] id_c, id_h, id_r, id_n;
    reg [15:0] crc;
    reg [5:0] dam_window;
    reg ddam;
    reg [13:0] sector_left;            // data bytes and the two CRC bytes left
    reg [13:0] rlen, data_count;

    wire [13:0] dr_len = dr[4] > 8'd8 ? 14'd8192 : 14'd128 << dr[4][3:0];
    wire [13:0] id_len = 14'd128 << id_n[3:0];
    wire [7:0] want_n = dr[4] > 8'd8 ? 8'd8 : dr[4];

    // Sense Interrupt Status reports the lowest unit whose seek has ended.
    reg [1:0] sense_unit;
    reg sense_any;
    integer k;
    always @* begin
        sense_any = 1'b0;
        sense_unit = 2'd0;
        for (k = 3; k >= 0; k = k - 1)
            if (seek_st[k] >= 3'd4) begin
                sense_any = 1'b1;
                sense_unit = 2'(k);
            end
    end
    wire [7:0] sense_st0 = (st0 & 8'h3f) | ST0_SEEK_END |
                           (seek_st[sense_unit] == 3'd5 ? ST0_ABNORMAL : 8'd0) |
                           (seek_st[sense_unit] == 3'd6 ? ST0_READY_CHANGE | ST0_NOT_READY : 8'd0);

    // cmd_result: the result phase, or the command phase when there is none.
    task automatic result_phase;
        begin
            exm <= 1'b0;
            rqm <= 1'b1;
            xs <= X_IDLE;
            cycle <= 4'd0;
            if (results(cmd) != 3'd0) begin
                phase <= P_RES;
                dio <= 1'b1;
                intrq <= I_RESULT;
            end else begin
                phase <= P_CMD;
                dio <= 1'b0;
                cb <= 1'b0;
            end
            if (head_loaded && cmd <= C_READ_ID) begin
                head_armed <= 1'b1;
                head_count <= hut_t;
            end
        end
    endtask

    task automatic fail(input [7:0] s0);
        begin
            st0 <= st0 | s0;
            result_phase();
        end
    endtask

    // Advance after a sector was read or skipped. A multi-track command reads
    // head 1 of the same cylinder after head 0, then ends at its EOT.
    task automatic next_read_sector;
        begin
            first_rw <= 1'b0;
            if (dr[5] > dr[3]) begin
                dr[3] <= dr[3] + 8'd1;
                revs <= 2'd0;
                xs <= X_SEARCH;
            end else if (mt && !hd) begin
                hd <= 1'b1;
                dr[0][2] <= 1'b1;
                dr[2] <= 8'd1;
                dr[3] <= 8'd1;
                revs <= 2'd0;
                xs <= X_SEARCH;
            end else begin
                if ((st0 & 8'hf8) == 8'd0 && st1 == 8'd0) begin
                    st0 <= (st0 & 8'h07) | ST0_ABNORMAL;
                    st1 <= ST1_EN;
                end else if ((st0 & ST0_READY_CHANGE) == 8'd0) begin
                    dr[1] <= dr[1] + 8'd1;
                    dr[3] <= 8'd1;
                end
                result_phase();
            end
        end
    endtask

    task automatic start_read;
        begin
            if (first_rw) begin
                first_rw <= 1'b0;
                revs <= 2'd0;
                xs <= X_SEARCH;
            end else next_read_sector();
        end
    endtask

    // A search ends without its ID: read_id's Missing Address Mark.
    task automatic search_failed;
        begin
            st1 <= (st1 & ~(ST1_DE | ST1_MA | ST1_ND)) | ST1_MA | ST1_ND |
                   (cmd == C_READ_DIAG ? ST1_EN : 8'd0);
            st2 <= st2 & ~(ST2_WC | ST2_BC);
            fail(ST0_ABNORMAL);
        end
    endtask

    wire start_command = phase == P_CMD && cb && xs == X_IDLE && cycle == 4'd1 + 4'(params(cmd));

    integer i;
    always @(posedge clk) begin
        step <= 1'b0;
        if (power_reset) begin
            motor_q <= 1'b0;
            motor_count <= 24'd0;
            spinning <= 1'b0;
            drive_ready <= 1'b0;
        end else begin
            // fdd.c: ready two turns after the motor starts; the disk stops
            // 1.5 turns after it stops.
            motor_q <= motor;
            if (motor != motor_q) motor_count <= motor ? READY_T : SPIN_DOWN_T;
            else if (tick && motor_count != 24'd0) motor_count <= motor_count - 24'd1;
            if (motor) spinning <= 1'b1;
            else if (motor_count == 24'd0 && motor == motor_q) spinning <= 1'b0;
            drive_ready <= motor && motor == motor_q && motor_count == 24'd0;
        end
        if (reset) begin
            phase <= P_CMD;
            rqm <= 1'b1;
            dio <= 1'b0;
            exm <= 1'b0;
            cb <= 1'b0;
            seeking <= 4'd0;
            seek_queue <= 4'd0;
            seek_busy <= 1'b0;
            ks <= K_IDLE;
            cycle <= 4'd0;
            st0 <= 8'd0;
            st1 <= 8'd0;
            st2 <= 8'd0;
            st3 <= 8'd0;
            for (i = 0; i < 4; i = i + 1) begin
                pcn[i] <= 8'd0;
                seek_st[i] <= 3'd0;
            end
            step_t <= 10'd16;
            hut_t <= 10'd240;
            hld_t <= 10'd254;
            ms_div <= 12'd0;
            head_loaded <= 1'b0;
            head_armed <= 1'b0;
            intrq <= I_NONE;
            xs <= X_IDLE;
            us <= 2'd0;
            hd <= 1'b0;
            mf <= 1'b1;
            cmd <= C_INVALID;
            data_r <= 8'hff;
            wait_t <= 10'd0;
            seek_t_count <= 10'd0;
        end else begin
            if (tick) ms_div <= ms_tick ? 12'd0 : ms_div + 12'd1;
            if (ms_tick && wait_t != 10'd0) wait_t <= wait_t - 10'd1;
            if (ms_tick && seek_t_count != 10'd0) seek_t_count <= seek_t_count - 10'd1;
            if (ms_tick && head_armed) begin
                if (head_count == 10'd0) begin
                    head_armed <= 1'b0;
                    head_loaded <= 1'b0;
                end else head_count <= head_count - 10'd1;
            end

            // --------------------------------------------- seek_step stepper
            case (ks)
                K_IDLE: for (i = 3; i >= 0; i = i - 1)
                    if (seek_queue[i]) begin
                        seek_unit <= 2'(i);
                        seek_busy <= 1'b1;
                        ks <= K_STEP;
                    end
                K_STEP: begin
                    if (pcn[seek_unit] == ncn[seek_unit] && seek_st[seek_unit] == 3'd2 && !tr00) begin
                        // A recalibrate that has not found track 0.
                        seek_st[seek_unit] <= 3'd5;
                        intrq <= I_SEEK;
                        st0 <= st0 | ST0_EQUIP;
                        ks <= K_IDLE;
                    end else if (pcn[seek_unit] == ncn[seek_unit] || (seek_st[seek_unit] == 3'd2 && tr00)) begin
                        if (seek_st[seek_unit] == 3'd2) pcn[seek_unit] <= 8'd0;
                        seek_st[seek_unit] <= 3'd4;
                        intrq <= I_SEEK;
                        ks <= K_IDLE;
                    end else if (!(drive_ready && !seek_unit[0] && disk_ready)) begin
                        if (seek_st[seek_unit] == 3'd2)
                            pcn[seek_unit] <= rec[seek_unit] - (8'd77 - pcn[seek_unit]);
                        seek_st[seek_unit] <= 3'd6;
                        intrq <= I_READY;
                        ks <= K_IDLE;
                    end else begin
                        step <= 1'b1;
                        step_out <= pcn[seek_unit] > ncn[seek_unit];
                        pcn[seek_unit] <= pcn[seek_unit] > ncn[seek_unit] ? pcn[seek_unit] - 8'd1 : pcn[seek_unit] + 8'd1;
                        seek_t_count <= step_t;
                        ks <= K_WAIT;
                    end
                    if (ks == K_STEP && (pcn[seek_unit] == ncn[seek_unit] || (seek_st[seek_unit] == 3'd2 && tr00) ||
                        !(drive_ready && !seek_unit[0] && disk_ready))) begin
                        seeking[seek_unit] <= 1'b0;
                        seek_queue[seek_unit] <= 1'b0;
                        seek_busy <= 1'b0;
                    end
                end
                default: if (seek_t_count == 10'd0) ks <= K_STEP;
            endcase

            // ------------------------------------------------- the command
            case (xs)
                X_DISPATCH: begin
                    // The unit and head are selected; run the command.
                    if (cmd < C_SENSE_INT) begin
                        st0 <= {5'd0, hd, us};
                        if (cmd < C_RECALIBRATE) begin
                            st1 <= 8'd0;
                            st2 <= 8'd0;
                        end
                    end
                    case (cmd)
                        C_INVALID, C_VERSION, C_SCAN: begin
                            st0 <= 8'h80;
                            result_phase();
                        end
                        C_SPECIFY: begin
                            // At 4 MHz: steps of (16 - SRT) * 2 ms, unload
                            // HUT * 32 ms (0: 256), load HLT * 4 ms (0: 512).
                            step_t <= {4'd0, 5'd16 - {1'b0, dr[0][7:4]}, 1'b0};
                            hut_t <= dr[0][3:0] == 4'd0 ? 10'd256 : {1'b0, dr[0][3:0], 5'd0};
                            hld_t <= dr[1][7:1] == 7'd0 ? 10'd512 : {1'b0, dr[1][7:1], 2'd0};
                            result_phase();
                        end
                        C_SENSE_DRIVE: begin
                            // The +3 wires double sided to write protect. Drive
                            // B is an empty drive, write protected, as in Fuse.
                            st3 <= {1'b0, 1'b1, !us[0] && ready, tr00, 1'b0, hd, us};
                            result_phase();
                        end
                        C_SENSE_INT: begin
                            if (sense_any) begin
                                st0 <= sense_st0;
                                sense0 <= sense_st0 & 8'hfb;
                                sense1 <= pcn[sense_unit];
                                seek_st[sense_unit] <= 3'd0;
                            end
                            result_phase();
                        end
                        C_RECALIBRATE, C_SEEK: begin
                            if (!seeking[us]) begin
                                if (cmd == C_RECALIBRATE) begin
                                    rec[us] <= pcn[us];
                                    pcn[us] <= 8'd77;
                                    ncn[us] <= 8'd0;
                                    dr[1] <= 8'd0;
                                    seek_st[us] <= 3'd2;
                                end else begin
                                    ncn[us] <= dr[1];
                                    seek_st[us] <= 3'd1;
                                end
                                seeking[us] <= 1'b1;
                                seek_queue[us] <= 1'b1;
                            end
                            result_phase();
                        end
                        C_WRITE_DATA, C_WRITE_ID: begin
                            st1 <= ST1_NW;
                            st0 <= {5'd0, hd, us} | ST0_ABNORMAL;
                            result_phase();
                        end
                        default: begin   // Read Data, Read Diagnostic, Read ID
                            rlen <= dr[4] == 8'd0 && dr[7] < 8'd128 ? {6'd0, dr[7]} : dr_len;
                            first_rw <= 1'b1;
                            head_armed <= 1'b0;
                            wait_t <= head_loaded ? 10'd0 : hld_t;
                            head_loaded <= 1'b1;
                            xs <= X_HEAD;
                        end
                    endcase
                end
                // ------------------------------------------- head load wait
                X_HEAD: if (wait_t == 10'd0) begin
                    if (cmd == C_READ_DIAG) xs <= X_INDEX;
                    else if (cmd == C_READ_DATA) start_read();
                    else begin
                        revs <= 2'd0;
                        xs <= X_SEARCH;
                    end
                end
                // Read Diagnostic starts at the index hole.
                X_INDEX: if (!ready) search_failed();
                else if (byte_ev && byte_wrap) begin
                    revs <= 2'd0;
                    xs <= X_SEARCH;
                end
                // ------------------------------------------------ ID search
                X_SEARCH: if (!ready) search_failed();
                else if (byte_ev) begin
                    if (byte_wrap) begin
                        // An index before an ID: read_id finds none.
                        st1 <= (st1 & ~(ST1_DE | ST1_MA | ST1_ND)) | ST1_MA | ST1_ND;
                        st2 <= st2 & ~(ST2_WC | ST2_BC);
                        if (revs == 2'd1) search_failed();
                        revs <= revs + 2'd1;
                    end else if (mark && byte_data == 8'hfe) begin
                        crc <= crc_step(mf ? CRC_A1A1A1 : 16'hffff, 8'hfe);
                        id_count <= 3'd0;
                        xs <= X_ID;
                    end
                end
                X_ID: if (byte_ev) begin
                    crc <= crc_step(crc, byte_data);
                    id_count <= id_count + 3'd1;
                    case (id_count)
                        3'd0: id_c <= byte_data;
                        3'd1: id_h <= byte_data;
                        3'd2: id_r <= byte_data;
                        3'd3: id_n <= byte_data > 8'd8 ? 8'd8 : byte_data;
                        default: ;
                    endcase
                    if (id_count == 3'd5) begin
                        xs <= X_SEARCH;
                        if (crc_step(crc, byte_data) != 16'd0) begin
                            // A bad ID: CRC Error and No Data.
                            st1 <= (st1 & ~(ST1_DE | ST1_MA | ST1_ND)) | ST1_DE | ST1_ND;
                            if (cmd == C_READ_DATA) st2 <= st2 & ~(ST2_WC | ST2_BC);
                            else if (cmd == C_READ_ID) begin
                                dr[1] <= id_c;
                                dr[2] <= id_h;
                                dr[3] <= id_r;
                                dr[4] <= id_n;
                                fail(ST0_ABNORMAL);
                            end else begin   // Read Diagnostic reads it anyway
                                if (id_c != dr[1]) st2 <= st2 | ST2_WC | (id_c == 8'hff ? ST2_BC : 8'd0);
                                dam_window <= 6'd43;
                                xs <= X_DAM;
                            end
                        end else case (cmd)
                            C_READ_ID: begin
                                st1 <= st1 & ~(ST1_DE | ST1_MA | ST1_ND);
                                dr[1] <= id_c;
                                dr[2] <= id_h;
                                dr[3] <= id_r;
                                dr[4] <= id_n;
                                result_phase();
                            end
                            C_READ_DIAG: begin
                                st1 <= (st1 & ~(ST1_DE | ST1_MA | ST1_ND)) |
                                       (id_c != dr[1] || id_r != dr[3] || id_h != dr[2] ? ST1_ND : 8'd0);
                                if (id_c != dr[1]) st2 <= st2 | ST2_WC | (id_c == 8'hff ? ST2_BC : 8'd0);
                                dam_window <= 6'd43;
                                xs <= X_DAM;
                            end
                            default: begin   // seek_id
                                st1 <= st1 & ~(ST1_DE | ST1_MA | ST1_ND);
                                st2 <= st2 & ~(ST2_WC | ST2_BC);
                                if (id_c != dr[1])
                                    st2 <= (st2 & ~(ST2_WC | ST2_BC)) | ST2_WC | (id_c == 8'hff ? ST2_BC : 8'd0);
                                else if (id_r == dr[3] && id_h == dr[2] && id_n == want_n) begin
                                    dam_window <= 6'd43;
                                    xs <= X_DAM;
                                end else
                                    st1 <= (st1 & ~(ST1_DE | ST1_MA | ST1_ND)) | ST1_ND;
                            end
                        endcase
                    end
                end
                // ---------------------------------------------- data mark
                X_DAM: if (byte_ev) begin
                    dam_window <= dam_window - 6'd1;
                    if (mark && byte_data >= 8'hf8 && byte_data <= 8'hfb) begin
                        ddam <= byte_data != 8'hfb;
                        crc <= crc_step(mf ? CRC_A1A1A1 : 16'hffff, byte_data);
                        sector_left <= id_len + 14'd2;
                        data_count <= 14'd0;
                        xs <= X_DATA;
                        if (cmd == C_READ_DATA && (byte_data != 8'hfb) != del_data) begin
                            st2 <= st2 | ST2_CM;
                            if (sk) begin
                                // Skip the sector and look for the next.
                                next_read_sector();
                            end
                        end
                    end else if (dam_window == 6'd0 || mark) begin
                        st2 <= st2 | ST2_MD;
                        fail(ST0_ABNORMAL);
                    end
                end
                // ---------------------------------------------- data bytes
                X_DATA: if (byte_ev) begin
                    crc <= crc_step(crc, byte_data);
                    sector_left <= sector_left - 14'd1;
                    if (sector_left > 14'd2 && data_count < rlen) begin
                        data_count <= data_count + 14'd1;
                        if (rqm) begin
                            st1 <= st1 | ST1_OR;
                            fail(ST0_ABNORMAL);
                        end else begin
                            data_r <= byte_data;
                            rqm <= 1'b1;
                            dio <= 1'b1;
                        end
                    end
                    if (sector_left == 14'd1 && !(rqm && sector_left > 14'd2)) begin
                        if (crc_step(crc, byte_data) != 16'd0) begin
                            st2 <= st2 | ST2_DD;
                            st1 <= st1 | ST1_DE;
                        end
                        if (cmd == C_READ_DATA) begin
                            if (crc_step(crc, byte_data) != 16'd0) fail(ST0_ABNORMAL);
                            else if (ddam != del_data) begin
                                if (dr[5] > dr[3]) st0 <= st0 | ST0_ABNORMAL;
                                result_phase();
                            end else start_read();
                        end else begin   // Read Diagnostic
                            dr[3] <= dr[3] + 8'd1;
                            dr[5] <= dr[5] - 8'd1;
                            if (dr[5] == 8'd1) result_phase();
                            else begin
                                revs <= 2'd0;
                                xs <= X_SEARCH;
                            end
                        end
                    end
                end
                default: ;
            endcase

            // ----------------------------------------------------- CPU access
            if (read_strobe && data_port && rqm && dio) begin
                if (phase == P_EXE) rqm <= 1'b0;
                else if (phase == P_RES) begin
                    if (cycle == 4'(results(cmd)) - 4'd1) begin
                        phase <= P_CMD;
                        rqm <= 1'b1;
                        dio <= 1'b0;
                        cb <= 1'b0;
                        if (cmd == C_SENSE_INT) begin
                            // A Sense Interrupt result acknowledges the seek
                            // status it returned. Keep an interrupt only when
                            // another unit still has a completed seek status.
                            if (!sense_any) intrq <= I_NONE;
                            else if (seek_st[sense_unit] == 3'd6) intrq <= I_READY;
                            else intrq <= I_SEEK;
                        end else if (intrq < I_READY) intrq <= I_NONE;
                        cycle <= 4'd0;
                    end else cycle <= cycle + 4'd1;
                end
            end
            if (write_strobe && rqm && !dio && phase == P_CMD && !start_command) begin
                if (cycle == 4'd0) begin
                    command <= wdata;
                    // Sense Interrupt Status with no interrupt pending is
                    // an invalid command.
                    cmd <= identify(wdata) == C_SENSE_INT && intrq == I_NONE ? C_INVALID : identify(wdata);
                    mt <= wdata[7];
                    mf <= wdata[6];
                    sk <= wdata[5];
                    cb <= 1'b1;
                end else dr[cycle - 4'd1] <= wdata;
                cycle <= cycle + 4'd1;
            end
            // After the last parameter byte, select the unit and head, then
            // dispatch the command on the next clock.
            if (start_command) begin
                phase <= P_EXE;
                rqm <= 1'b0;
                exm <= 1'b1;
                if (cmd != C_SENSE_INT && cmd != C_SPECIFY && cmd != C_VERSION && cmd != C_INVALID) begin
                    us <= dr[0][1:0];
                    hd <= dr[0][2];
                    if (cmd == C_READ_DATA || cmd == C_WRITE_DATA) begin
                        del_data <= command[3];
                        sk <= dr[0][5];   // as upd_fdc.c takes it
                    end
                end
                if (cmd == C_RECALIBRATE || cmd == C_SEEK || cmd == C_SPECIFY) cb <= 1'b0;
                xs <= X_DISPATCH;
            end
        end
    end
endmodule
